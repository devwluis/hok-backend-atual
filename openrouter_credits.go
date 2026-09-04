package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

type openRouterKeyInfo struct {
	Data struct {
		Usage          float64  `json:"usage"`
		UsageDaily     float64  `json:"usage_daily"`
		UsageWeekly    float64  `json:"usage_weekly"`
		UsageMonthly   float64  `json:"usage_monthly"`
		Limit          *float64 `json:"limit"`
		LimitRemaining *float64 `json:"limit_remaining"`
	} `json:"data"`
}

type openRouterCreditsInfo struct {
	Data struct {
		TotalCredits float64 `json:"total_credits"`
		TotalUsage   float64 `json:"total_usage"`
	} `json:"data"`
}

func fetchOpenRouterJSON(url, apiKey string, out interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("resposta invalida: %s", string(body))
	}
	return nil
}

func handleOpenRouterCredits(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		http.Error(w, `{"error":"OPENROUTER_API_KEY nao definida"}`, http.StatusInternalServerError)
		return
	}

	var keyInfo openRouterKeyInfo
	if err := fetchOpenRouterJSON("https://openrouter.ai/api/v1/auth/key", apiKey, &keyInfo); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	result := map[string]interface{}{
		"usage_monthly":   keyInfo.Data.UsageMonthly,
		"usage_daily":     keyInfo.Data.UsageDaily,
		"usage_weekly":    keyInfo.Data.UsageWeekly,
		"usage_total":     keyInfo.Data.Usage,
		"limit":           keyInfo.Data.Limit,
		"limit_remaining": keyInfo.Data.LimitRemaining,
	}

	mgmtKey := os.Getenv("OPENROUTER_MANAGEMENT_KEY")
	if mgmtKey != "" {
		var creditsInfo openRouterCreditsInfo
		if err := fetchOpenRouterJSON("https://openrouter.ai/api/v1/credits", mgmtKey, &creditsInfo); err == nil {
			result["total_credits"] = creditsInfo.Data.TotalCredits
			result["total_usage"] = creditsInfo.Data.TotalUsage
			result["balance"] = creditsInfo.Data.TotalCredits - creditsInfo.Data.TotalUsage
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

// openRouterActivityItem é o shape REAL de um registro retornado pela
// API /api/v1/activity do OpenRouter. Documentação: https://openrouter.ai/docs/api-reference/activity
//
// O OpenRouter devolve **agregados por dia + modelo + endpoint** (não por
// chamada individual). Campos:
//
//   - date: "YYYY-MM-DD HH:MM:SS" (formato MySQL DATE, UTC)
//   - model: slug OpenRouter do modelo (ex: "minimax/minimax-m3")
//   - model_permaslug: slug imutável com data (ex: "minimax/minimax-m3-20260531")
//   - usage: custo em USD (float)
//   - requests: número de requests no dia
//   - prompt_tokens, completion_tokens, reasoning_tokens: tokens usados
//   - byok_*: trazido pelos clientes BYOK (zero no nosso caso)
//   - provider_name: provedor upstream que respondeu (ex: "gmicloud/fp8")
//   - endpoint_id: UUID do endpoint usado
type openRouterActivityItem struct {
	Date          string  `json:"date"`
	Model         string  `json:"model"`
	ModelPermaslug string `json:"model_permaslug,omitempty"`
	EndpointID    string  `json:"endpoint_id,omitempty"`
	ProviderName  string  `json:"provider_name,omitempty"`
	Usage         float64 `json:"usage"`
	ByokUsage     float64 `json:"byok_usage_inference,omitempty"`
	Requests      int     `json:"requests"`
	ByokRequests  int     `json:"byok_requests,omitempty"`
	PromptTokens  int     `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	ReasoningTokens  int  `json:"reasoning_tokens,omitempty"`
}

// openRouterActivityResp é o envelope da resposta da API.
type openRouterActivityResp struct {
	Data []openRouterActivityItem `json:"data"`
}

// handleOpenRouterActivity retorna os últimos N registros de uso do OpenRouter
// (agregados por dia + modelo + endpoint). Requer OPENROUTER_MANAGEMENT_KEY.
//
//	GET /openrouter/activity?limit=20
//
// limit: default 20, max 100.
//
// Resposta: objeto {limit, count, items[], source}.
// items[] tem {date, model, model_permaslug, usage, requests,
// prompt_tokens, completion_tokens, reasoning_tokens, provider_name,
// endpoint_id, route}.
//
// Política free-only 04/09: este endpoint é somente leitura — apenas exibe
// o histórico de uso que o OpenRouter já tem. Não dispara nenhuma chamada.
func handleOpenRouterActivity(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	mgmtKey := os.Getenv("OPENROUTER_MANAGEMENT_KEY")
	if mgmtKey == "" {
		http.Error(w, `{"error":"OPENROUTER_MANAGEMENT_KEY nao definida"}`, http.StatusInternalServerError)
		return
	}

	// Parse ?limit=N (default 20, max 100)
	limit := 20
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}

	url := fmt.Sprintf("https://openrouter.ai/api/v1/activity?limit=%d", limit)
	var actResp openRouterActivityResp
	if err := fetchOpenRouterJSON(url, mgmtKey, &actResp); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	// NOTA (04/09): o OpenRouter IGNORA o ?limit e devolve o conjunto completo
	// (hoje 291 registros). Aplicamos o limit manualmente aqui, pegando os
	// `limit` itens mais recentes. Se o universo crescer além de centenas de
	// registros, vai precisar paginar via cursor (parâmetro documentado mas
	// não testado aqui).
	if len(actResp.Data) > limit {
		actResp.Data = actResp.Data[:limit]
	}

	// Normaliza saída. "route" fica vazio porque o OpenRouter não devolve
	// qual endpoint do HOK (ex: /smart, /vision) originou a chamada — isso
	// teria que ser correlacionado via logs internos do HOK.
	out := make([]map[string]interface{}, 0, len(actResp.Data))
	for _, it := range actResp.Data {
		out = append(out, map[string]interface{}{
			"date":             it.Date,
			"model":            it.Model,
			"model_permaslug":  it.ModelPermaslug,
			"usage":            it.Usage,
			"requests":         it.Requests,
			"prompt_tokens":    it.PromptTokens,
			"completion_tokens": it.CompletionTokens,
			"reasoning_tokens": it.ReasoningTokens,
			"provider_name":    it.ProviderName,
			"endpoint_id":      it.EndpointID,
			"route":            "", // ver comentário acima
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"limit":  limit,
		"count":  len(out),
		"items":  out,
		"source": "openrouter_api",
	})
}
