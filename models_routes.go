package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ModelInfo descreve um modelo exposto no seletor frontend.
type ModelInfo struct {
	ID          string  `json:"id"`            // ex: deepseek/deepseek-chat-v3.1
	Provider    string  `json:"provider"`      // ex: OpenRouter, OpenCode Zen, Google
	Free        bool    `json:"free"`          // true gratuito/zen, false pago
	Compatible  *bool   `json:"compatible"`    // true validado para os 4 motores; null nao validado
	Name        string  `json:"name"`          // nome amigavel
	Active      bool    `json:"active"`        // este e o modelo ativo
}

// categorizeModel agrupa um ID de modelo pela provider/categoria.
func categorizeModel(modelID string) (provider, name string, free bool) {
	switch {
	case strings.HasPrefix(modelID, "deepseek/deepseek-chat"): // gratuito/zen
		return "OpenCode Zen", "DeepSeek Chat v3.1", true
	case strings.HasPrefix(modelID, "deepseek/deepseek-r1") || strings.HasPrefix(modelID, "deepseek/deepseek-v4"):
		return "OpenRouter", "DeepSeek", true
	case strings.HasPrefix(modelID, "google/gemini"):
		return "Google", "Gemini " + strings.TrimPrefix(modelID, "google/"), false
	case strings.HasPrefix(modelID, "anthropic/claude"):
		return "OpenRouter", "Claude", false
	case strings.HasPrefix(modelID, "openai/gpt"):
		return "OpenAI", "GPT", false
	case strings.HasPrefix(modelID, "meta-llama"):
		return "OpenRouter", "Llama", false
	case strings.HasPrefix(modelID, "cohere"), strings.HasPrefix(modelID, "mistral"), strings.HasPrefix(modelID, "qwen"):
		return "OpenRouter", strings.Split(modelID, "/")[0], false
	default:
		prov := "OpenRouter"
		if idx := strings.Index(modelID, "/"); idx > 0 {
			prov = strings.Title(strings.Split(modelID, "/")[0])
		}
		return prov, modelID, false
	}
}

// fetchOpenCodeModels roda `opencode models` (cache em memoria com TTL) e
// devolve a lista bruta. A busca por modelos novos fica automática: o cache
// expira em 10 min e o próximo GET /models/available re-roda o CLI opencode.
var (
	cachedOpenCodeModels    []ModelInfo
	cachedOpenCodeModelsErr error
	openCodeModelsMu        sync.Mutex
	openCodeModelsAt        time.Time
	openCodeModelsTTL       = 10 * time.Minute
)

func refreshOpenCodeModels() {
	openCodeModelsMu.Lock()
	defer openCodeModelsMu.Unlock()
	out, err := exec.Command(opencodeBinary, "models", "openrouter").CombinedOutput()
	if err != nil {
		cachedOpenCodeModelsErr = fmt.Errorf("opencode models: %w — %s", err, string(out))
		return
	}
	openCodeModelsAt = time.Now()
	cachedOpenCodeModelsErr = nil
	active := getActiveModel()
	rows := strings.Split(strings.TrimSpace(string(out)), "\n")
	list := []ModelInfo{}
	for _, r := range rows {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		prov, name, free := categorizeModel(r)
		compat := validatedModels[r] // true/false se validado; ausente -> null
		var cptr *bool
		if _, ok := validatedModels[r]; ok {
			cptr = &compat
		}
		list = append(list, ModelInfo{
			ID:         r,
			Provider:   prov,
			Free:       free,
			Compatible: cptr,
			Name:       name,
			Active:     r == active,
		})
	}
	cachedOpenCodeModels = list
}

// handleModelsAvailable devolve a lista de modelos agrupados por provider,
// com flags free/compatible e o modelo ativo. Serve o seletor vertical do frontend.
func handleModelsAvailable(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if len(cachedOpenCodeModels) == 0 && cachedOpenCodeModelsErr == nil || time.Since(openCodeModelsAt) > openCodeModelsTTL {
		refreshOpenCodeModels()
	}
	// agrupa por provider
	byProvider := map[string][]ModelInfo{}
	for _, m := range cachedOpenCodeModels {
		byProvider[m.Provider] = append(byProvider[m.Provider], m)
	}
	type providerGroup struct {
		Provider string      `json:"provider"`
		Models   []ModelInfo `json:"models"`
	}
	groups := []providerGroup{}
	// ordem determinista: OpenCode Zen, Google, OpenRouter, ...
	order := []string{"OpenCode Zen", "Google", "OpenRouter", "OpenAI"}
	seen := map[string]bool{}
	for _, p := range order {
		if ms, ok := byProvider[p]; ok {
			groups = append(groups, providerGroup{Provider: p, Models: ms})
			seen[p] = true
		}
	}
	for p, ms := range byProvider {
		if seen[p] {
			continue
		}
		groups = append(groups, providerGroup{Provider: p, Models: ms})
	}
	resp := map[string]interface{}{
		"status":  "ok",
		"active":  getActiveModel(),
		"modelA":  ModelA,
		"modelB":  ModelB,
		"providers": groups,
	}
	if cachedOpenCodeModelsErr != nil {
		resp["warning"] = cachedOpenCodeModelsErr.Error()
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleModelsSelect atualiza o modelo ativo (persiste em app_settings) e invalida cache.
func handleModelsSelect(w http.ResponseWriter, r *http.Request) {
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
	var body struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "bad request"})
		return
	}
	if strings.TrimSpace(body.Model) == "" {
		respondJSON(w, map[string]string{"status": "error", "message": "modelo nao informado"})
		return
	}
	setActiveModel(strings.TrimSpace(body.Model))
	log.Printf("[AUDIT] modelo ativo atualizado para %s", body.Model)
	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"active": getActiveModel(),
	})
}
