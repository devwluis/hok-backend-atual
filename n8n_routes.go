package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// ── Configuração N8N ─────────────────────────────────────────
var (
	N8N_BASE_URL = getEnvOrFile("N8N_BASE_URL", "http://127.0.0.1:5678")
	N8N_API_KEY  = getEnvOrFile("N8N_API_KEY", "")
)

func getEnvOrFile(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	// Tenta ler de ~/.keys
	data, err := os.ReadFile(os.Getenv("HOME") + "/.keys")
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, key+"=") {
			return strings.TrimPrefix(line, key+"=")
		}
	}
	return fallback
}

// Registro de workflows disponíveis
type N8NWorkflow struct {
	ID          string
	Name        string
	WebhookPath string
	Description string
}

var N8N_WORKFLOWS = []N8NWorkflow{
	{
		ID:          "LouYvmcty2uaoXCn",
		Name:        "self-heal",
		WebhookPath: "hokos-self-heal",
		Description: "Aciona auto-reparo do HOK OS",
	},
}

// ── POST /n8n/trigger ────────────────────────────────────────
// Aciona um workflow do N8N pelo nome
func handleN8NTrigger(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var body struct {
		Workflow string                 `json:"workflow"`
		Payload  map[string]interface{} `json:"payload"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.Workflow == "" {
		respondJSON(w, map[string]string{"status": "error", "message": "workflow obrigatório"})
		return
	}

	// Encontra o workflow
	var found *N8NWorkflow
	for _, wf := range N8N_WORKFLOWS {
		if strings.EqualFold(wf.Name, body.Workflow) ||
			strings.EqualFold(wf.ID, body.Workflow) ||
			strings.Contains(strings.ToLower(wf.Description), strings.ToLower(body.Workflow)) {
			wf := wf
			found = &wf
			break
		}
	}

	if found == nil {
		// Lista workflows disponíveis
		names := []string{}
		for _, wf := range N8N_WORKFLOWS {
			names = append(names, wf.Name)
		}
		respondJSON(w, map[string]interface{}{
			"status":    "error",
			"message":   fmt.Sprintf("workflow '%s' não encontrado", body.Workflow),
			"available": names,
		})
		return
	}

	// Monta payload
	if body.Payload == nil {
		body.Payload = map[string]interface{}{}
	}
	body.Payload["source"] = "hokos-backend"
	body.Payload["timestamp"] = time.Now().Unix()
	body.Payload["tunnel_url"] = "https://api.imoveischaves.com"

	payloadBytes, _ := json.Marshal(body.Payload)
	webhookURL := fmt.Sprintf("%s/webhook/%s", N8N_BASE_URL, found.WebhookPath)

	// Chama o webhook do N8N
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", webhookURL, bytes.NewReader(payloadBytes))
	if err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-N8N-Token", N8N_TOKEN)

	resp, err := client.Do(req)
	if err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "N8N offline: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	sqliteExec(fmt.Sprintf(
		`INSERT INTO logs (event, level, source) VALUES ('N8N trigger: %s', 'INFO', 'n8n');`,
		strings.ReplaceAll(found.Name, "'", "''")))

	respondJSON(w, map[string]interface{}{
		"status":   "ok",
		"workflow": found.Name,
		"n8n_url":  webhookURL,
		"response": string(respBody),
	})
}

// ── GET /n8n/workflows ───────────────────────────────────────
// Lista workflows disponíveis
func handleN8NWorkflows(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	// Consulta N8N API para status atual
	apiKey := N8N_API_KEY
	var items []map[string]interface{}

	if apiKey != "" {
		client := &http.Client{Timeout: 10 * time.Second}
		req, err := http.NewRequest("GET", N8N_BASE_URL+"/api/v1/workflows", nil)
		if err == nil {
			req.Header.Set("X-N8N-Api-Key", apiKey)
			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				var result map[string]interface{}
				if json.NewDecoder(resp.Body).Decode(&result) == nil {
					if data, ok := result["data"].([]interface{}); ok {
						for _, d := range data {
							if wf, ok := d.(map[string]interface{}); ok {
								items = append(items, map[string]interface{}{
									"id":     wf["id"],
									"name":   wf["name"],
									"active": wf["active"],
								})
							}
						}
					}
				}
			}
		}
	}

	// Fallback: lista local
	if items == nil {
		for _, wf := range N8N_WORKFLOWS {
			items = append(items, map[string]interface{}{
				"id":          wf.ID,
				"name":        wf.Name,
				"description": wf.Description,
				"webhook":     wf.WebhookPath,
			})
		}
	}

	if items == nil {
		items = []map[string]interface{}{}
	}

	respondJSON(w, map[string]interface{}{"status": "ok", "workflows": items})
}

// ── GET /n8n/status ──────────────────────────────────────────
func handleN8NStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(N8N_BASE_URL + "/healthz")
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"status": "offline",
			"url":    N8N_BASE_URL,
			"error":  err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	respondJSON(w, map[string]interface{}{
		"status": "online",
		"url":    N8N_BASE_URL,
		"code":   resp.StatusCode,
	})
}

// ── Router /n8n ──────────────────────────────────────────────
func handleN8N(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/n8n")
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "trigger":
		handleN8NTrigger(w, r)
	case "workflows":
		handleN8NWorkflows(w, r)
	case "status":
		handleN8NStatus(w, r)
	default:
		setCORS(w)
		http.Error(w, "not found", 404)
	}
}

// ── POST /api/n8n-proxy ──────────────────────────────────────
// Proxy para a API REST do N8N (baseUrl + token vêm do frontend)
type n8nProxyReq struct {
	BaseURL string `json:"baseUrl"`
	Token   string `json:"token"`
	Path    string `json:"path"`
}

func handleN8NProxy(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != "POST" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(405)
		w.Write([]byte(`{"error":"method not allowed"}`))
		return
	}
	var req n8nProxyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"json invalido"}`))
		return
	}
	if req.BaseURL == "" || req.Path == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"baseUrl e path obrigatorios"}`))
		return
	}
	// Sempre usa N8N_BASE_URL interno — ignora baseUrl do request
	target := strings.TrimRight(N8N_BASE_URL, "/") + "/" + strings.TrimLeft(req.Path, "/")
	client := &http.Client{Timeout: 15 * time.Second}
	proxyReq, err := http.NewRequest("GET", target, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write([]byte(`{"error":"url invalida"}`))
		return
	}
	apiKey := req.Token
	if apiKey == "" {
		apiKey = N8N_API_KEY
	}
	if apiKey != "" {
		proxyReq.Header.Set("X-N8N-API-KEY", apiKey)
	}
	proxyReq.Header.Set("Accept", "application/json")
	resp, err := client.Do(proxyReq)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(502)
		w.Write([]byte(`{"error":"n8n inacessivel"}`))
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}
