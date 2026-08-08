package main

import (
	"database/sql"
	"log"
	"time"
)

// pendingActionsDB abre uma conexão sob demanda com o banco principal
// (mesmo padrão usado em outras partes do projeto, ex: openCRMDB).
func pendingActionsDB() (*sql.DB, error) {
	return sql.Open("sqlite", DB_PATH)
}

// savePendingAction grava (ou atualiza) uma pending action no SQLite,
// além do mapa em memória. Chamado a partir de setPendingAction.
func savePendingAction(key string, pa *PendingAction) {
	db, err := pendingActionsDB()
	if err != nil {
		log.Printf("[WARN] pending_actions: erro ao abrir DB para persistir: %v", err)
		return
	}
	defer db.Close()
	_, err = db.Exec(
		`INSERT INTO pending_actions (key, id, tool_name, args_json, description, created_at, action_type, tenant_id, diff_preview)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET
		   id=excluded.id, tool_name=excluded.tool_name, args_json=excluded.args_json,
		   description=excluded.description, created_at=excluded.created_at,
		   action_type=excluded.action_type, tenant_id=excluded.tenant_id, diff_preview=excluded.diff_preview`,
		key, pa.ID, pa.ToolName, pa.ArgsJSON, pa.Description,
		pa.CreatedAt.Format(time.RFC3339), pa.ActionType, pa.TenantID, pa.DiffPreview,
	)
	if err != nil {
		log.Printf("[WARN] pending_actions: erro ao persistir key=%s: %v", key, err)
	}
}

// deletePendingActionDB remove a pending action do SQLite. Chamado a
// partir de clearPendingAction.
func deletePendingActionDB(key string) {
	db, err := pendingActionsDB()
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec(`DELETE FROM pending_actions WHERE key = ?`, key)
}

// loadPendingActionsFromDB repopula pendingActionMap a partir do SQLite
// no startup do backend, para sobreviver a restarts sem perder
// aprovações pendentes. Ações já expiradas (TTL 30min, mesma regra de
// getPendingAction) são descartadas e removidas do banco nesse momento.
func loadPendingActionsFromDB() {
	db, err := pendingActionsDB()
	if err != nil {
		log.Printf("[WARN] pending_actions: erro ao abrir DB no startup: %v", err)
		return
	}
	defer db.Close()

	rows, err := db.Query(`SELECT key, id, tool_name, args_json, description, created_at, action_type, tenant_id, diff_preview FROM pending_actions`)
	if err != nil {
		log.Printf("[WARN] pending_actions: erro ao ler DB no startup: %v", err)
		return
	}
	defer rows.Close()

	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()

	loaded := 0
	expired := 0
	var expiredKeys []string
	for rows.Next() {
		var key, createdAtStr string
		pa := &PendingAction{}
		if err := rows.Scan(&key, &pa.ID, &pa.ToolName, &pa.ArgsJSON, &pa.Description, &createdAtStr, &pa.ActionType, &pa.TenantID, &pa.DiffPreview); err != nil {
			log.Printf("[WARN] pending_actions: erro ao ler linha: %v", err)
			continue
		}
		createdAt, err := time.Parse(time.RFC3339, createdAtStr)
		if err != nil {
			createdAt = time.Now()
		}
		pa.CreatedAt = createdAt

		if time.Since(pa.CreatedAt) > 30*time.Minute {
			expired++
			expiredKeys = append(expiredKeys, key)
			continue
		}
		pendingActionMap[key] = pa
		loaded++
	}
	for _, k := range expiredKeys {
		deletePendingActionDB(k)
	}
	log.Printf("[AUDIT] pending_actions: %d acao(oes) pendente(s) recarregada(s) do SQLite, %d expirada(s) descartada(s)", loaded, expired)
}
