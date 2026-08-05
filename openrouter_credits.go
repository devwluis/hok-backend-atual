package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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
