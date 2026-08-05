package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// mcpN8nClient mantém uma sessão MCP com o servidor n8n-mcp,
// renovando automaticamente quando expira.
type mcpN8nClient struct {
	baseURL   string
	authToken string
	mu        sync.Mutex
	sessionID string
	sessionAt time.Time
	client    *http.Client
}

func newMCPN8nClient() *mcpN8nClient {
	return &mcpN8nClient{
		baseURL:   getEnvDefault("N8N_MCP_URL", "http://localhost:3100/mcp"),
		authToken: os.Getenv("N8N_MCP_TOKEN"),
		client:    &http.Client{Timeout: 15 * time.Second},
	}
}

func getEnvDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type mcpRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type mcpResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ensureSession inicializa (ou reinicializa) a sessão MCP se necessário.
func (c *mcpN8nClient) ensureSession() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// sessão ainda válida (margem de 5min antes do timeout de 30min do servidor)
	if c.sessionID != "" && time.Since(c.sessionAt) < 25*time.Minute {
		return nil
	}

	reqBody := mcpRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]string{
				"name":    "hok-backend",
				"version": "1.0.0",
			},
		},
	}

	respBody, headers, err := c.rawCall(reqBody, "")
	if err != nil {
		return fmt.Errorf("mcp initialize falhou: %w", err)
	}

	var resp mcpResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("mcp initialize: resposta invalida: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("mcp initialize: %s", resp.Error.Message)
	}

	sid := headers.Get("Mcp-Session-Id")
	if sid == "" {
		return fmt.Errorf("mcp initialize: servidor nao retornou Mcp-Session-Id")
	}

	c.sessionID = sid
	c.sessionAt = time.Now()
	return nil
}

// rawCall faz a chamada HTTP crua e retorna body + headers de resposta.
func (c *mcpN8nClient) rawCall(payload mcpRequest, sessionID string) ([]byte, http.Header, error) {
	buf, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}

	req, err := http.NewRequest("POST", c.baseURL, bytes.NewReader(buf))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.authToken)
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	body := make([]byte, 0)
	chunk := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(chunk)
		if n > 0 {
			body = append(body, chunk[:n]...)
		}
		if rerr != nil {
			break
		}
	}

	// resposta pode vir em formato SSE ("event: message\ndata: {...}")
	// ou JSON puro — normaliza extraindo so o JSON da linha "data:"
	body = extractSSEData(body)

	return body, resp.Header, nil
}

func extractSSEData(raw []byte) []byte {
	s := string(raw)
	const marker = "data: "
	idx := bytes.Index(raw, []byte(marker))
	if idx == -1 {
		return raw
	}
	rest := s[idx+len(marker):]
	return []byte(rest)
}

// CallTool chama uma tool do n8n-mcp (search_nodes, get_node, validate_node, etc)
// e retorna o texto de resultado já pronto pra injetar no contexto do agente.
func (c *mcpN8nClient) CallTool(toolName string, args map[string]interface{}) (string, error) {
	if c.authToken == "" {
		return "", fmt.Errorf("N8N_MCP_TOKEN nao configurado")
	}

	if err := c.ensureSession(); err != nil {
		return "", err
	}

	reqBody := mcpRequest{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	respBody, _, err := c.rawCall(reqBody, c.sessionID)
	if err != nil {
		return "", err
	}

	var resp mcpResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return "", fmt.Errorf("resposta invalida do mcp: %w", err)
	}

	if resp.Error != nil {
		// sessao pode ter expirado no meio do caminho -- forca renovar na proxima
		if resp.Error.Code == -32001 {
			c.mu.Lock()
			c.sessionID = ""
			c.mu.Unlock()
		}
		return "", fmt.Errorf("mcp tool error: %s", resp.Error.Message)
	}

	return string(resp.Result), nil
}
