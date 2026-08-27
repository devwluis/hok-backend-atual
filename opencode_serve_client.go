package main

// opencode_serve_client.go — cliente HTTP para o `opencode serve` (Fase 3).
//
// Falando com a API REST headless do opencode (OpenAPI 3.1.0 em /doc):
//   - POST /session                          cria sessão
//   - GET  /session/{id}                     estado da sessão
//   - POST /session/{id}/message             mensagem síncrona (bloqueia até o fim)
//   - POST /session/{id}/prompt_async        prompt assíncrono (204, fire-and-forget)
//   - POST /session/{id}/summarize           compactação assíncrona (retorna true;
//                                            resumo vira mensagem "compaction" no
//                                            histórico, entregue via eventos SSE)
//   - POST /session/{id}/permissions/{id}    resposta a pedido de permissão
//   - GET  /event                            stream SSE de eventos do servidor
//
// Autenticação: HTTP Basic com usuário fixo "opencode" (configurável via
// OPENCODE_SERVE_USER) e senha = OPENCODE_SERVE_PASSWORD (o servidor recusa
// qualquer outro usuário com 401).

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── Configuração ────────────────────────────────────────────────────────────

func opencodeServeURL() string {
	u := os.Getenv("OPENCODE_SERVE_URL")
	if u == "" {
		u = "http://127.0.0.1:4100"
	}
	return strings.TrimSuffix(u, "/")
}

func opencodeServeUser() string {
	u := os.Getenv("OPENCODE_SERVE_USER")
	if u == "" {
		u = "opencode"
	}
	return u
}

func opencodeServePassword() string {
	return os.Getenv("OPENCODE_SERVE_PASSWORD")
}

// opencodeServeHTTPTimeout limita cada chamada HTTP ao servidor opencode.
// A mensagem síncrona /message pode demorar (modelo + tools); o timeout
// espelha opencodeTimeout do CLI (300s).
const opencodeServeHTTPTimeout = 320 * time.Second

// ─── Cliente ─────────────────────────────────────────────────────────────────

type opencodeServeClient struct {
	baseURL  string
	username string
	password string
	http     *http.Client
}

func newOpenCodeServeClient() *opencodeServeClient {
	return &opencodeServeClient{
		baseURL:  opencodeServeURL(),
		username: opencodeServeUser(),
		password: opencodeServePassword(),
		http:     &http.Client{Timeout: opencodeServeHTTPTimeout},
	}
}

func (c *opencodeServeClient) do(method, path string, body interface{}) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("opencode serve: corpo invalido: %v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.baseURL+path, rdr)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// decodeJSON lê a resposta e faz parse; em erro HTTP inclui o status e o
// corpo bruto no erro (sem vazar segredos: corpo do opencode é JSON de erro).
func (c *opencodeServeClient) decodeJSON(resp *http.Response, out interface{}) error {
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("opencode serve: falha ao ler resposta %d: %v", resp.StatusCode, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("opencode serve: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("opencode serve: resposta invalida (%d): %v", resp.StatusCode, err)
	}
	return nil
}

// ─── Tipos da API ────────────────────────────────────────────────────────────

type openCodeServeSession struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	ProjectID string `json:"projectID"`
	Directory string `json:"directory"`
	Path      string `json:"path"`
	Title     string `json:"title"`
	Agent     string `json:"agent"`
	Model     struct {
		ID         string `json:"id"`
		ProviderID string `json:"providerID"`
	} `json:"model"`
	Version string `json:"version"`
	Time    struct {
		Created int64 `json:"created"`
		Updated int64 `json:"updated"`
	} `json:"time"`
}

type openCodeServeMessage struct {
	Info openCodeServeMessageInfo `json:"info"`
	// Parts é a lista de partes: step-start, reasoning, text, tool, step-finish...
	Parts []openCodeServePart `json:"parts"`
}

type openCodeServeMessageInfo struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionID"`
	ParentID   string `json:"parentID"`
	Role       string `json:"role"`
	Mode       string `json:"mode"`
	Agent      string `json:"agent"`
	// Summary pode ser bool (evento) ou objeto (resumo da sessão no
	// GET /message) — mantido cru para não quebrar o decode.
	Summary    json.RawMessage `json:"summary"`
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
	Finish     string `json:"finish"`
	Cost       float64 `json:"cost"`
	Tokens     struct {
		Total     int `json:"total"`
		Input     int `json:"input"`
		Output    int `json:"output"`
		Reasoning int `json:"reasoning"`
	} `json:"tokens"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
}

type openCodeServePart struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Text string `json:"text"`
	// State é objeto de formato variável (ex.: tool → {status, input, output};
	// text → string) — mantido cru para não quebrar o decode.
	State   json.RawMessage `json:"state"`
	Tool    string          `json:"tool"`
	Reason  string          `json:"reason"`
	Message string          `json:"message"`
}

// messageText concatena as partes de texto do assistente (ignora reasoning,
// tool e partes de controle) — mesma semântica do processOpenCodeJSONStream.
func openCodeServeMessageText(m *openCodeServeMessage) string {
	var sb strings.Builder
	for _, p := range m.Parts {
		if p.Type == "text" && p.Text != "" {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// openCodeServeEvent é um evento do SSE /event (tipo + propriedades).
type openCodeServeEvent struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

// ─── Operações ───────────────────────────────────────────────────────────────

// createOpenCodeServeSession — POST /session.
func (c *opencodeServeClient) createSession(title string) (*openCodeServeSession, error) {
	body := map[string]interface{}{}
	if title != "" {
		body["title"] = title
	}
	resp, err := c.do("POST", "/session", body)
	if err != nil {
		return nil, err
	}
	var s openCodeServeSession
	if err := c.decodeJSON(resp, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// getOpenCodeServeSession — GET /session/{id}.
func (c *opencodeServeClient) getSession(sessionID string) (*openCodeServeSession, error) {
	resp, err := c.do("GET", "/session/"+sessionID, nil)
	if err != nil {
		return nil, err
	}
	var s openCodeServeSession
	if err := c.decodeJSON(resp, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// getOpenCodeServeMessages — GET /session/{id}/message (histórico da sessão,
// usado pelo caminho async da Etapa B para casar a resposta por parentID).
func (c *opencodeServeClient) getMessages(sessionID string) ([]openCodeServeMessage, error) {
	resp, err := c.do("GET", "/session/"+sessionID+"/message", nil)
	if err != nil {
		return nil, err
	}
	var msgs []openCodeServeMessage
	if err := c.decodeJSON(resp, &msgs); err != nil {
		return nil, err
	}
	return msgs, nil
}

// sendOpenCodeServeMessage — POST /session/{id}/message (síncrono: a resposta
// chega completa ao final). Devolve a mensagem do assistente (info + parts).
func (c *opencodeServeClient) sendMessage(sessionID, text string, opts openCodeServeMessageOpts) (*openCodeServeMessage, error) {
	body := map[string]interface{}{
		"parts": []map[string]interface{}{{"type": "text", "text": text}},
	}
	if opts.NoReply {
		body["noReply"] = true
	}
	if opts.Agent != "" {
		body["agent"] = opts.Agent
	}
	if opts.System != "" {
		body["system"] = opts.System
	}
	if opts.ModelID != "" && opts.ProviderID != "" {
		body["model"] = map[string]string{
			"providerID": opts.ProviderID,
			"modelID":    opts.ModelID,
		}
	}
	resp, err := c.do("POST", "/session/"+sessionID+"/message", body)
	if err != nil {
		return nil, err
	}
	var m openCodeServeMessage
	if err := c.decodeJSON(resp, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// promptOpenCodeServeAsync — POST /session/{id}/prompt_async (204, sem esperar).
func (c *opencodeServeClient) promptAsync(sessionID, text string) error {
	body := map[string]interface{}{
		"parts": []map[string]interface{}{{"type": "text", "text": text}},
	}
	resp, err := c.do("POST", "/session/"+sessionID+"/prompt_async", body)
	if err != nil {
		return err
	}
	return c.decodeJSON(resp, nil)
}

// summarizeOpenCodeServeSession — POST /session/{id}/summarize. O servidor
// retorna `true` imediatamente e executa a compactação em segundo plano
// (mensagem "compaction" no histórico + eventos SSE session.compacted).
func (c *opencodeServeClient) summarizeSession(sessionID, providerID, modelID string) error {
	body := map[string]string{"providerID": providerID, "modelID": modelID}
	resp, err := c.do("POST", "/session/"+sessionID+"/summarize", body)
	if err != nil {
		return err
	}
	return c.decodeJSON(resp, nil)
}

// replyOpenCodeServePermission — POST /session/{id}/permissions/{permissionID}.
// response: "once", "always" ou "reject".
func (c *opencodeServeClient) replyPermission(sessionID, permissionID, response string) error {
	body := map[string]string{"response": response}
	resp, err := c.do("POST", "/session/"+sessionID+"/permissions/"+permissionID, body)
	if err != nil {
		return err
	}
	return c.decodeJSON(resp, nil)
}

// abortOpenCodeServeSession — POST /session/{id}/abort: cancela o
// processamento corrente. Rede de segurança do timeout do async — evita
// deixar a sessão busy com tool pendente.
func (c *opencodeServeClient) abortSession(sessionID string) error {
	resp, err := c.do("POST", "/session/"+sessionID+"/abort", nil)
	if err != nil {
		return err
	}
	return c.decodeJSON(resp, nil)
}

// openCodeServeEventStream abre o SSE /event e entrega cada evento ao handler.
// Retorna um canal de erro (ou erro de abertura). Bloqueia até a conexão
// fechar ou o ctx ser cancelado — chamar em goroutine.
// NOTA (Etapa B): o SSE é conexão LONGA — usa client PRÓPRIO sem timeout
// global (o http.Client do client tem Timeout 320s, que cortaria o stream
// a cada ~5min; o ctx controla a vida da conexão).
func (c *opencodeServeClient) eventStream(ctx context.Context, handler func(ev openCodeServeEvent)) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/event", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("opencode serve: /event HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev openCodeServeEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		handler(ev)
	}
	return sc.Err()
}

// openCodeServeMessageOpts — opções da mensagem síncrona/assíncrona.
type openCodeServeMessageOpts struct {
	NoReply    bool
	Agent      string
	System     string
	ModelID    string
	ProviderID string
}

// ─── Rotas de teste (Fase 3, Passo 2) ────────────────────────────────────────
// Registradas SOMENTE com OPENCODE_SERVE_TEST=1 (ausente em produção), para o
// teste isolado em porta separada. Protegidas por requireHokAuth. O cliente
// só opera se OPENCODE_SERVE_PASSWORD estiver definida (fail closed).

func init() {
	if os.Getenv("OPENCODE_SERVE_TEST") != "1" {
		return
	}
	http.HandleFunc("/opencode/serve/status", handleOpenCodeServeStatus)
	http.HandleFunc("/opencode/serve/session", handleOpenCodeServeSession)
	http.HandleFunc("/opencode/serve/sessions", handleOpenCodeServeSessions)
	http.HandleFunc("/opencode/serve/message", handleOpenCodeServeMessage)
	http.HandleFunc("/opencode/serve/summarize", handleOpenCodeServeSummarize)
}

func openCodeServeTestClient(w http.ResponseWriter, r *http.Request) (*opencodeServeClient, bool) {
	if !requireHokAuth(w, r) {
		return nil, false
	}
	c := newOpenCodeServeClient()
	if c.password == "" {
		jsonError(w, "OPENCODE_SERVE_PASSWORD nao definida (fail closed)", http.StatusServiceUnavailable)
		return nil, false
	}
	return c, true
}

// handleOpenCodeServeStatus — GET /opencode/serve/status: mostra a config do
// cliente (sem segredos) e o health do servidor opencode.
func handleOpenCodeServeStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	c, ok := openCodeServeTestClient(w, r)
	if !ok {
		return
	}
	healthy := false
	status := "down"
	req, err := http.NewRequest("GET", c.baseURL+"/api/health", nil)
	if err == nil {
		req.SetBasicAuth(c.username, c.password)
		resp, err := c.http.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				healthy = true
				status = "up"
			}
		}
	}
	respondJSON(w, map[string]interface{}{
		"status":  status,
		"healthy": healthy,
		"baseURL": c.baseURL,
		"user":    c.username,
	})
}

// handleOpenCodeServeSession — POST /opencode/serve/session:
//   - com {conv_id, tenant_id, user_id}: get-or-create (reaproveita a sessão
//     persistida da conversa, cria e persiste se não existir);
//   - só com {title}: cria sessão solta (sem persistência por conv).
// GET  /opencode/serve/session?id=... consulta a sessão pelo id.
func handleOpenCodeServeSession(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	c, ok := openCodeServeTestClient(w, r)
	if !ok {
		return
	}
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Title    string `json:"title"`
			ConvID   string `json:"conv_id"`
			TenantID string `json:"tenant_id"`
			UserID   string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonError(w, "body invalido: "+err.Error(), http.StatusBadRequest)
			return
		}
		if req.ConvID != "" || req.TenantID != "" || req.UserID != "" {
			// get-or-create com persistência por conversa.
			if req.ConvID == "" || req.TenantID == "" || req.UserID == "" {
				jsonError(w, "conv_id, tenant_id e user_id obrigatorios juntos", http.StatusBadRequest)
				return
			}
			sessionID, reused, err := getOrCreateOpenCodeServeSession(req.ConvID, req.TenantID, req.UserID, req.Title, c)
			if err != nil {
				jsonError(w, err.Error(), http.StatusBadGateway)
				return
			}
			respondJSON(w, map[string]interface{}{
				"sessionID": sessionID,
				"reused":    reused,
				"conv_id":   req.ConvID,
			})
			return
		}
		s, err := c.createSession(req.Title)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadGateway)
			return
		}
		respondJSON(w, s)
	case http.MethodGet:
		sessionID := r.URL.Query().Get("id")
		if sessionID == "" {
			jsonError(w, "parametro id obrigatorio", http.StatusBadRequest)
			return
		}
		s, err := c.getSession(sessionID)
		if err != nil {
			jsonError(w, err.Error(), http.StatusBadGateway)
			return
		}
		respondJSON(w, s)
	default:
		jsonError(w, "metodo nao suportado", http.StatusMethodNotAllowed)
	}
}

// handleOpenCodeServeSessions — GET /opencode/serve/sessions
// ?conv_id=...&tenant_id=...&user_id=...: devolve o mapeamento persistido
// (linha da tabela session_mode) para validar a persistência.
func handleOpenCodeServeSessions(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if _, ok := openCodeServeTestClient(w, r); !ok {
		return
	}
	convID := r.URL.Query().Get("conv_id")
	tenantID := r.URL.Query().Get("tenant_id")
	userID := r.URL.Query().Get("user_id")
	if convID == "" || tenantID == "" || userID == "" {
		jsonError(w, "conv_id, tenant_id e user_id obrigatorios", http.StatusBadRequest)
		return
	}
	var (
		mode    string
		setBy   string
		sid     string
		updated int64
	)
	err := db.QueryRow(
		`SELECT mode, set_by, opencode_session_id, updated_at FROM session_mode
		 WHERE tenant_id = ? AND user_id = ? AND conv_id = ?`,
		tenantID, userID, convID,
	).Scan(&mode, &setBy, &sid, &updated)
	if err != nil {
		jsonError(w, "nenhuma sessao persistida para esta conversa", http.StatusNotFound)
		return
	}
	respondJSON(w, map[string]interface{}{
		"conv_id":       convID,
		"tenant_id":     tenantID,
		"user_id":       userID,
		"mode":          mode,
		"set_by":        setBy,
		"opencode_sid":  sid,
		"updated_at":    updated,
	})
}

// handleOpenCodeServeMessage — POST /opencode/serve/message
// {sessionID, text, noReply, model, agent} — envia e devolve a resposta
// (info + parts + texto concatenado).
func handleOpenCodeServeMessage(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	c, ok := openCodeServeTestClient(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, "metodo nao suportado", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID string `json:"sessionID"`
		Text      string `json:"text"`
		NoReply   bool   `json:"noReply"`
		Model     string `json:"model"`
		Agent     string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "body invalido: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" || strings.TrimSpace(req.Text) == "" {
		jsonError(w, "sessionID e text obrigatorios", http.StatusBadRequest)
		return
	}
	opts := openCodeServeMessageOpts{NoReply: req.NoReply, Agent: req.Agent}
	if req.Model != "" && req.Model != "auto" {
		// aceita "openrouter/<id>" ou id simples (normaliza como o CLI)
		opts.ProviderID, opts.ModelID = openCodeServeSplitModel(req.Model)
	}
	start := time.Now()
	m, err := c.sendMessage(req.SessionID, req.Text, opts)
	if err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	respondJSON(w, map[string]interface{}{
		"sessionID": req.SessionID,
		"message":   m,
		"text":      openCodeServeMessageText(m),
		"elapsedMs": time.Since(start).Milliseconds(),
	})
}

// handleOpenCodeServeSummarize — POST /opencode/serve/summarize
// {sessionID, providerID, modelID} — dispara a compactação assíncrona.
func handleOpenCodeServeSummarize(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	c, ok := openCodeServeTestClient(w, r)
	if !ok {
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, "metodo nao suportado", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SessionID  string `json:"sessionID"`
		ProviderID string `json:"providerID"`
		ModelID    string `json:"modelID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "body invalido: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SessionID == "" {
		jsonError(w, "sessionID obrigatorio", http.StatusBadRequest)
		return
	}
	// fallback: provider/model padrão do catálogo ativo
	if req.ProviderID == "" || req.ModelID == "" {
		req.ProviderID, req.ModelID = openCodeServeSplitModel(opencodeModelID(getActiveModel()))
	}
	if err := c.summarizeSession(req.SessionID, req.ProviderID, req.ModelID); err != nil {
		jsonError(w, err.Error(), http.StatusBadGateway)
		return
	}
	respondJSON(w, map[string]interface{}{
		"status":    "accepted",
		"sessionID": req.SessionID,
		"note":      "compactacao assincrona (eventos SSE: session.compacted / session.idle)",
	})
}

// openCodeServeSplitModel separa "providerID/modelID" (ex.: "openrouter/
// deepseek/deepseek-chat-v3.1") no par exigido pelo /message e /summarize.
func openCodeServeSplitModel(m string) (providerID, modelID string) {
	m = strings.TrimSpace(m)
	if m == "" {
		return "", ""
	}
	if i := strings.Index(m, "/"); i > 0 {
		rest := m[i+1:]
		if rest != "" {
			return m[:i], rest
		}
	}
	return "", m
}

// logOpenCodeServe registra eventos do cliente no log de auditoria (mesmo
// padrão do opencode_client.go).
func logOpenCodeServe(tag, detail string) {
	sqliteExecParams(
		`INSERT INTO logs (event, level, source) VALUES (?, 'INFO', 'opencode_serve_client');`,
		tag+" "+detail,
	)
	_ = log.Output(2, "opencode_serve_client: "+tag+" "+detail)
}