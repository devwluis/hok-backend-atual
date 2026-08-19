package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// ModelCatalogItem representa um modelo do catálogo unificado
type ModelCatalogItem struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	Provider    string  `json:"provider"`     // "OpenRouter" | "OpenCode Zen"
	Free        bool    `json:"free"`
	Compatible  *bool   `json:"compatible"`   // true validado, false invalidado, null não testado
	Active      bool    `json:"active"`
}

// ModelCatalogResponse resposta do endpoint /models/catalog
type ModelCatalogResponse struct {
	Status     string              `json:"status"`
	Active     string              `json:"active"`
	ModelA     string              `json:"modelA"`
	ModelB     string              `json:"modelB"`
	Providers  []ProviderGroup     `json:"providers"`
	CachedAt   string              `json:"cachedAt"`
	TotalCount int                 `json:"totalCount"`
	FreeCount  int                 `json:"freeCount"`
	PaidCount  int                 `json:"paidCount"`
	Warning    string              `json:"warning,omitempty"`
}

type ProviderGroup struct {
	Provider string              `json:"provider"`
	Models   []ModelCatalogItem  `json:"models"`
}

var (
	catalogCache      []ModelCatalogItem
	catalogCacheMutex sync.RWMutex
	catalogCachedAt   time.Time
	catalogCacheErr   error
	cacheTTL          = 5 * time.Minute
)

// OpenRouterModel estrutura da resposta da API OpenRouter
type OpenRouterModel struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Pricing     struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
		Image      string `json:"image"`
	} `json:"pricing"`
	ContextLength int    `json:"context_length"`
	Architecture  struct {
		Modality string `json:"modality"`
	} `json:"architecture"`
}

type OpenRouterResponse struct {
	Data []OpenRouterModel `json:"data"`
}

// OpenCodeZenModel estrutura da resposta da API OpenCode Zen
type OpenCodeZenModel struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
}

type OpenCodeZenResponse struct {
	Object string              `json:"object"`
	Data   []OpenCodeZenModel `json:"data"`
}

// fetchOpenRouterModels busca modelos da API OpenRouter
func fetchOpenRouterModels() ([]ModelCatalogItem, error) {
	url := "https://openrouter.ai/api/v1/models"
	req, _ := http.NewRequest("GET", url, nil)
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

	var respData OpenRouterResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("OpenRouter unmarshal failed: %w", err)
	}

	var models []ModelCatalogItem
	for _, m := range respData.Data {
		// Determina se é free baseado no pricing
		isFree := m.Pricing.Prompt == "0" && m.Pricing.Completion == "0"
		
		// Nome amigável
		label := m.Name
		if label == "" {
			label = m.ID
		}
		
		models = append(models, ModelCatalogItem{
			ID:       m.ID,
			Label:    label,
			Provider: "OpenRouter",
			Free:     isFree,
			Compatible: nil, // não validado por padrão
		})
	}
	return models, nil
}

// fetchOpenCodeZenModels busca modelos da API OpenCode Zen
func fetchOpenCodeZenModels() ([]ModelCatalogItem, error) {
	url := "https://opencode.ai/zen/v1/models"
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("OpenCode Zen request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenCode Zen API error %d: %s", resp.StatusCode, string(body))
	}

	var respData OpenCodeZenResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("OpenCode Zen unmarshal failed: %w", err)
	}

	var models []ModelCatalogItem
	for _, m := range respData.Data {
		// OpenCode Zen models are all free (Zen models)
		label := m.ID
		
		models = append(models, ModelCatalogItem{
			ID:       m.ID,
			Label:    label,
			Provider: "OpenCode Zen",
			Free:     true, // Zen models are free
			Compatible: nil,
		})
	}
	return models, nil
}

// mergeModels mescla modelos de ambas as fontes, deduplica e ordena
func mergeModels(orModels, zenModels []ModelCatalogItem) []ModelCatalogItem {
	seen := make(map[string]bool)
	var merged []ModelCatalogItem
	
	// Ordem de prioridade: OpenCode Zen primeiro, depois OpenRouter
	all := append(zenModels, orModels...)
	
	for _, m := range all {
		key := m.ID
		if seen[key] {
			continue // duplicata exata - mantém o primeiro (Zen tem prioridade)
		}
		seen[key] = true
		
		// Verifica se está validado nos motores
		var compat *bool
		if _, ok := validatedModels[m.ID]; ok {
			c := validatedModels[m.ID]
			compat = &c
		}
		
		merged = append(merged, ModelCatalogItem{
			ID:        m.ID,
			Label:     m.Label,
			Provider:  m.Provider,
			Free:      m.Free,
			Compatible: compat,
		})
	}
	
	return merged
}

// refreshCatalog atualiza o cache do catálogo
func refreshCatalog() error {
	log.Printf("[catalog] Iniciando refresh do catálogo de modelos...")
	
	// Busca em paralelo
	var orModels, zenModels []ModelCatalogItem
	var orErr, zenErr error
	var wg sync.WaitGroup
	
	wg.Add(2)
	go func() {
		defer wg.Done()
		orModels, orErr = fetchOpenRouterModels()
	}()
	go func() {
		defer wg.Done()
		zenModels, zenErr = fetchOpenCodeZenModels()
	}()
	wg.Wait()
	
	if orErr != nil {
		log.Printf("[catalog] OpenRouter erro: %v", orErr)
	}
	if zenErr != nil {
		log.Printf("[catalog] OpenCode Zen erro: %v", zenErr)
	}
	
	// Se ambos falharam, retorna erro
	if orErr != nil && zenErr != nil {
		return fmt.Errorf("ambas APIs falharam: OR=%v, Zen=%v", orErr, zenErr)
	}
	
	// Se uma falhou, usa apenas a outra
	if orErr != nil {
		log.Printf("[catalog] Usando apenas OpenCode Zen (OpenRouter falhou)")
	} else if zenErr != nil {
		log.Printf("[catalog] Usando apenas OpenRouter (OpenCode Zen falhou)")
	}
	
	// Mescla
	merged := mergeModels(orModels, zenModels)
	
	// Atualiza cache
	catalogCacheMutex.Lock()
	catalogCache = merged
	catalogCachedAt = time.Now()
	catalogCacheErr = nil
	catalogCacheMutex.Unlock()
	
	// Log estatísticas
	freeCount := 0
	paidCount := 0
	for _, m := range merged {
		if m.Free {
			freeCount++
		} else {
			paidCount++
		}
	}
	log.Printf("[catalog] Refresh concluído: %d modelos (%d free, %d pagos), cachedAt=%v", 
		len(merged), freeCount, paidCount, catalogCachedAt.Format(time.RFC3339))
	
	return nil
}

// getCatalog retorna o catálogo (do cache ou refresh se expirado)
func getCatalog() ([]ModelCatalogItem, error) {
	catalogCacheMutex.RLock()
	cached := len(catalogCache) > 0
	expired := time.Since(catalogCachedAt) > cacheTTL
	catalogCacheMutex.RUnlock()
	
	if !cached || expired {
		if err := refreshCatalog(); err != nil {
			catalogCacheMutex.RLock()
			defer catalogCacheMutex.RUnlock()
			if len(catalogCache) == 0 {
				return nil, err
			}
			// Retorna cache stale se disponível
			return catalogCache, err
		}
	}
	
	catalogCacheMutex.RLock()
	defer catalogCacheMutex.RUnlock()
	return catalogCache, nil
}

// buildProviderGroups agrupa modelos por provider
func buildProviderGroups(models []ModelCatalogItem) []ProviderGroup {
	byProvider := make(map[string][]ModelCatalogItem)
	for _, m := range models {
		byProvider[m.Provider] = append(byProvider[m.Provider], m)
	}
	
	groups := []ProviderGroup{}
	// Ordem: OpenCode Zen, Google, OpenRouter, outros
	order := []string{"OpenCode Zen", "Google", "OpenRouter", "OpenAI", "Anthropic", "Meta", "Mistral", "Cohere", "Qwen", "DeepSeek", "MiniMax", "GLM", "Kimi", "Muse", "Nemotron", "Jamba", "Mistral", "Qwen", "GLM", "Kimi", "Muse"}
	seen := make(map[string]bool)
	
	for _, p := range order {
		if ms, ok := byProvider[p]; ok && len(ms) > 0 {
			groups = append(groups, ProviderGroup{Provider: p, Models: ms})
			seen[p] = true
		}
	}
	for p, ms := range byProvider {
		if !seen[p] && len(ms) > 0 {
			groups = append(groups, ProviderGroup{Provider: p, Models: ms})
		}
	}
	return groups
}

// handleModelsCatalog endpoint unificado do catálogo
func handleModelsCatalog(w http.ResponseWriter, r *http.Request) {
	log.Printf("[catalog] handleModelsCatalog called: %s %s", r.Method, r.URL.Path)
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return
	}
	
	// ?force=1 → refaz o refresh imediatamente (novos modelos do OpenCode
	// aparecem na hora, sem esperar o TTL de 5 min).
	if r.URL.Query().Get("force") == "1" {
		if err := refreshCatalog(); err != nil {
			log.Printf("[catalog] force refresh erro: %v", err)
		}
	}

	models, err := getCatalog()
	if err != nil {
		log.Printf("[catalog] Erro ao buscar catálogo: %v", err)
	}
	
	// Contagem
	freeCount := 0
	paidCount := 0
	catalogCacheMutex.RLock()
	modelsList := catalogCache
	catalogCacheMutex.RUnlock()
	for _, m := range modelsList {
		if m.Free {
			freeCount++
		} else {
			paidCount++
		}
	}
	
	resp := ModelCatalogResponse{
		Status:     "ok",
		Active:     getActiveModel(),
		ModelA:     ModelA,
		ModelB:     ModelB,
		Providers:  buildProviderGroups(models),
		CachedAt:   catalogCachedAt.Format(time.RFC3339),
		TotalCount: len(models),
		FreeCount:  freeCount,
		PaidCount:  paidCount,
	}
	if catalogCacheErr != nil {
		resp.Warning = catalogCacheErr.Error()
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// Inicialização do catálogo na inicialização do servidor
func initCatalog() {
	go func() {
		time.Sleep(2 * time.Second) // Aguarda servidor subir
		if err := refreshCatalog(); err != nil {
			log.Printf("[catalog] Erro inicial: %v", err)
		}
		// Background refresh a cada 5 minutos (mantém os modelos do
		// OpenCode/OpenRouter sempre atualizados)
		ticker := time.NewTicker(5 * time.Minute)
		go func() {
			for range ticker.C {
				refreshCatalog()
			}
		}()
	}()
}
