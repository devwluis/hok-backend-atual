package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ModelCatalogItem representa um modelo do catálogo unificado
type ModelCatalogItem struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Provider    string   `json:"provider"`     // "OpenRouter" | "OpenCode Zen" | "OpenCode Go"
	Free        bool     `json:"free"`
	Tags        []string `json:"tags,omitempty"` // família/provedor normalizado + "free" quando custo zero (busca)
	Compatible  *bool    `json:"compatible"`   // true validado, false invalidado, null não testado
	Active      bool     `json:"active"`
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
	ActiveStatus string            `json:"activeStatus"` // ok | expired (sumiu do catálogo) — trava de segurança (29/08)
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
	
	// Cache separado para cada fonte com TTLs diferentes
	zenCache        []ModelCatalogItem
	zenCacheMutex   sync.RWMutex
	zenCachedAt     time.Time
	zenCacheTTL     = 1 * time.Hour // OpenCode Zen: TTL 1h (sincronização automática — nunca hardcode)
	
	goCache         []ModelCatalogItem
	goCacheMutex    sync.RWMutex
	goCachedAt      time.Time
	goCacheTTL      = 24 * time.Hour // OpenCode Go: TTL 24h
	
	openRouterCache []ModelCatalogItem
	openRouterCacheMutex sync.RWMutex
	openRouterCachedAt   time.Time
	openRouterCacheTTL   = 6 * time.Hour // OpenRouter: TTL 6h

	// AIHubMix: gateway OpenAI-compatible. Fonte nova (31/08): a API
	// /api/v1/models?type=llm retorna pricing real por modelo (input/output
	// em USD por 1K tokens) — free = input==0 && output==0. TTL 1h
	// (sincronização automática, nunca hardcode).
	aihubmixCache        []ModelCatalogItem
	aihubmixCacheMutex   sync.RWMutex
	aihubmixCachedAt     time.Time
	aihubmixCacheTTL     = 1 * time.Hour

	// Fonte OpenCode CLI (`opencode models`): descoberta automática de
	// modelos novos direto do binário do opencode (gateway opencode/,
	// opencode-go/ e google/). TTL 12h — a lista muda pouco e o exec é lento.
	cliCache      []ModelCatalogItem
	cliCacheMutex sync.RWMutex
	cliCachedAt   time.Time
	cliCacheTTL   = 12 * time.Hour
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

// OpenCodeModel estrutura da resposta da API OpenCode (Zen e Go)
type OpenCodeModel struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created"`
	OwnedBy     string `json:"owned_by"`
	Name        string `json:"name,omitempty"`
	Free        bool   `json:"free,omitempty"`
	Pricing     struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing,omitempty"`
}

type OpenCodeResponse struct {
	Object string             `json:"object"`
	Data   []OpenCodeModel `json:"data"`
}

// AIHubMixModel — modelo da API NOVA do AIHubMix (GET /api/v1/models).
// Diferente do /v1/models legado (que só tem id/owned_by), a API nova
// retorna pricing REAL por modelo — usado para marcar free (input==0 &&
// output==0) sem suposição de sufixo.
type AIHubMixModel struct {
	ModelID       string `json:"model_id"`
	ModelName     string `json:"model_name"`
	DeveloperID   int    `json:"developer_id"`
	Desc          string `json:"desc"`
	Types         string `json:"types"`          // "llm", "image_generation", "tts", ...
	Features      string `json:"features"`       // "thinking,tools,function_calling,..."
	InputModalities string `json:"input_modalities"`
	Endpoints     string `json:"endpoints"`
	MaxOutput     int    `json:"max_output"`
	ContextLength int    `json:"context_length"`
	Pricing       struct {
		Input    float64 `json:"input"`
		Output   float64 `json:"output"`
		CacheRead float64 `json:"cache_read"`
		CacheWrite float64 `json:"cache_write"`
	} `json:"pricing"`
}

type AIHubMixResponse struct {
	Success bool             `json:"success"`
	Message string           `json:"message"`
	Data    []AIHubMixModel  `json:"data"`
}

// modelTags monta tags de busca para um modelo: nome/label + id normalizado +
// família (segmento antes de "/" para ids OpenRouter, ex: "deepseek") + "free"
// quando o custo é zero. Usado pelo filtro client-side (digitar "deepseek",
// "claude", "free", etc. casa com o modelo).
func modelTags(id, label, provider string, free bool) []string {
	seen := make(map[string]bool)
	tags := []string{}
	add := func(s string) {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" && !seen[s] {
			seen[s] = true
			tags = append(tags, s)
		}
	}
	add(label)
	add(id)
	add(provider)
	// família: parte antes da primeira "/" (ex: "deepseek/deepseek-chat" → "deepseek")
	if i := strings.Index(id, "/"); i > 0 {
		add(id[:i])
	}
	if i := strings.Index(label, "/"); i > 0 {
		add(label[:i])
	}
	if free {
		add("free")
		add("gratuito")
	}
	return tags
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
			Tags:     modelTags(m.ID, label, "OpenRouter", isFree),
			Compatible: nil, // não validado por padrão
		})
	}

	return models, nil
}

// fetchOpenCodeGoModels busca modelos do catálogo OpenCode Go
func fetchOpenCodeGoModels() ([]ModelCatalogItem, error) {
	url := "https://opencode.ai/zen/go/v1/models"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+OR_KEY)
	req.Header.Set("HTTP-Referer", "https://hokma.ai")
	req.Header.Set("X-Title", "Hokma")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OpenCode Go request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("OpenCode Go API error %d: %s", resp.StatusCode, string(body))
	}

	var respData OpenCodeResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("OpenCode Go unmarshal failed: %w", err)
	}

	var models []ModelCatalogItem
	for _, m := range respData.Data {
	// OpenCode Go é o tier gratuito do opencode: considera free por padrão,
	// só vira pago se a API trouxer pricing explicitamente não-zero.
	isFree := true
	if m.Pricing.Prompt != "" && m.Pricing.Completion != "" &&
		!(m.Pricing.Prompt == "0" && m.Pricing.Completion == "0") {
		isFree = false
	}
		
	// Nome amigável
	label := m.Name
	if label == "" {
		label = m.ID
	}
		
	models = append(models, ModelCatalogItem{
		ID:       "opencode-go/" + m.ID,
		Label:    label,
		Provider: "OpenCode Go",
		Free:     isFree,
		Tags:     modelTags("opencode-go/"+m.ID, label, "OpenCode Go", isFree),
		Compatible: nil,
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

	var respData OpenCodeResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("OpenCode Zen unmarshal failed: %w", err)
	}

	var models []ModelCatalogItem
	for _, m := range respData.Data {
		// Zen: o próprio catálogo expõe free/pricing por modelo — não assumir
		// que TODOS são gratuitos (a Zen tem modelos pagos e gratuitos).
		// FIX 29/08: a API Zen NÃO retorna o campo free/pricing — o sufixo
		// "-free" no ID marca a variante gratuita PROMOCIONAL (ex:
		// mimo-v2.5-free, deepseek-v4-flash-free — "grátis durante o período
		// promocional", podem virar pagos/sair do ar sem aviso).
		isFree := m.Free || (m.Pricing.Prompt == "0" && m.Pricing.Completion == "0") ||
			strings.HasSuffix(m.ID, "-free")
		label := m.Name
		if label == "" {
			label = m.ID
		}

		// FIX 29/08: o formato correto do ID no config do opencode é sempre
		// "opencode/<model-id>" (ex: opencode/gpt-5.5) — o ID cru da API Zen
		// (sem prefixo) era inválido quando repassado aos motores.
		id := "opencode/" + m.ID
		models = append(models, ModelCatalogItem{
			ID:       id,
			Label:    label,
			Provider: "OpenCode Zen",
			Free:     isFree,
			Tags:     modelTags(id, label, "OpenCode Zen", isFree),
			Compatible: nil,
		})
	}
	return models, nil
}

// fetchAIHubMixModels busca modelos da API NOVA do AIHubMix
// (https://aihubmix.com/api/v1/models?type=llm). A API retorna pricing real
// (input/output em USD por 1K tokens) por modelo — free é marcado pelo
// pricing (input==0 && output==0), NÃO por suposição de sufixo. Filtra
// type=llm para trazer só modelos de chat (descarta image_generation/tts/
// stt/embedding/rerank/video).
func fetchAIHubMixModels() ([]ModelCatalogItem, error) {
	url := "https://aihubmix.com/api/v1/models?type=llm"
	req, _ := http.NewRequest("GET", url, nil)
	// A listagem é pública (grupo default), mas se houver key configurada
	// envia Bearer para ver a lista do próprio token (respeita o grupo real).
	if k := os.Getenv("AIHUBMIX_API_KEY"); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	req.Header.Set("HTTP-Referer", "https://hokma.ai")
	req.Header.Set("X-Title", "Hokma")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AIHubMix request failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("AIHubMix API error %d: %s", resp.StatusCode, string(body))
	}

	var respData AIHubMixResponse
	if err := json.Unmarshal(body, &respData); err != nil {
		return nil, fmt.Errorf("AIHubMix unmarshal failed: %w", err)
	}
	if !respData.Success {
		return nil, fmt.Errorf("AIHubMix API success=false: %s", respData.Message)
	}

	var models []ModelCatalogItem
	for _, m := range respData.Data {
		// Só LLMs de chat (type=llm já filtra, mas reforça defensivamente).
		if !strings.Contains(m.Types, "llm") {
			continue
		}
		// Skip do router "auto" (AIHubMix Smart Router) e de variantes
		// internas de canal (prefixos aihubmix-/cc- com preço oculto).
		if m.ModelID == "auto" || strings.HasPrefix(m.ModelID, "aihubmix-") {
			continue
		}
		// free REAL pelo pricing (input==0 && output==0). Não usa sufixo.
		isFree := m.Pricing.Input == 0 && m.Pricing.Output == 0

		label := m.ModelName
		if label == "" {
			label = m.ModelID
		}

		models = append(models, ModelCatalogItem{
			ID:       "aihubmix/" + m.ModelID,
			Label:    label,
			Provider: "AIHubMix",
			Free:     isFree,
			Tags:     modelTags("aihubmix/"+m.ModelID, label, "AIHubMix", isFree),
			Compatible: nil,
		})
	}

	return models, nil
}

// fetchOpenCodeCLIModels roda `opencode models` (binário local) e converte a
// saída (uma linha "provider/id" por modelo) em itens do catálogo. É a busca
// automática por modelos novos direto com opencode: o que o CLI conhece que
// as APIs Zen/Go/OpenRouter ainda não listam entra no catálogo do chat HOK.
// Linhas "openrouter/" são ignoradas (a fonte OpenRouter API já cobre com
// pricing); "opencode/", "opencode-go/" e "google/" viram Zen/Go/Google.
func fetchOpenCodeCLIModels() ([]ModelCatalogItem, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, opencodeBinary, "models")
	// O provedor google do opencode CLI lê GEMINI_API_KEY; o backend usa
	// GEMINI_KEY no .env — injeta um no lugar do outro (valor nunca logado).
	if gk := os.Getenv("GEMINI_KEY"); gk != "" {
		cmd.Env = append(os.Environ(), "GEMINI_API_KEY="+gk)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("opencode models: %w — %s", err, string(out))
	}

	var models []ModelCatalogItem
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "openrouter/") {
			continue
		}
		provTag, id, ok := strings.Cut(line, "/")
		id = strings.TrimSpace(id)
		if !ok || id == "" || strings.HasPrefix(id, "~") {
			continue
		}

		provider := ""
		isFree := false
		switch provTag {
		case "opencode":
			// FIX 29/08: formato oficial "opencode/<model-id>" (ex:
			// opencode/mimo-v2.5-free) — consistente com a fonte da API Zen
			// e com o que o CLI aceita via --model. Antes o id ficava sem o
			// prefixo, inválido fora do opencode.
			provider = "OpenCode Zen"
			id = "opencode/" + id
		case "opencode-go":
			// Mantém o prefixo "opencode-go/" no id: é o formato aceito pelo
			// CLI opencode (--model opencode-go/<id>) e deduplica com a fonte
			// da API Go. Tier gratuito do opencode.
			provider, isFree = "OpenCode Go", true
			id = "opencode-go/" + id
		case "google":
			provider = "Google"
			id = "google/" + id // mantém formato OpenRouter pra dedupe/uso real
		default:
			continue
		}

		models = append(models, ModelCatalogItem{
			ID:         id,
			Label:      id,
			Provider:   provider,
			Free:       isFree,
			Tags:       modelTags(id, id, provider, isFree),
			Compatible: nil, // não validado por padrão
		})
	}
	log.Printf("[catalog] OpenCode CLI: %d modelos descobertos via `opencode models`", len(models))
	return models, nil
}

// mergeModels mescla modelos das 5 fontes (Zen, Go, OpenRouter, AIHubMix,
// OpenCode CLI), deduplica por ID e preserva tags. Prioridade de dedup: Zen →
// Go → OpenRouter → AIHubMix → CLI (a CLI só complementa com modelos que as
// APIs não têm).
func mergeModels(zenModels, goModels, orModels, aihubmixModels, cliModels []ModelCatalogItem) []ModelCatalogItem {
	seen := make(map[string]bool)
	var merged []ModelCatalogItem

	all := append(append(append(append(zenModels, goModels...), orModels...), aihubmixModels...), cliModels...)

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
			Tags:      m.Tags,
			Compatible: compat,
		})
	}

	return merged
}

// getCachedSource devolve uma cópia do cache atual de uma fonte (ignora TTL).
func getCachedSource(mu sync.Locker, items *[]ModelCatalogItem) []ModelCatalogItem {
	mu.Lock()
	defer mu.Unlock()
	out := make([]ModelCatalogItem, len(*items))
	copy(out, *items)
	return out
}

// setSourceCache substitui o cache de uma fonte e grava o timestamp.
func setSourceCache(mu sync.Locker, items *[]ModelCatalogItem, at *time.Time, fetched []ModelCatalogItem) {
	mu.Lock()
	defer mu.Unlock()
	*items = fetched
	*at = time.Now()
}

// refreshCatalog atualiza o cache do catálogo. Cada fonte (Zen/Go/OpenRouter/
// AIHubMix/CLI) é buscada de forma independente, respeitando o TTL PRÓPRIO
// (24h/24h/6h/1h/12h) — uma falha numa fonte não derruba as outras (usa
// cache stale se houver). force=true ignora os TTLs e busca todas de novo.
func refreshCatalog(force bool) error {
	log.Printf("[catalog] Iniciando refresh do catálogo de modelos...")

	var zenModels, goModels, orModels, aihubmixModels, cliModels []ModelCatalogItem
	var zenErr, goErr, orErr, aihubmixErr, cliErr error
	var wg sync.WaitGroup

	// ── OpenCode Zen (TTL 24h) ──
	zenModels = getCachedSource(&zenCacheMutex, &zenCache)
	if force || len(zenModels) == 0 || time.Since(zenCachedAt) > zenCacheTTL {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := fetchOpenCodeZenModels()
			if err != nil {
				zenErr = err
				return
			}
			setSourceCache(&zenCacheMutex, &zenCache, &zenCachedAt, f)
			zenModels = f
		}()
	}

	// ── OpenCode Go (TTL 24h) ──
	goModels = getCachedSource(&goCacheMutex, &goCache)
	if force || len(goModels) == 0 || time.Since(goCachedAt) > goCacheTTL {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := fetchOpenCodeGoModels()
			if err != nil {
				goErr = err
				return
			}
			setSourceCache(&goCacheMutex, &goCache, &goCachedAt, f)
			goModels = f
		}()
	}

	// ── OpenRouter (TTL 6h) ──
	orModels = getCachedSource(&openRouterCacheMutex, &openRouterCache)
	if force || len(orModels) == 0 || time.Since(openRouterCachedAt) > openRouterCacheTTL {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := fetchOpenRouterModels()
			if err != nil {
				orErr = err
				return
			}
			setSourceCache(&openRouterCacheMutex, &openRouterCache, &openRouterCachedAt, f)
			orModels = f
		}()
	}

	// ── OpenCode CLI `opencode models` (TTL 12h) ──
	cliModels = getCachedSource(&cliCacheMutex, &cliCache)
	if force || len(cliModels) == 0 || time.Since(cliCachedAt) > cliCacheTTL {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := fetchOpenCodeCLIModels()
			if err != nil {
				cliErr = err
				return
			}
			setSourceCache(&cliCacheMutex, &cliCache, &cliCachedAt, f)
			cliModels = f
		}()
	}

	// ── AIHubMix (TTL 1h) ──
	aihubmixModels = getCachedSource(&aihubmixCacheMutex, &aihubmixCache)
	if force || len(aihubmixModels) == 0 || time.Since(aihubmixCachedAt) > aihubmixCacheTTL {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := fetchAIHubMixModels()
			if err != nil {
				aihubmixErr = err
				return
			}
			setSourceCache(&aihubmixCacheMutex, &aihubmixCache, &aihubmixCachedAt, f)
			aihubmixModels = f
		}()
	}

	wg.Wait()

	if orErr != nil {
		log.Printf("[catalog] OpenRouter erro (usando cache stale): %v", orErr)
	}
	if zenErr != nil {
		log.Printf("[catalog] OpenCode Zen erro (usando cache stale): %v", zenErr)
	}
	if goErr != nil {
		log.Printf("[catalog] OpenCode Go erro (usando cache stale): %v", goErr)
	}
	if aihubmixErr != nil {
		log.Printf("[catalog] AIHubMix erro (usando cache stale): %v", aihubmixErr)
	}
	if cliErr != nil {
		log.Printf("[catalog] OpenCode CLI erro (usando cache stale): %v", cliErr)
	}
	if orErr != nil && zenErr != nil && goErr != nil && aihubmixErr != nil && cliErr != nil {
		return fmt.Errorf("as 5 fontes falharam: OR=%v, Zen=%v, Go=%v, AIHubMix=%v, CLI=%v", orErr, zenErr, goErr, aihubmixErr, cliErr)
	}

	// Mescla
	merged := mergeModels(zenModels, goModels, orModels, aihubmixModels, cliModels)

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

// getCatalog retorna o catálogo (do cache ou refresh se expirado). O TTL do
// cache MESCLADO é curto (cacheTTL) só para reconstruir o merge; as fontes
// respeitam seus TTLs próprios dentro de refreshCatalog.
func getCatalog() ([]ModelCatalogItem, error) {
	catalogCacheMutex.RLock()
	cached := len(catalogCache) > 0
	expired := time.Since(catalogCachedAt) > cacheTTL
	catalogCacheMutex.RUnlock()

	if !cached || expired {
		if err := refreshCatalog(false); err != nil {
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
	// Ordem: OpenCode Zen, Google, OpenRouter, AIHubMix, outros
	order := []string{"OpenCode Zen", "Google", "OpenRouter", "AIHubMix", "OpenAI", "Anthropic", "Meta", "Mistral", "Cohere", "Qwen", "DeepSeek", "MiniMax", "GLM", "Kimi", "Muse", "Nemotron", "Jamba", "Mistral", "Qwen", "GLM", "Kimi", "Muse"}
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
	
	// ?force=1 → refaz o refresh imediatamente, ignorando TTLs das fontes
	// (novos modelos do OpenCode/OpenRouter aparecem na hora).
	if r.URL.Query().Get("force") == "1" {
		if err := refreshCatalog(true); err != nil {
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
		ActiveStatus: activeModelStatus(modelsList),
	}
	if catalogCacheErr != nil {
		resp.Warning = catalogCacheErr.Error()
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// activeModelStatus — TRAVA DE SEGURANÇA (29/08): se o modelo ativo do
// usuário sumiu do catálogo (expirado/removido da lista), reporta "expired"
// para o frontend exibir o badge — a seleção NUNCA é sobrescrita sozinha.
func activeModelStatus(models []ModelCatalogItem) string {
	active := getActiveModel()
	if active == "" {
		return modelStatusOK
	}
	for _, m := range models {
		if m.ID == active {
			return modelStatusOK
		}
	}
	return modelStatusExpired
}

// Inicialização do catálogo na inicialização do servidor
func initCatalog() {
	go func() {
		time.Sleep(2 * time.Second) // Aguarda servidor subir
		if err := refreshCatalog(false); err != nil {
			log.Printf("[catalog] Erro inicial: %v", err)
		}
		// Background refresh: cada fonte respeita seu próprio TTL dentro de
		// refreshCatalog (Zen/Go 24h, OpenRouter 6h). O ticker só "acorda" o
		// refresh; as fontes não-stale são reutilizadas do cache.
		ticker := time.NewTicker(5 * time.Minute)
		go func() {
			for range ticker.C {
				refreshCatalog(false)
			}
		}()
	}()
}
