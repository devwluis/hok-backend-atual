package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
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

// deepSeekFetchBalance busca o saldo atual na API DeepSeek e devolve o
// primeiro balance_info (normalmente USD, único).
func deepSeekFetchBalance(apiKey string) (deepSeekBalanceInfo, bool, error) {
	var bal deepSeekBalanceResp
	if err := fetchDeepSeekJSON("https://api.deepseek.com/user/balance", apiKey, &bal); err != nil {
		return deepSeekBalanceInfo{Currency: "USD"}, false, err
	}
	info := deepSeekBalanceInfo{Currency: "USD"}
	if len(bal.BalanceInfos) > 0 {
		info = bal.BalanceInfos[0]
	}
	return info, bal.IsAvailable, nil
}

// parseMoney converte string decimal da API em float (0 em falha).
func parseMoney(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}

// deepSeekLastSnapshot lê o último snapshot gravado (balance, total_loaded, ts).
func deepSeekLastSnapshot() (balance, totalLoaded float64, ts int64, ok bool) {
	out := strings.TrimSpace(sqliteExecParams(
		`SELECT balance, total_loaded, ts FROM deepseek_balance_snapshots ORDER BY id DESC LIMIT 1;`))
	if out == "" || strings.HasPrefix(out, "Error:") {
		return 0, 0, 0, false
	}
	parts := strings.Split(out, "|")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	b, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	tl, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if e1 != nil || e2 != nil {
		return 0, 0, 0, false
	}
	ts, _ = strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	return b, tl, ts, true
}

// deepSeekRecordSnapshot insere um snapshot no histórico.
func deepSeekRecordSnapshot(info deepSeekBalanceInfo, balance, totalLoaded, spent float64) {
	sqliteExecParams(
		`INSERT INTO deepseek_balance_snapshots (currency, balance, topped_up, granted, total_loaded, spent) VALUES (?,?,?,?,?,?);`,
		info.Currency, balance, parseMoney(info.ToppedUpBalance), parseMoney(info.GrantedBalance), totalLoaded, spent)
}

// deepSeekComputeState calcula total_loaded ("total carregado", fixo) e spent
// ("gasto até o momento") a partir do saldo atual. A API DeepSeek não expõe
// total histórico — então:
//   - primeiro snapshot: total_loaded = balance (baseline automático);
//   - chamadas seguintes: salto POSITIVO de saldo = recarga → soma ao total;
//     consumo (saldo cai) não altera o total.
//
// override != nil força total_loaded (endpoint de baseline manual).
// Grava snapshot com throttle (saldo mudou ou > 1h).
func deepSeekComputeState(info deepSeekBalanceInfo, balance float64, override *float64) (totalLoaded, spent float64) {
	lastBal, lastTotal, lastTs, has := deepSeekLastSnapshot()
	now := time.Now().Unix()
	switch {
	case override != nil:
		totalLoaded = *override
	case !has:
		totalLoaded = balance
	default:
		totalLoaded = lastTotal
		if delta := balance - lastBal; delta > 0.005 {
			totalLoaded += delta
		}
	}
	spent = totalLoaded - balance
	if spent < 0 {
		spent = 0
	}
	if !has || override != nil || math.Abs(balance-lastBal) > 0.0001 || now-lastTs >= 3600 {
		deepSeekRecordSnapshot(info, balance, totalLoaded, spent)
	}
	return totalLoaded, spent
}

// handleDeepSeekCredits devuelve el saldo de la cuenta DeepSeek + los campos
// derivados localmente (total_loaded, spent).
//
//	GET /deepseek/credits
//
// Auth: X-Hok-Token (requireHokAuth) + CORS. La API key se lee del
// ambiente (DEEPSEEK_API_KEY) — el frontend nunca la ve.
//
// Respuesta:
//	{
//	  "balance": "4.97",            total_balance actual
//	  "currency": "USD",
//	  "granted_balance": "0.00",
//	  "topped_up_balance": "4.97",
//	  "total_loaded": 9.97,         total carregado (baseline + recargas)
//	  "spent": 5.00,                total_loaded - balance
//	  "is_available": true,
//	  "source": "deepseek_api"
//	}
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

	info, available, err := deepSeekFetchBalance(apiKey)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	balance := parseMoney(info.TotalBalance)
	totalLoaded, spent := deepSeekComputeState(info, balance, nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"balance":           info.TotalBalance,
		"currency":          info.Currency,
		"granted_balance":   info.GrantedBalance,
		"topped_up_balance": info.ToppedUpBalance,
		"total_loaded":      totalLoaded,
		"spent":             spent,
		"is_available":      available,
		"source":            "deepseek_api",
	})
}

// handleDeepSeekCreditsBaseline permite forçar manualmente o "total carregado"
// de referência (saída de emergência quando a detecção automática de recarga
// erra — ex: saldo caiu abaixo de zero entre consultas).
//
//	POST /deepseek/credits/baseline   {"total_loaded": 10.00}
func handleDeepSeekCreditsBaseline(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		TotalLoaded *float64 `json:"total_loaded"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"json invalido"}`, http.StatusBadRequest)
		return
	}
	if body.TotalLoaded == nil {
		http.Error(w, `{"error":"campo total_loaded obrigatorio"}`, http.StatusBadRequest)
		return
	}
	if *body.TotalLoaded < 0 {
		http.Error(w, `{"error":"total_loaded deve ser >= 0"}`, http.StatusBadRequest)
		return
	}
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		http.Error(w, `{"error":"DEEPSEEK_API_KEY nao definida"}`, http.StatusInternalServerError)
		return
	}
	info, _, err := deepSeekFetchBalance(apiKey)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
		return
	}
	balance := parseMoney(info.TotalBalance)
	totalLoaded, spent := deepSeekComputeState(info, balance, body.TotalLoaded)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":       "ok",
		"total_loaded": totalLoaded,
		"spent":        spent,
		"balance":      balance,
	})
}
