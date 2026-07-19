package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ── Pending store (in-memory) ──────────────────────────────────────────────────

var (
	pendingAutomations = make(map[string]pendingAuto)
	pendingMu          sync.Mutex
)

type pendingAuto struct {
	Description  string
	WorkflowJSON map[string]interface{}
	CreatedAt    time.Time
}

// ── Types ──────────────────────────────────────────────────────────────────────

type AutoDesignReq struct {
	Description string `json:"description"`
}

type AutoDesignResp struct {
	PendingID    string                 `json:"pending_id"`
	Description  string                 `json:"description"`
	Plan         string                 `json:"plan"`
	WorkflowJSON map[string]interface{} `json:"workflow_json"`
	DeployHint   string                 `json:"deploy_hint"`
}

type AutoDeployReq struct {
	PendingID    string                 `json:"pending_id,omitempty"`
	WorkflowJSON map[string]interface{} `json:"workflow_json,omitempty"`
}

type AutoDeployResp struct {
	WorkflowID string `json:"workflow_id"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	Message    string `json:"message"`
}

// ── System Prompt ──────────────────────────────────────────────────────────────

const autoSystemPrompt = `Você é especialista em N8N workflow automation.
Gere um workflow JSON válido para N8N baseado na descrição.

REGRAS (siga à risca):
1. Retorne APENAS o JSON. Sem markdown, sem explicações, sem blocos de código.
2. Estrutura: {"name":"...","nodes":[...],"connections":{...},"settings":{"executionOrder":"v1"}}
3. Cada node: {"id":"s1","name":"...","type":"...","typeVersion":N,"position":[x,y],"parameters":{...}}
4. Nodes disponíveis:
   - n8n-nodes-base.scheduleTrigger  (typeVersion:1)
   - n8n-nodes-base.httpRequest      (typeVersion:4)
   - n8n-nodes-base.code             (typeVersion:2, parâmetro: jsCode)
   - n8n-nodes-base.if               (typeVersion:2)
   - n8n-nodes-base.set              (typeVersion:3)
5. Posicoes: iniciar [240,300], incrementar 220px no X
6. CRITICO: connections usa SEMPRE o NOME do node (campo "name"), NUNCA o id ("s1","s2"). Exemplo: {"Schedule":{"main":[[{"node":"Get Disk","type":"main","index":0}]]}}
7. Schedule minutos: {"rule":{"interval":[{"field":"minutes","minutesInterval":N}]}}
8. Schedule cron:    {"rule":{"interval":[{"field":"cronExpression","expression":"0 3 * * *"}]}}
9. HTTP ao HOK backend (sempre com auth):
   {"url":"http://127.0.0.1:8082/ENDPOINT","method":"GET",
    "sendHeaders":true,"headerParameters":{"parameters":[{"name":"X-Hok-Token","value":"hok-api-2026"}]}}
10. POST com body: adicionar "sendBody":true,"contentType":"raw","rawBody":"{json escaped}"
11. Node Code: acesse dados com $input.first().json

Endpoints HOK disponíveis:
  GET  /health           → {ok:bool}
  GET  /debug/resources  → {ram_percent, cpu_percent, disk_percent}
  POST /terminal         → {command:"bash"} → {output:"..."}
  POST /n8n/trigger      → {workflow:"self-heal",payload:{...}}
  GET  /memories         → lista memórias

Retorne SOMENTE o JSON. Nenhuma palavra extra.`

// ── Handler: Design ────────────────────────────────────────────────────────────

func handleAutomationDesign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	if r.Header.Get("X-Hok-Token") != os.Getenv("HOK_TOKEN") {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var req AutoDesignReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Description) == "" {
		http.Error(w, `{"error":"description required"}`, 400)
		return
	}

	raw, err := callDeepSeekAuto(req.Description)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"AI: %s"}`, err.Error()), 500)
		return
	}

	// Limpar markdown fences
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var wfJSON map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &wfJSON); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":       "IA gerou JSON inválido",
			"raw":         cleaned,
			"parse_error": err.Error(),
		})
		return
	}

	pendingID := fmt.Sprintf("auto_%d", time.Now().UnixNano())
	pendingMu.Lock()
	pendingAutomations[pendingID] = pendingAuto{
		Description:  req.Description,
		WorkflowJSON: wfJSON,
		CreatedAt:    time.Now(),
	}
	pendingMu.Unlock()

	name, _ := wfJSON["name"].(string)
	nodes, _ := wfJSON["nodes"].([]interface{})
	nodeNames := []string{}
	for _, n := range nodes {
		if nm, ok := n.(map[string]interface{}); ok {
			if nn, ok := nm["name"].(string); ok {
				nodeNames = append(nodeNames, nn)
			}
		}
	}
	plan := fmt.Sprintf(`"%s" | %d nodes: %s`, name, len(nodes), strings.Join(nodeNames, " → "))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AutoDesignResp{
		PendingID:    pendingID,
		Description:  req.Description,
		Plan:         plan,
		WorkflowJSON: wfJSON,
		DeployHint:   fmt.Sprintf(`POST /automation/deploy {"pending_id":"%s"}`, pendingID),
	})
}

// ── Handler: Deploy ────────────────────────────────────────────────────────────

func handleAutomationDeploy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	if r.Header.Get("X-Hok-Token") != os.Getenv("HOK_TOKEN") {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var req AutoDeployReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}

	var wfJSON map[string]interface{}

	if req.PendingID != "" {
		pendingMu.Lock()
		p, ok := pendingAutomations[req.PendingID]
		if !ok {
			pendingMu.Unlock()
			http.Error(w, `{"error":"pending não encontrado"}`, 404)
			return
		}
		wfJSON = p.WorkflowJSON
		delete(pendingAutomations, req.PendingID)
		pendingMu.Unlock()
	} else if req.WorkflowJSON != nil {
		wfJSON = req.WorkflowJSON
	} else {
		http.Error(w, `{"error":"pending_id ou workflow_json obrigatório"}`, 400)
		return
	}

	n8nBase := os.Getenv("N8N_BASE_URL")
	if n8nBase == "" {
		n8nBase = "http://127.0.0.1:5678"
	}
	n8nKey := os.Getenv("N8N_API_KEY")

	wfBytes, _ := json.Marshal(wfJSON)
	createBody, err := n8nRequest("POST", n8nBase+"/api/v1/workflows", n8nKey, wfBytes)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"N8N create: %s"}`, err.Error()), 500)
		return
	}

	var created map[string]interface{}
	json.Unmarshal(createBody, &created)

	wfID, _ := created["id"].(string)
	if wfID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write(createBody)
		return
	}

	n8nRequest("POST", n8nBase+"/api/v1/workflows/"+wfID+"/activate", n8nKey, nil)

	wfName, _ := created["name"].(string)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(AutoDeployResp{
		WorkflowID: wfID,
		Name:       wfName,
		Active:     true,
		Message:    fmt.Sprintf("✅ Workflow '%s' criado e ativado (ID: %s)", wfName, wfID),
	})
}

// ── AI Helper ─────────────────────────────────────────────────────────────────

func callDeepSeekAuto(description string) (string, error) {
	dsKey := os.Getenv("GROQ_KEY")
	if dsKey == "" {
		return "", fmt.Errorf("GROQ_KEY não configurado")
	}

	payload := map[string]interface{}{
		"model":      "llama-3.3-70b-versatile",
		"max_tokens": 4000,
		"messages": []map[string]string{
			{"role": "system", "content": autoSystemPrompt},
			{"role": "user", "content": "Crie um workflow N8N para: " + description},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+dsKey)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Error != nil {
		return "", fmt.Errorf(result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("sem resposta da IA")
	}
	return result.Choices[0].Message.Content, nil
}

// ── N8N Helper ────────────────────────────────────────────────────────────────

func n8nRequest(method, url, apiKey string, body []byte) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-N8N-API-KEY", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
