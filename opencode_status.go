package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Limites publicados do plano OpenCode Go (docs opencode.ai/go):
// 5h = US$12 · semanal = US$30 · mensal = US$60.
var openCodeGoLimitsDollars = map[string]float64{
	"rolling": 12,
	"weekly":  30,
	"monthly": 60,
}

type openCodeGoUsageResponse struct {
	Usage struct {
		Rolling *openCodeGoWindow `json:"rolling"`
		Weekly  *openCodeGoWindow `json:"weekly"`
		Monthly *openCodeGoWindow `json:"monthly"`
	} `json:"usage"`
}

type openCodeGoWindow struct {
	Status   string  `json:"status"`
	Percent  float64 `json:"percent"`
	ResetsAt string  `json:"resetsAt"`
}

// openCodeAPIKey resolve a chave da assinatura OpenCode Go:
// env OPENCODE_API_KEY tem prioridade; senão lê o auth.json do CLI
// (~/.local/share/opencode/auth.json) — campos "opencode-go" e "opencode".
func openCodeAPIKey() (string, error) {
	if v := os.Getenv("OPENCODE_API_KEY"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(home, ".local", "share", "opencode", "auth.json"))
	if err != nil {
		return "", fmt.Errorf("auth.json nao encontrado: %v", err)
	}
	var data map[string]map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", fmt.Errorf("auth.json invalido: %v", err)
	}
	for _, key := range []string{"opencode-go", "opencode"} {
		if entry, ok := data[key]; ok {
			if v, ok := entry["key"].(string); ok && v != "" {
				return v, nil
			}
		}
	}
	return "", fmt.Errorf("nenhuma chave opencode encontrada em auth.json")
}

// handleOpenCodeStatus — GET /opencode/status (protegido por requireHokAuth):
// consulta a API oficial do OpenCode Go e normaliza o consumo por janela,
// enriquecendo com os limites em dólares do plano.
func handleOpenCodeStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	apiKey, err := openCodeAPIKey()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, "chave OpenCode nao encontrada: "+err.Error()), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest("GET", "https://opencode.ai/zen/go/v1/usage", nil)
	if err != nil {
		http.Error(w, `{"error":"falha ao montar request OpenCode"}`, http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, `{"error":"falha ao consultar OpenCode Go"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, `{"error":"falha ao ler resposta OpenCode Go"}`, http.StatusBadGateway)
		return
	}
	if resp.StatusCode == 401 {
		http.Error(w, `{"error":"chave OpenCode invalida ou revogada (401)","code":"invalid_key"}`, http.StatusBadGateway)
		return
	}
	if resp.StatusCode == 404 {
		http.Error(w, `{"error":"sem assinatura OpenCode Go ativa (404)","code":"no_subscription"}`, http.StatusBadGateway)
		return
	}
	if resp.StatusCode != 200 {
		http.Error(w, fmt.Sprintf(`{"error":"OpenCode Go respondeu %d"}`, resp.StatusCode), http.StatusBadGateway)
		return
	}

	var data openCodeGoUsageResponse
	if err := json.Unmarshal(body, &data); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, "resposta invalida do OpenCode Go"), http.StatusBadGateway)
		return
	}

	result := map[string]interface{}{
		"status":      "ok",
		"plan":        "go",
		"subscribed":  data.Usage.Monthly != nil,
		"rolling":     enrichOpenCodeWindow(data.Usage.Rolling, "rolling"),
		"weekly":      enrichOpenCodeWindow(data.Usage.Weekly, "weekly"),
		"monthly":     enrichOpenCodeWindow(data.Usage.Monthly, "monthly"),
	}
	if data.Usage.Rolling == nil && data.Usage.Weekly == nil && data.Usage.Monthly == nil {
		http.Error(w, `{"error":"sem assinatura OpenCode Go ativa","code":"no_subscription"}`, http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func enrichOpenCodeWindow(win *openCodeGoWindow, key string) map[string]interface{} {
	if win == nil {
		return nil
	}
	limit := openCodeGoLimitsDollars[key]
	return map[string]interface{}{
		"status":       win.Status,
		"percent":      win.Percent,
		"resetsAt":     win.ResetsAt,
		"limitDollars": limit,
		"usedDollars":  round2(limit * win.Percent / 100),
	}
}

func round2(v float64) float64 {
	return float64(int(v*100+0.5)) / 100
}