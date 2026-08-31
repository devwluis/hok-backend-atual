package main

import (
	"log"
	"net/http"
	"strconv"
	"time"
)

// ─── CURAÇÃO FREE + AUTO-ATUALIZAÇÃO (31/08) ───────────────────────────────
// Decisão aprovada: o job de sincronização opera SOMENTE sobre modelos
// GRATUITOS confirmados via fonte oficial (Zen/Go/OpenRouter/AIHubMix). Modelos
// pagos (ex: GLM-5.3-Flash) ficam fora — não entram na rotina free.
// Mecanismo: snapshot persistido da lista free por fonte → diff (added/
// removed/metadata_changed) → audit log → upsert do snapshot. Roda 1x/dia
// (ticker interno) e sob demanda via POST /catalog/sync.

const (
	catalogSyncDaily = 24 * time.Hour
)

// catalogSnapshotRow — linha do snapshot persistido (somente free).
type catalogSnapshotRow struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Category string `json:"category"`
	Source   string `json:"source"`
	SeenAt   string `json:"seen_at"`
}

func ensureCatalogTables() {
	sqliteExec(`CREATE TABLE IF NOT EXISTS catalog_snapshot (
		id TEXT PRIMARY KEY,
		provider TEXT, category TEXT, source TEXT, seen_at INTEGER DEFAULT (unixepoch())
	);`)
	sqliteExec(`CREATE TABLE IF NOT EXISTS catalog_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER DEFAULT (unixepoch()),
		action TEXT, model_id TEXT, provider TEXT, source TEXT, detail TEXT
	);`)
}

func catalogSnapshotLoad() []catalogSnapshotRow {
	rows := sqliteExecQuoted(`SELECT id, provider, category, source, seen_at FROM catalog_snapshot;`)
	var out []catalogSnapshotRow
	for _, r := range parseQuotedRows(rows, 5) {
		if len(r) < 4 {
			continue
		}
		out = append(out, catalogSnapshotRow{ID: r[0], Provider: r[1], Category: r[2], Source: r[3]})
	}
	return out
}

func catalogSnapshotUpsert(rows []catalogSnapshotRow) {
	for _, r := range rows {
		sqliteExecParams(`INSERT INTO catalog_snapshot (id, provider, category, source, seen_at) VALUES (?,?,?,?,unixepoch())
			ON CONFLICT(id) DO UPDATE SET provider=excluded.provider, category=excluded.category,
				source=excluded.source, seen_at=unixepoch()`,
			r.ID, r.Provider, r.Category, r.Source)
	}
}

func catalogSnapshotDelete(ids []string) {
	for _, id := range ids {
		sqliteExecParams(`DELETE FROM catalog_snapshot WHERE id=?;`, id)
	}
}

func catalogAuditLog(action, modelID, provider, source, detail string) {
	sqliteExecParams(`INSERT INTO catalog_audit (action, model_id, provider, source, detail) VALUES (?,?,?,?,?)`,
		action, modelID, provider, source, detail)
}

// catalogSyncFromFreeLists — delta detection PURA (testável): compara a lista
// nova de modelos free (id -> {provider, category, source}) com o snapshot e
// devolve added/removed/metadata_changed. Não toca no banco.
func catalogSyncFromFreeLists(snapshot []catalogSnapshotRow, newFree map[string]catalogSnapshotRow) (added, removed, changed []catalogSnapshotRow) {
	seen := map[string]bool{}
	for _, s := range snapshot {
		seen[s.ID] = true
		n, ok := newFree[s.ID]
		if !ok {
			removed = append(removed, s)
			continue
		}
		if n.Category != s.Category || n.Provider != s.Provider || n.Source != s.Source {
			changed = append(changed, n)
		}
	}
	for _, n := range newFree {
		if !seen[n.ID] {
			added = append(added, n)
		}
	}
	return added, removed, changed
}

// freeCatalogFromCache monta o mapa de modelos free do catálogo atual (fonte
// de verdade em memória, já populado por refreshCatalog). Somente free.
func freeCatalogFromCache() map[string]catalogSnapshotRow {
	catalogCacheMutex.RLock()
	defer catalogCacheMutex.RUnlock()
	out := map[string]catalogSnapshotRow{}
	for _, m := range catalogCache {
		if !m.Free {
			continue
		}
		if paidDenylist[m.ID] {
			continue // REMOÇÃO EXPLÍCITA (31/08): pago confirmado — sync nunca re-insere
		}
		src := m.FreeSource
		if src == "" {
			src = "api-official"
		}
		out[m.ID] = catalogSnapshotRow{ID: m.ID, Provider: m.Provider, Category: m.Category, Source: src}
	}
	return out
}

// catalogSync — roda a sincronização: garante o catálogo fresco (refresh),
// difere os free vs snapshot e grava o audit. Retorna resumo textual.
func catalogSync() (string, error) {
	if err := refreshCatalog(true); err != nil {
		log.Printf("[catalog/sync] refresh falhou: %v", err)
	}
	ensureCatalogTables()
	snapshot := catalogSnapshotLoad()
	newFree := freeCatalogFromCache()
	added, removed, changed := catalogSyncFromFreeLists(snapshot, newFree)

	for _, a := range added {
		catalogAuditLog("added", a.ID, a.Provider, a.Source, "novo modelo free confirmado")
	}
	for _, r := range removed {
		catalogAuditLog("removed", r.ID, r.Provider, r.Source, "modelo free sumiu da lista oficial")
	}
	for _, c := range changed {
		catalogAuditLog("metadata_changed", c.ID, c.Provider, c.Source,
			"categoria/source mudou de "+c.Category)
	}

	// Aplica no snapshot: remove sumidos, upsert do restante.
	var upsert []catalogSnapshotRow
	removedIDs := map[string]bool{}
	for _, r := range removed {
		removedIDs[r.ID] = true
	}
	for _, s := range snapshot {
		if !removedIDs[s.ID] {
			upsert = append(upsert, s)
		}
	}
	// sobrescreve com o estado atual (novo + existente preservado)
	cur := map[string]bool{}
	for _, a := range upsert {
		cur[a.ID] = true
	}
	for _, n := range newFree {
		if !cur[n.ID] {
			upsert = append(upsert, n)
		}
	}
	catalogSnapshotDelete(removedIDsKeys(removedIDs))
	catalogSnapshotUpsert(upsert)

	summary := "catalog/sync ok: +" + strconv.Itoa(len(added)) + " added, -" + strconv.Itoa(len(removed)) +
		" removed, " + strconv.Itoa(len(changed)) + " metadata_changed (free only)"
	log.Printf("[catalog/sync] %s", summary)
	return summary, nil
}

func removedIDsKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// handleCatalogSync — POST /catalog/sync (requireHokAuth): roda a
// sincronização free sob demanda (uso: cron/curação manual).
func handleCatalogSync(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	summary, err := catalogSync()
	if err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "summary": summary})
}

// startCatalogSyncDaily — ticker diário de sincronização da curadoria free
// (requisito 6-7: detectar adições/remoções/mudanças e aplicar + audit).
func startCatalogSyncDaily() {
	go func() {
		time.Sleep(5 * time.Minute) // não disputar com o refresh inicial
		for range time.NewTicker(catalogSyncDaily).C {
			_, _ = catalogSync()
		}
	}()
}
