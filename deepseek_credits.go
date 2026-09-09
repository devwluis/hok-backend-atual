package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// deepSeekBalanceInfo — un elemento de balance_infos devuelto por
// GET https://api.deepseek.com/user/balance. Documentado por DeepSeek:
//
//   - currency: "USD"
//   - total_balance: saldo total actual (string con decimal)
//   - granted_balance: saldo de bono concedido (gratuito)
//   - topped_up_balance: saldo cargado (recarga de pago)
type deepSeekBalanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// deepSeekBalanceResp — envelope de la respuesta del endpoint de saldo.
type deepSeekBalanceResp struct {
	IsAvailable bool                 `json:"is_available"`
	BalanceInfos []deepSeekBalanceInfo `json:"balance_infos"`
}

// fetchDeepSeekJSON — GET autenticado com Bearer DEEPSEEK_API_KEY
// (mismo patrón que fetchOpenRouterJSON. timeout 15s).
func fetchDeepSeekJSON(url, apiKey string, out interface{}) error {
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

// handleDeepSeekCredits devuelve el saldo de la cuenta DeepSeek.
//
//	GET /deepseek/credits
//
// Auth: X-Hok-Token (requireHokAuth) + CORS. La API key se lee del
// ambiente (DEEPSEEK_API_KEY) — el frontend nunca la ve.
//
// Respuesta:
//	{
//	  "balance": "4.97",            total_balance
//	  "currency": "USD",
//	  "granted_balance": "0.00",
//	  "topped_up_balance": "4.97",
//	  "is_available": true,
//	  "source": "deepseek_api"
//	}
//
// Nota: la API de DeepSeek expone solo el saldo (no usa histórico por
// mes/semana/día) — eso vive solo en el dashboard web. El card del
// frontend usa los campos que sí existen.
func handleDeepSeekCredits(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		http.Error(w, `{"error":"DEEPSEEK_API_KEY nao definida"}`, http.StatusInternalServerError)
		return
	}

	var bal deepSeekBalanceResp
	if err := fetchDeepSeekJSON("https://api.deepseek.com/user/balance", apiKey, &bal); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}

	// Toma el primer balance_infos (normalmente el único; USD).
	balance := ""
	currency := "USD"
	granted := ""
	topped := ""
	if len(bal.BalanceInfos) > 0 {
		info := bal.BalanceInfos[0]
		balance = info.TotalBalance
		currency = info.Currency
		granted = info.GrantedBalance
		topped = info.ToppedUpBalance
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"balance":          balance,
		"currency":         currency,
		"granted_balance":  granted,
		"topped_up_balance": topped,
		"is_available":     bal.IsAvailable,
		"source":           "deepseek_api",
	})
}