package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ModelInfo descreve um modelo exposto no seletor frontend.
type ModelInfo struct {
	ID         string   `json:"id"`             // ex: deepseek/deepseek-chat-v3.1
	Provider   string   `json:"provider"`         // ex: OpenRouter Free, DeepSeek Oficial
	Free       bool     `json:"free"`             // true gratuito, false pago
	Compatible *bool    `json:"compatible"`       // true validado para os 4 motores; null nao validado
	Name       string   `json:"name"`             // nome amigavel
	Active     bool     `json:"active"`           // este e o modelo ativo
	Source     string   `json:"source"`           // openrouter_free | deepseek_official
	Label      string   `json:"label"`            // nome amigavel (compatibilidade frontend)
	Tags       []string `json:"tags,omitempty"`   // tags para busca frontend
}

// Cache para modelos free OpenRouter (API direta, pricing==0).
var (
	cachedOpenRouterFreeModels    []ModelInfo
	cachedOpenRouterFreeErr       error
	openRouterFreeMu              sync.Mutex
	openRouterFreeAt              time.Time
	openRouterFreeTTL             = 20 * time.Minute
)

// fetchOpenRouterFreeModels consulta a API oficial da OpenRouter e filtra
// SOMENTE modelos onde pricing.prompt=="0" E pricing.completion=="0"
// (confirmado pela API, nao por nome/sufixo). Cada modelo recebe
// source="openrouter_free".
func fetchOpenRouterFreeModels() ([]ModelInfo, error) {
	req, err := http.NewRequest("GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("OpenRouter request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+OR_KEY)
	req.Header.Set("HTTP-Referer", "https://hokma.ai")
	req.Header.Set("X-Title", "Hokma")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenRouter request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenRouter API error %d: %s", resp.StatusCode, string(body))
	}

	var respData struct {
		Data []struct {
			ID            string `json:"id"`
			Name          string `json:"name"`
			Pricing       struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
			ContextLength int `json:"context_length"`
			Architecture  struct {
				Modality string `json:"modality"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("OpenRouter unmarshal failed: %w", err)
	}

	var models []ModelInfo
	for _, m := range respData.Data {
		isFree := m.Pricing.Prompt == "0" && m.Pricing.Completion == "0"
		if !isFree {
			continue
		}
		models = append(models, ModelInfo{
			ID:       m.ID,
			Provider: "OpenRouter Free",
			Free:     true,
			Name:     m.Name,
			Label:    m.Name,
			Source:   "openrouter_free",
			Tags:     []string{"openrouter", "free", "gratuito", strings.ToLower(m.ID)},
		})
	}
	return models, nil
}

// refreshOpenRouterFreeModels atualiza o cache de modelos free OpenRouter.
func refreshOpenRouterFreeModels() {
	openRouterFreeMu.Lock()
	defer openRouterFreeMu.Unlock()
	models, err := fetchOpenRouterFreeModels()
	openRouterFreeAt = time.Now()
	if err != nil {
		cachedOpenRouterFreeErr = err
		return
	}
	cachedOpenRouterFreeErr = nil
	cachedOpenRouterFreeModels = models
	log.Printf("[models/available] OpenRouter Free: %d modelos (cache %.0fmin)", len(models), float64(openRouterFreeTTL)/60)
}

// getDeepSeekOfficialModels retorna os modelos DeepSeek oficiais
// (api.deepseek.com via DEEPSEEK_API_KEY) como categoria separada.
// source="deepseek_official". Estes sao PAGOS.
func getDeepSeekOfficialModels() []ModelInfo {
	active := getActiveModel()
	return []ModelInfo{
		{
			ID:       "deepseek-native/deepseek-flash",
			Provider: "DeepSeek Oficial",
			Free:     false,
			Name:     "DeepSeek V4 Flash",
			Label:    "DeepSeek V4 Flash",
			Active:   "deepseek-native/deepseek-flash" == active,
			Source:   "deepseek_official",
			Tags:     []string{"deepseek", "deepseek-native", "nativo", "pago", "paid", "flash"},
		},
		{
			ID:       "deepseek-native/deepseek-v4-pro",
			Provider: "DeepSeek Oficial",
			Free:     false,
			Name:     "DeepSeek V4 Pro",
			Label:    "DeepSeek V4 Pro",
			Active:   "deepseek-native/deepseek-v4-pro" == active,
			Source:   "deepseek_official",
			Tags:     []string{"deepseek", "deepseek-native", "nativo", "pago", "paid", "pro"},
		},
	}
}

// handleModelsAvailable devolve a lista de modelos dinâmica, gerada a
// partir de fontes oficiais (nao lista estatica):
//   - OpenRouter Free: GET https://openrouter.ai/api/v1/models, filtrados
//     pricing.prompt==0 E pricing.completion==0 (confirmado pela API).
//     source="openrouter_free"
//   - DeepSeek Oficial: modelos deepseek-native/* via api.deepseek.com.
//     source="deepseek_official"
// Cada modelo inclui o campo "source" indicando a origem.
func handleModelsAvailable(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	if len(cachedOpenRouterFreeModels) == 0 && cachedOpenRouterFreeErr == nil || time.Since(openRouterFreeAt) > openRouterFreeTTL {
		refreshOpenRouterFreeModels()
	}

	type providerGroup struct {
		Provider string      `json:"provider"`
		Models   []ModelInfo `json:"models"`
	}

	groups := []providerGroup{}

	// DeepSeek Oficial (pago) — sempre presente
	dsModels := getDeepSeekOfficialModels()
	if len(dsModels) > 0 {
		groups = append(groups, providerGroup{Provider: "DeepSeek Oficial", Models: dsModels})
	}

	// OpenRouter Free (API real, pricing==0)
	openRouterFreeMu.Lock()
	orModels := cachedOpenRouterFreeModels
	orErr := cachedOpenRouterFreeErr
	openRouterFreeMu.Unlock()

	if len(orModels) > 0 {
		groups = append(groups, providerGroup{Provider: "OpenRouter Free", Models: orModels})
	}

	resp := map[string]interface{}{
		"status":    "ok",
		"active":    getActiveModel(),
		"modelA":    ModelA,
		"modelB":    ModelB,
		"providers": groups,
	}
	if orErr != nil {
		resp["warning"] = orErr.Error()
		log.Printf("[models/available] erro OpenRouter Free (cache stale): %v", orErr)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
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
