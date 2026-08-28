package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// ─── Session mode (29/08) ───────────────────────────────────────────────────
// POST /session/mode {conv_id?, mode, autonomous_budget?, auto_rollback?} —
// upsert na tabela session_mode (alimenta os 3 botões do ChatScreen + o modo
// AUTÔNOMO TOTAL). GET /session/mode lê o estado atual. O fluxo
// (handleSmartChat) lê a tabela como fonte do modo quando o request não traz
// mode (decisão 1 fechada em 28/08).

func sessionModeLoad(convID, tenantID, userID string) (mode string, budget int, checkpointID string, autoRollback bool, ok bool) {
	err := db.QueryRow(`SELECT mode, autonomous_budget, checkpoint_id, auto_rollback FROM session_mode WHERE tenant_id=? AND user_id=? AND conv_id=?`, tenantID, userID, convID).Scan(&mode, &budget, &checkpointID, &autoRollback)
	if err != nil {
		return "", 0, "", false, false
	}
	return mode, budget, checkpointID, autoRollback, true
}

func sessionModeCheckpoint(convID, tenantID, userID string) string {
	mode, _, checkpointID, _, ok := sessionModeLoad(convID, tenantID, userID)
	if !ok || mode == "" {
		return ""
	}
	return checkpointID
}

func sessionModeSet(convID, tenantID, userID, mode string, budget int, autoRollback bool, setBy string) error {
	_, err := db.Exec(`INSERT INTO session_mode (tenant_id, user_id, conv_id, mode, autonomous_budget, set_by, auto_rollback, updated_at)
		VALUES (?,?,?,?,?,?,?,unixepoch())
		ON CONFLICT(tenant_id, user_id, conv_id) DO UPDATE SET
			mode=excluded.mode, autonomous_budget=excluded.autonomous_budget,
			set_by=excluded.set_by, auto_rollback=excluded.auto_rollback,
			updated_at=unixepoch()`,
		tenantID, userID, convID, mode, budget, setBy, boolInt(autoRollback))
	if err == nil {
		autonomousCBReset(convID)
	}
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// normalizeAutonomousBudget — decisões 2 (28/08) e 2 (29/08): default 5 no
// modo autônomo, 50 no AUTÔNOMO TOTAL (budget alto como rede de segurança
// mesmo se o circuit breaker falhar de forma imprevista); 0 nos demais
// modos; o body pode especificar.
func normalizeAutonomousBudget(mode string, requested *int) int {
	if mode == "autonomous_total" {
		if requested != nil && *requested > 0 {
			return *requested
		}
		return totalBudget
	}
	if mode != "autonomous" {
		return 0
	}
	if requested != nil && *requested > 0 {
		return *requested
	}
	return autonomousDefaultBudget
}

func handleSessionMode(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	tenantID := tenantIdFromRequest(r)
	userID := userIdFromRequest(r)
	convID := convIdFromRequest(r)

	w.Header().Set("Content-Type", "application/json")
	if r.Method == http.MethodGet {
		mode, budget, checkpointID, autoRollback, ok := sessionModeLoad(convID, tenantID, userID)
		if !ok {
			json.NewEncoder(w).Encode(map[string]interface{}{"conv_id": convID, "mode": "", "autonomous_budget": 0})
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"conv_id": convID, "mode": mode, "autonomous_budget": budget, "checkpoint_id": checkpointID, "auto_rollback": autoRollback})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ConvID           string `json:"conv_id"`
		Mode             string `json:"mode"`
		AutonomousBudget *int   `json:"autonomous_budget"`
		AutoRollback     *bool  `json:"auto_rollback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if body.ConvID != "" {
		convID = body.ConvID
	}
	if body.Mode != "plan" && body.Mode != "build" && body.Mode != "autonomous" && body.Mode != "autonomous_total" {
		http.Error(w, "mode inválido (plan|build|autonomous|autonomous_total)", http.StatusBadRequest)
		return
	}
	budget := normalizeAutonomousBudget(body.Mode, body.AutonomousBudget)
	autoRollback := body.AutoRollback != nil && *body.AutoRollback
	if err := sessionModeSet(convID, tenantID, userID, body.Mode, budget, autoRollback, "web"); err != nil {
		http.Error(w, "falha ao salvar: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// MODO AUTÔNOMO TOTAL (29/08, decisão 1): snapshot automático no
	// momento da ATIVAÇÃO (1x por conversa — o checkpoint fica gravado).
	if body.Mode == "autonomous_total" {
		if sessionModeCheckpoint(convID, tenantID, userID) == "" {
			if id, err := snapshotCreate(convID, tenantID, userID); err != nil {
				log.Printf("[AUDIT] snapshot na ativação do autonomous_total FALHOU conv=%s: %v", convID, err)
			} else {
				sessionModeSetCheckpoint(convID, tenantID, userID, id)
			}
		}
	}
	log.Printf("[AUDIT] session/mode set conv=%s tenant=%s mode=%s budget=%d auto_rollback=%v", convID, tenantID, body.Mode, budget, autoRollback)
	json.NewEncoder(w).Encode(map[string]interface{}{"conv_id": convID, "mode": body.Mode, "autonomous_budget": budget, "auto_rollback": autoRollback})
}