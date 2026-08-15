package main

import (
	"net/http"
)

// ── GET /flows ─────────────────────────────────────────────────
// Lista flows disponíveis reaproveitando o módulo n8n existente.
// Mapeia workflows do n8n para o formato esperado pela tela Flow Builder
// (name, steps, status). Sem POST — criar/executar flows requer
// confirmação de fluxo de aprovação com o usuário.
func handleFlows(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != "GET" {
		jsonError(w, "method not allowed", 405)
		return
	}

	items := fetchN8nWorkflowsItems()

	flows := make([]map[string]interface{}, 0, len(items))
	for _, wf := range items {
		name, _ := wf["name"].(string)
		steps, _ := wf["steps"].(int)
		if steps == 0 {
			steps = 1
		}

		status := "draft"
		if active, ok := wf["active"].(bool); ok && active {
			status = "active"
		} else if activeStr, ok := wf["active"].(string); ok && activeStr == "true" {
			status = "active"
		}

		flows = append(flows, map[string]interface{}{
			"name":   name,
			"steps":  steps,
			"status": status,
		})
	}

	if flows == nil {
		flows = []map[string]interface{}{}
	}

	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"flows":  flows,
	})
}