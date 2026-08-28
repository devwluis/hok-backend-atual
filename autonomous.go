package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"sync"
	"time"
)

// ─── Modo autônomo (29/08) ────────────────────────────────────────────────
// Decisões fechadas por Washington (28/08):
//  1. POST /session/mode + leitura do session_mode no fluxo (a tabela é a
//     fonte do modo quando o request não traz mode).
//  2. Budget = nº de chamadas ao agente (default 5), contado por execução
//     no Hokma (não por tool no stream).
//  3. Hermes autônomo = container efêmero com o volume real montado rw
//     (sem --rm) dentro do budget controlado — base do isolamento do plan,
//     adaptada para permitir escrita real.
//  4. Blocklist em autônomo → aviso direto; sem aprovação manual automática
//     (o usuário troca de modo manualmente se quiser a ação).
//  5. Circuit breaker: 3 ações idênticas / 3 erros consecutivos / 10 min
//     por conversa (ajustável com o uso real).

const (
	autonomousDefaultBudget  = 5
	autonomousMaxDuration    = 10 * time.Minute
	autonomousRepeatLimit    = 3
	autonomousErrorLimit     = 3
	autonomousAuditTruncLen  = 200
	autonomousAuditTableDDL = `CREATE TABLE IF NOT EXISTS autonomous_audit (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER DEFAULT (unixepoch()),
		conv_id TEXT, tenant_id TEXT, user_id TEXT,
		agent TEXT, action TEXT, action_hash TEXT,
		budget_left INTEGER, status TEXT
	);`
)

func autonomousActionHash(action string) string {
	sum := sha256.Sum256([]byte(action))
	return hex.EncodeToString(sum[:8])
}

// isAutonomousLike — modo autônomo (com budget) e autônomo TOTAL (com
// snapshot+rollback) compartilham os gates de blocklist/budget/circuit
// breaker; a diferença é o snapshot automático e o budget alto (50).
func isAutonomousLike(mode string) bool {
	return mode == "autonomous" || mode == "autonomous_total"
}

// sessionModeSetCheckpoint grava o checkpoint_id da conversa (o rollback e o
// comando "volte pro checkpoint" usam essa referência).
func sessionModeSetCheckpoint(convID, tenantID, userID, checkpointID string) {
	_, err := db.Exec(`UPDATE session_mode SET checkpoint_id=?, updated_at=unixepoch() WHERE tenant_id=? AND user_id=? AND conv_id=?`, checkpointID, tenantID, userID, convID)
	if err != nil {
		log.Printf("[AUDIT] sessionModeSetCheckpoint falhou conv=%s: %v", convID, err)
	}
}

// autonomousTotalEnsureSnapshot — modo AUTÔNOMO TOTAL (29/08): snapshot
// automático (código git + banco + volume hermes + .env) antes da primeira
// execução da conversa, mesmo quando o request traz o mode direto (sem
// passar pelo POST /session/mode). Idempotente: o checkpoint_id gravado
// impede snapshots duplicados.
func autonomousTotalEnsureSnapshot(convID, tenantID, userID string) {
	if sessionModeCheckpoint(convID, tenantID, userID) != "" {
		return
	}
	id, err := snapshotCreate(convID, tenantID, userID)
	if err != nil {
		log.Printf("[AUDIT] snapshot do autonomous_total FALHOU conv=%s: %v", convID, err)
		return
	}
	sessionModeSetCheckpoint(convID, tenantID, userID, id)
}

// ─── Circuit breaker (in-memory por conversa) ──────────────────────────────

type autonomousCB struct {
	lastHash    string
	repeatCount int
	errStreak   int
	startTs     time.Time
}

var (
	autonomousCBmu sync.Mutex
	autonomousCBs  = map[string]*autonomousCB{}
)

func autonomousCBFor(convID string) *autonomousCB {
	autonomousCBmu.Lock()
	defer autonomousCBmu.Unlock()
	cb, ok := autonomousCBs[convID]
	if !ok {
		cb = &autonomousCB{startTs: time.Now()}
		autonomousCBs[convID] = cb
	}
	return cb
}

func autonomousCBReset(convID string) {
	autonomousCBmu.Lock()
	defer autonomousCBmu.Unlock()
	delete(autonomousCBs, convID)
}

// decide aplica as regras do circuit breaker (decisão 5): 3 ações idênticas
// em sequência, 3 erros consecutivos ou 10 min de modo autônomo → blocked.
func (s *autonomousCB) decide(actionHash string, now time.Time) (blocked bool, reason string) {
	if now.Sub(s.startTs) > autonomousMaxDuration {
		return true, "tempo de modo autônomo excedido (10 min) — troque o modo e retome"
	}
	if s.errStreak >= autonomousErrorLimit {
		return true, "3 erros consecutivos — execução interrompida (circuit breaker)"
	}
	if actionHash == s.lastHash {
		s.repeatCount++
	} else {
		s.lastHash = actionHash
		s.repeatCount = 1
	}
	if s.repeatCount >= autonomousRepeatLimit {
		return true, "mesma ação repetida 3x — execução interrompida (circuit breaker)"
	}
	return false, ""
}

func (s *autonomousCB) recordResult(err error) {
	if err != nil {
		s.errStreak++
	} else {
		s.errStreak = 0
	}
}

// ─── Budget (session_mode.autonomous_budget) ───────────────────────────────

func autonomousBudgetLeft(convID, tenantID, userID string) int {
	var b int
	err := db.QueryRow(`SELECT autonomous_budget FROM session_mode WHERE tenant_id=? AND user_id=? AND conv_id=?`, tenantID, userID, convID).Scan(&b)
	if err != nil {
		return 0
	}
	return b
}

func autonomousBudgetDecrement(convID, tenantID, userID string) error {
	_, err := db.Exec(`UPDATE session_mode SET autonomous_budget = autonomous_budget - 1 WHERE tenant_id=? AND user_id=? AND conv_id=?`, tenantID, userID, convID)
	return err
}

// ─── Auditoria (autonomous_audit) ───────────────────────────────────────────

func autonomousAuditLog(convID, tenantID, userID, agent, action, status string, budgetLeft int) {
	h := autonomousActionHash(action)
	if len(action) > autonomousAuditTruncLen {
		action = action[:autonomousAuditTruncLen]
	}
	_, err := db.Exec(`INSERT INTO autonomous_audit (conv_id, tenant_id, user_id, agent, action, action_hash, budget_left, status) VALUES (?,?,?,?,?,?,?,?)`,
		convID, tenantID, userID, agent, action, h, budgetLeft, status)
	if err != nil {
		log.Printf("[AUDIT] autonomous_audit falhou: %v", err)
	}
}

// ─── Autorização ────────────────────────────────────────────────────────────

// autonomousAllow valida circuit breaker + budget e decrementa quando
// autoriza. A BLOCKLIST é checada ANTES pelo chamador (o reply de bloqueio
// é específico por agente — decisão 4: aviso direto, sem pendência).
// MODO AUTÔNOMO TOTAL (29/08, decisão 3): se a conversa tem auto_rollback
// ligado e o modo é total, o bloqueio dispara o rollback automático (o
// recovery.sh roda fora do cgroup — o serviço para, restaura e volta).
func autonomousAllow(convID, tenantID, userID, agent, action string) (allowed bool, reason string, budgetLeft int) {
	cb := autonomousCBFor(convID)
	if blocked, r := cb.decide(autonomousActionHash(action), time.Now()); blocked {
		left := autonomousBudgetLeft(convID, tenantID, userID)
		autonomousAuditLog(convID, tenantID, userID, agent, action, "blocked_cb", left)
		return false, autonomousTotalMaybeRollback(convID, tenantID, userID, r), left
	}
	left := autonomousBudgetLeft(convID, tenantID, userID)
	if left <= 0 {
		autonomousAuditLog(convID, tenantID, userID, agent, action, "blocked_budget", 0)
		return false, autonomousTotalMaybeRollback(convID, tenantID, userID, fmt.Sprintf("budget esgotado (0/%d) — recarregue o modo autônomo via POST /session/mode", autonomousDefaultBudget)), 0
	}
	if err := autonomousBudgetDecrement(convID, tenantID, userID); err != nil {
		log.Printf("[AUDIT] decremento de budget falhou conv=%s: %v", convID, err)
	}
	autonomousAuditLog(convID, tenantID, userID, agent, action, "ok", left-1)
	return true, "", left - 1
}

// autonomousTotalMaybeRollback — se a conversa está em autonomous_total com
// auto_rollback, dispara o rollback automático e anexa o aviso ao motivo.
func autonomousTotalMaybeRollback(convID, tenantID, userID, reason string) string {
	mode, _, checkpointID, autoRollback, ok := sessionModeLoad(convID, tenantID, userID)
	if !ok || mode != "autonomous_total" || !autoRollback || checkpointID == "" {
		return reason
	}
	if err := triggerRecovery(checkpointID); err != nil {
		log.Printf("[AUDIT] auto_rollback falhou conv=%s checkpoint=%s: %v", convID, checkpointID, err)
		return reason + " (auto_rollback: disparo falhou)"
	}
	return reason + fmt.Sprintf(" — ROLLBACK AUTOMÁTICO disparado (checkpoint %s, serviço será reiniciado)", checkpointID)
}