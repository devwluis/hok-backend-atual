package main

import (
	"net/http"
	"time"
)

// ── GET /agents ─────────────────────────────────────────────────
// Agentes reais do backend: os monitores de trigger em runtime.
// Somente leitura — não há mecanismo para pausar/disparar
// individualmente; a UI marca as ações como não suportadas.
func handleAgents(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	type agentInfo struct {
		Name      string `json:"name"`
		Desc      string `json:"desc"`
		Status    string `json:"status"`
		LastFired string `json:"last_fired"`
		LastMsg   string `json:"last_msg"`
	}

	defs := []struct {
		id   string
		name string
		desc string
	}{
		{"on_disk", "Monitor de Disco", "Dispara alerta quando o uso de disco passa de 80% (cooldown 30min)."},
		{"on_memory_insert", "Monitor de Memórias", "Observa inserções de memória persistente (cooldown 5min)."},
		{"on_error_detected", "Monitor de Erros", "Detecta erros em execuções recentes (cooldown 2min)."},
	}

	triggerMu.Lock()
	agents := make([]map[string]interface{}, 0, len(defs))
	for _, d := range defs {
		st := triggerStates[d.id]
		lastFired := ""
		lastMsg := ""
		if st != nil {
			if !st.lastFired.IsZero() {
				lastFired = st.lastFired.Format(time.RFC3339)
			}
			lastMsg = st.lastMsg
		}
		agents = append(agents, map[string]interface{}{
			"id":         d.id,
			"name":       d.name,
			"desc":       d.desc,
			"status":     "running",
			"last_fired": lastFired,
			"last_msg":   lastMsg,
		})
	}
	triggerMu.Unlock()

	respondJSON(w, map[string]interface{}{"status": "ok", "agents": agents})
}
