package main

// agent_orchestrator.go
//
// BLOCO 1 — Orquestrador + subagentes (n8n Agents / HOK 100%).
//
// O HOK já tinha um loop de agente único (RunAgentLoop). Este arquivo adiciona
// a ORQUESTRAÇÃO: um agente principal (orchestrator) que delega tarefas para
// subagentes especializados, cada um com instruções, tools e base de
// conhecimento próprias — espelhando o modelo do n8n Agents (Preview, 2.32.3+).
//
// Estrutura:
//   - hok_agents      : tabela de agentes (orchestrator + subagentes)
//   - hok_agent_runs  : sessões/tracing (BLOCO 4, preenchido aqui)
//   - RunOrchestrator : loop do orquestrador — decide, delega, avalia, repete
//   - runSubagent     : executa um subagente específico (loop com tools próprias)

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// ─── Modelo ────────────────────────────────────────────────────────────────

type HOKAgent struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Desc        string   `json:"desc"`
	Kind        string   `json:"kind"` // "orchestrator" | "subagent"
	Instructions string  `json:"instructions"`
	Tools       []string `json:"tools"` // nomes de tools permitidas ("" = todas)
	Model       string   `json:"model"` // vazio = modelo ativo
	Knowledge   string   `json:"knowledge"` // base de conhecimento (texto/skill)
	Active      bool     `json:"active"`
	CreatedAt   string   `json:"created_at"`
}

type SubagentResult struct {
	Agent   string `json:"agent"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
	Seconds float64 `json:"seconds"`
}

type OrchestratorRequest struct {
	Task     string `json:"task"`
	AgentID  string `json:"agent_id,omitempty"` // roda um agente específico
	Model    string `json:"model,omitempty"`
	MaxSteps int    `json:"max_steps,omitempty"`
	ConvID   string `json:"conv_id,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

type OrchestratorResponse struct {
	Task      string            `json:"task"`
	Reply     string            `json:"reply"`
	Subagents []SubagentResult  `json:"subagents"`
	Steps     int               `json:"steps"`
	ModelUsed string            `json:"model_used"`
	Tracing   []AgentTraceEntry `json:"tracing"`
}

type AgentTraceEntry struct {
	Step  int    `json:"step"`
	Kind  string `json:"kind"` // "orchestrator" | "subagent" | "tool"
	Agent string `json:"agent,omitempty"`
	Tool  string `json:"tool,omitempty"`
	Input string `json:"input,omitempty"`
	Output string `json:"output,omitempty"`
	Ts    string `json:"ts"`
}

// ─── Persistência ──────────────────────────────────────────────────────────

// initAgentOrchestratorSchema — cria as tabelas do orquestrador. Chamado a
// partir de initSQLite() (db já pronto), NÃO em init() (evita corrida com db).
func initAgentOrchestratorSchema() {
	sqliteExec(`CREATE TABLE IF NOT EXISTS hok_agents (
		id           TEXT PRIMARY KEY,
		name         TEXT NOT NULL,
		desc         TEXT DEFAULT '',
		kind         TEXT DEFAULT 'subagent',
		instructions TEXT DEFAULT '',
		tools        TEXT DEFAULT '',
		model        TEXT DEFAULT '',
		knowledge    TEXT DEFAULT '',
		active       INTEGER DEFAULT 1,
		created_at   TEXT DEFAULT CURRENT_TIMESTAMP
	);`)
	sqliteExec(`CREATE TABLE IF NOT EXISTS hok_agent_runs (
		id         TEXT PRIMARY KEY,
		agent_id   TEXT,
		agent_name TEXT,
		task       TEXT,
		reply      TEXT,
		steps      INTEGER DEFAULT 0,
		model      TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP
	);`)
	sqliteExec(`CREATE TABLE IF NOT EXISTS hok_agent_run_steps (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id   TEXT,
		step     INTEGER,
		kind     TEXT,
		agent    TEXT,
		tool     TEXT,
		input    TEXT,
		output   TEXT,
		ts       TEXT
	);`)
}

func newAgentID() string {
	return fmt.Sprintf("ag_%d", time.Now().UnixNano()%1_000_000_000)
}

func saveAgent(a *HOKAgent) {
	toolsJSON, _ := json.Marshal(a.Tools)
	sqliteExecParams(`INSERT INTO hok_agents (id, name, desc, kind, instructions, tools, model, knowledge, active, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,COALESCE(?,CURRENT_TIMESTAMP))
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name, desc=excluded.desc, kind=excluded.kind,
			instructions=excluded.instructions, tools=excluded.tools,
			model=excluded.model, knowledge=excluded.knowledge, active=excluded.active;`,
		a.ID, a.Name, a.Desc, a.Kind, a.Instructions, string(toolsJSON),
		a.Model, a.Knowledge, boolToInt(a.Active), a.CreatedAt)
}

func listAgents() []HOKAgent {
	rows := sqliteExecParams(`SELECT id, name, desc, kind, instructions, tools, model, knowledge, active, created_at
		FROM hok_agents ORDER BY kind='orchestrator' DESC, name ASC;`)
	var out []HOKAgent
	for _, ln := range strings.Split(strings.TrimSpace(rows), "\n") {
		if ln == "" {
			continue
		}
		cols := strings.SplitN(ln, "|", 10)
		if len(cols) < 10 {
			continue
		}
		a := HOKAgent{
			ID: cols[0], Name: cols[1], Desc: cols[2], Kind: cols[3],
			Instructions: cols[4], Model: cols[6], Knowledge: cols[7],
			Active: cols[8] == "1", CreatedAt: cols[9],
		}
		_ = json.Unmarshal([]byte(cols[5]), &a.Tools)
		out = append(out, a)
	}
	return out
}

func getAgent(id string) *HOKAgent {
	for _, a := range listAgents() {
		if a.ID == id {
			aa := a
			return &aa
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// agentEffectiveModel — modelo do agente ou modelo ativo global.
func agentEffectiveModel(a *HOKAgent) string {
	if a != nil && a.Model != "" {
		return a.Model
	}
	if m := os.Getenv("MINIMAX_AGENT_MODEL"); m != "" {
		return m
	}
	if m := getActiveModel(); m != "" {
		return m
	}
	return ModelB
}

// agentAllowedTools — tools do agente; vazio = todas as do catálogo base.
func agentAllowedTools(a *HOKAgent) []toolDef {
	base := agentTools()
	if a == nil || len(a.Tools) == 0 {
		return base
	}
	allow := map[string]bool{}
	for _, t := range a.Tools {
		allow[t] = true
	}
	var out []toolDef
	for _, t := range base {
		if allow[t.Function.Name] {
			out = append(out, t)
		}
	}
	return out
}

// ─── Rotas ─────────────────────────────────────────────────────────────────

// registerAgentRoutes — registra as rotas do bloco orquestrador.
func registerAgentRoutes() {
	http.HandleFunc("/agents/orchestrate", handleOrchestrate)
	http.HandleFunc("/agents/crud", handleAgentCRUD)
	http.HandleFunc("/agents/runs", handleAgentRuns)
}

func handleAgentCRUD(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, map[string]interface{}{"status": "ok", "agents": listAgents()})
	case http.MethodPost:
		var a HOKAgent
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			respondJSON(w, map[string]string{"status": "error", "message": "JSON invalido: " + err.Error()})
			return
		}
		if a.Name == "" {
			respondJSON(w, map[string]string{"status": "error", "message": "nome obrigatorio"})
			return
		}
		if a.ID == "" {
			a.ID = newAgentID()
		}
		if a.Kind == "" {
			a.Kind = "subagent"
		}
		// active default true quando o campo não veio no payload
		if !a.Active && a.CreatedAt == "" {
			a.Active = true
		}
		a.CreatedAt = time.Now().Format(time.RFC3339)
		saveAgent(&a)
		log.Printf("[AUDIT] agente salvo: id=%s name=%q kind=%s", a.ID, a.Name, a.Kind)
		respondJSON(w, map[string]interface{}{"status": "ok", "agent": a})
	case http.MethodDelete:
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
			respondJSON(w, map[string]string{"status": "error", "message": "id obrigatorio"})
			return
		}
		sqliteExecParams(`DELETE FROM hok_agents WHERE id=?;`, req.ID)
		log.Printf("[AUDIT] agente removido: id=%s", req.ID)
		respondJSON(w, map[string]string{"status": "ok"})
	default:
		respondJSON(w, map[string]string{"status": "error", "message": "method not allowed"})
	}
}

// ─── Orquestrador ──────────────────────────────────────────────────────────

// RunOrchestrator — loop principal: decide qual subagente/tool chamar, delega,
// avalia o resultado e repete até concluir (máx. maxSteps). Preenche o tracing.
func RunOrchestrator(ctx context.Context, req OrchestratorRequest) OrchestratorResponse {
	model := req.Model
	if model == "" {
		model = agentEffectiveModel(nil)
	}
	maxSteps := req.MaxSteps
	if maxSteps <= 0 {
		maxSteps = maxAgentSteps
	}
	orchestrator := getDefaultOrchestrator(req.TenantID)
	resp := OrchestratorResponse{
		Task:      req.Task,
		ModelUsed: model,
		Tracing:   []AgentTraceEntry{},
	}

	// Modo "roda agente específico": pula a orquestração, executa direto.
	if req.AgentID != "" {
		if a := getAgent(req.AgentID); a != nil {
			start := time.Now()
			out, err := runSubagent(ctx, a, req.Task, model)
			sub := SubagentResult{Agent: a.Name, Output: out, Seconds: time.Since(start).Seconds()}
			if err != nil {
				sub.Error = err.Error()
			}
			resp.Subagents = append(resp.Subagents, sub)
			resp.Reply = out
			resp.Steps = 1
			resp.Tracing = append(resp.Tracing, AgentTraceEntry{
				Step: 1, Kind: "subagent", Agent: a.Name, Input: req.Task,
				Output: truncateStr(out, 600), Ts: time.Now().Format(time.RFC3339),
			})
			saveAgentRun(req, resp)
			return resp
		}
		resp.Reply = "Agente nao encontrado: " + req.AgentID
		return resp
	}

	// Loop do orquestrador.
	messages := []chatMessage{
		{Role: "system", Content: orchestratorInstructions(orchestrator, req.Task)},
	}
	subagents := listActiveSubagents()
	subagentDesc := buildSubagentCatalog(subagents)
	messages = append(messages, chatMessage{Role: "system", Content: subagentDesc})

	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		resp.Reply = "OPENROUTER_API_KEY nao definida"
		return resp
	}

	// Fallback de modelo: modelo ativo pode estar em rate-limit (ex:
	// z-ai/glm-5.2:free). Tenta fallback seguros com bom tool-use.
	fallbackChain := []string{ModelB}
	if model != ModelB {
		fallbackChain = append([]string{model}, fallbackChain...)
	}
	fallbackChain = append(fallbackChain, ModelB)

	usedModel := model

	for step := 1; step <= maxSteps; step++ {
		respMsg, finish, err := callGroqAgentLoop(ctx, apiKey, usedModel, messages, append(agentTools(), runEngineTool()))
		if err != nil {
			// troca para o próximo modelo da cadeia e reprocessa o passo
			if next := nextFallbackModel(fallbackChain, usedModel); next != "" {
				log.Printf("[orchestrator] modelo %s falhou (%v) — fallback %s", usedModel, err, next)
				usedModel = next
				resp.ModelUsed = next
				step--
				continue
			}
			resp.Reply = fmt.Sprintf("erro no passo %d: %v", step, err)
			return resp
		}
		if len(respMsg.ToolCalls) == 0 {
			resp.Reply = strings.TrimSpace(respMsg.Content)
			resp.Steps = step
			resp.ModelUsed = usedModel
			resp.Tracing = append(resp.Tracing, AgentTraceEntry{
				Step: step, Kind: "orchestrator", Agent: "orchestrator",
				Output: truncateStr(respMsg.Content, 600), Ts: time.Now().Format(time.RFC3339),
			})
			saveAgentRun(req, resp)
			return resp
		}
		messages = append(messages, respMsg)
		for _, tc := range respMsg.ToolCalls {
			// Delegação: tool virtual "delegate_to_<agent>".
			if strings.HasPrefix(tc.Function.Name, "delegate_to_") {
				name := strings.TrimPrefix(tc.Function.Name, "delegate_to_")
				target := findSubagentByName(subagents, name)
				if target == nil {
					result := "Subagente '" + name + "' nao existe. Disponiveis: " + strings.Join(subagentNames(subagents), ", ")
					messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
					resp.Tracing = append(resp.Tracing, AgentTraceEntry{
						Step: step, Kind: "tool", Tool: tc.Function.Name, Input: tc.Function.Arguments,
						Output: result, Ts: time.Now().Format(time.RFC3339),
					})
					continue
				}
				// Extrai o sub-tarefa dos argumentos.
				subTask := taskFromDelegateArgs(tc.Function.Arguments, req.Task)
				start := time.Now()
				out, err := runSubagent(ctx, target, subTask, usedModel)
				sub := SubagentResult{Agent: target.Name, Output: truncateStr(out, 800), Seconds: time.Since(start).Seconds()}
				if err != nil {
					sub.Error = err.Error()
				}
				resp.Subagents = append(resp.Subagents, sub)
				result := "Subagente " + target.Name + " retornou:\n" + sub.Output
				if sub.Error != "" {
					result = "Subagente " + target.Name + " falhou: " + sub.Error
				}
				messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
				resp.Tracing = append(resp.Tracing, AgentTraceEntry{
					Step: step, Kind: "subagent", Agent: target.Name, Input: subTask,
					Output: sub.Output, Ts: time.Now().Format(time.RFC3339),
				})
				continue
			}
			// Tool normal (executada direto).
			var result string
			if tc.Function.Name == "run_engine" {
				result = runEngineToolExec(ctx, tc.Function.Arguments)
			} else {
				result = executeTool(ctx, tc.Function.Name, tc.Function.Arguments)
			}
			messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
			resp.Tracing = append(resp.Tracing, AgentTraceEntry{
				Step: step, Kind: "tool", Tool: tc.Function.Name, Input: tc.Function.Arguments,
				Output: truncateStr(result, 600), Ts: time.Now().Format(time.RFC3339),
			})
		}
		_ = finish
	}
	resp.Reply = fmt.Sprintf("orquestracao excedeu %d passos sem resposta final", maxSteps)
	resp.Steps = maxSteps
	saveAgentRun(req, resp)
	return resp
}

// runSubagent — executa UM subagente com suas tools e instruções próprias.
func runSubagent(ctx context.Context, a *HOKAgent, task string, model string) (string, error) {
	if !a.Active {
		return "", fmt.Errorf("agente inativo: %s", a.Name)
	}
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY nao definida")
	}
	messages := []chatMessage{
		{Role: "system", Content: subagentSystemPrompt(a, task)},
		{Role: "user", Content: task},
	}
	tools := agentAllowedTools(a)
	// Adiciona a tool run_engine para que o subagente também possa delegar a
	// claude/opencode/hermes quando a tarefa exigir execução real no servidor.
	tools = append(tools, runEngineTool())
	usedModel := model
	fallbackChain := []string{ModelB}
	if model != ModelB {
		fallbackChain = append([]string{model}, fallbackChain...)
	}
	for step := 1; step <= 5; step++ {
		respMsg, _, err := callGroqAgentLoop(ctx, apiKey, usedModel, messages, tools)
		if err != nil {
			if next := nextFallbackModel(fallbackChain, usedModel); next != "" {
				log.Printf("[subagent %s] modelo %s falhou (%v) — fallback %s", a.Name, usedModel, err, next)
				usedModel = next
				step--
				continue
			}
			return "", err
		}
		if len(respMsg.ToolCalls) == 0 {
			return strings.TrimSpace(respMsg.Content), nil
		}
		messages = append(messages, respMsg)
		for _, tc := range respMsg.ToolCalls {
			if tc.Function.Name == "run_engine" {
				result := runEngineToolExec(ctx, tc.Function.Arguments)
				messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
				continue
			}
			result := executeTool(ctx, tc.Function.Name, tc.Function.Arguments)
			messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
		}
	}
	return "", fmt.Errorf("subagente %s excedeu passos sem resposta final", a.Name)
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func getDefaultOrchestrator(tenantID string) *HOKAgent {
	for _, a := range listAgents() {
		if a.Kind == "orchestrator" && a.Active {
			return &a
		}
	}
	return &HOKAgent{
		ID: "orchestrator_default", Name: "Orquestrador",
		Kind: "orchestrator", Instructions: "Distribua a tarefa entre os subagentes disponiveis.",
		Active: true,
	}
}

func listActiveSubagents() []HOKAgent {
	var out []HOKAgent
	for _, a := range listAgents() {
		if a.Kind == "subagent" && a.Active {
			out = append(out, a)
		}
	}
	return out
}

func orchestratorInstructions(a *HOKAgent, task string) string {
	inst := a.Instructions
	if inst == "" {
		inst = "Voce e o orquestrador de agentes do HOK. Distribua a tarefa entre os subagentes disponiveis (usando delegate_to_<nome>) ou resolva com as tools, conforme achar melhor."
	}
	return fmt.Sprintf("Voce e o orquestrador do HOK. Tarefa: %s\n\n%s\n\nUse delegate_to_<nome> para delegar a um subagente. Depois que os resultados chegarem, responda em portugues (PT-BR).", task, inst)
}

func subagentSystemPrompt(a *HOKAgent, task string) string {
	inst := a.Instructions
	if inst == "" {
		inst = "Resolva a tarefa com as tools disponiveis e responda em portugues (PT-BR)."
	}
	p := fmt.Sprintf("Voce e o agente '%s' do HOK.\n%s\n", a.Name, inst)
	if a.Knowledge != "" {
		p += "\nBASE DE CONHECIMENTO:\n" + truncateStr(a.Knowledge, 4000) + "\n"
	}
	return p
}

func buildSubagentCatalog(agents []HOKAgent) string {
	if len(agents) == 0 {
		return "Nenhum subagente configurado. Resolva a tarefa diretamente com as tools disponiveis."
	}
	var sb strings.Builder
	sb.WriteString("SUBAGENTES DISPONIVEIS (use delegate_to_<nome> nos arguments como JSON {\"task\":\"...\"}):\n")
	for _, a := range agents {
		desc := a.Desc
		if desc == "" {
			desc = a.Instructions
		}
		sb.WriteString(fmt.Sprintf("- delegate_to_%s: %s\n", a.Name, truncateStr(desc, 200)))
	}
	return sb.String()
}

func subagentNames(agents []HOKAgent) []string {
	var out []string
	for _, a := range agents {
		out = append(out, "delegate_to_"+a.Name)
	}
	return out
}

func findSubagentByName(agents []HOKAgent, name string) *HOKAgent {
	for i := range agents {
		if agents[i].Name == name {
			return &agents[i]
		}
	}
	return nil
}

func taskFromDelegateArgs(argsJSON, fallback string) string {
	var a struct {
		Task string `json:"task"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &a); err == nil && a.Task != "" {
		return a.Task
	}
	return fallback
}

func saveAgentRun(req OrchestratorRequest, resp OrchestratorResponse) {
	agentName := "orchestrator"
	if req.AgentID != "" {
		if a := getAgent(req.AgentID); a != nil {
			agentName = a.Name
		}
	}
	runID := fmt.Sprintf("run_%d", time.Now().UnixNano()%1_000_000_000)
	stepsJSON, _ := json.Marshal(resp.Tracing)
	// grava resumo na tabela principal
	sqliteExecParams(`INSERT INTO hok_agent_runs (id, agent_id, agent_name, task, reply, steps, model)
		VALUES (?,?,?,?,?,?,?);`,
		runID, req.AgentID, agentName, truncateStr(req.Task, 500),
		truncateStr(resp.Reply, 2000), resp.Steps, resp.ModelUsed)
	// grava passos para tracing
	for _, tr := range resp.Tracing {
		sqliteExecParams(`INSERT INTO hok_agent_run_steps (run_id, step, kind, agent, tool, input, output, ts)
			VALUES (?,?,?,?,?,?,?,?);`,
			runID, tr.Step, tr.Kind, tr.Agent, tr.Tool,
			truncateStr(tr.Input, 1000), truncateStr(tr.Output, 2000), tr.Ts)
	}
	_ = stepsJSON
}

func handleOrchestrate(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		respondJSON(w, map[string]string{"status": "error", "message": "POST esperado"})
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var req OrchestratorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Task == "" {
		respondJSON(w, map[string]string{"status": "error", "message": "task obrigatoria"})
		return
	}
	req.ConvID = convIdFromRequest(r)
	req.TenantID = tenantIdFromRequest(r)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	resp := RunOrchestrator(ctx, req)
	respondJSON(w, map[string]interface{}{"status": "ok", "result": resp})
}

func handleAgentRuns(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	// GET /agents/runs?run_id=... → detalhe com passos (tracing completo)
	if runID := r.URL.Query().Get("run_id"); runID != "" {
		handleAgentRunDetail(w, runID)
		return
	}
	// sqliteExecQuoted escapa corretamente vírgulas/aspas em conteúdo —
	// o parsing com encoding/csv é robusto contra "|" e quebras de linha.
	rows := sqliteExecQuoted(`SELECT id, agent_name, task, reply, steps, model, created_at
		FROM hok_agent_runs ORDER BY created_at DESC LIMIT 50;`)
	type runRow struct {
		ID        string `json:"id"`
		AgentName string `json:"agent_name"`
		Task      string `json:"task"`
		Reply     string `json:"reply"`
		Steps     int    `json:"steps"`
		Model     string `json:"model"`
		CreatedAt string `json:"created_at"`
	}
	var out []runRow
	reader := strings.NewReader(rows)
	csvR := csv.NewReader(reader)
	csvR.FieldsPerRecord = -1
	records, err := csvR.ReadAll()
	if err == nil {
		for _, rec := range records {
			if len(rec) < 7 {
				continue
			}
			out = append(out, runRow{ID: rec[0], AgentName: rec[1], Task: rec[2],
				Reply: rec[3], Steps: atoiDefault(rec[4], 0), Model: rec[5], CreatedAt: rec[6]})
		}
	} else {
		// fallback: parse simples por linha (apenas se sem vírgulas)
		for _, ln := range strings.Split(strings.TrimSpace(rows), "\n") {
			if ln == "" {
				continue
			}
			cols := strings.SplitN(ln, "|", 7)
			if len(cols) < 7 {
				continue
			}
			out = append(out, runRow{ID: cols[0], AgentName: cols[1], Task: cols[2],
				Reply: cols[3], Steps: atoiDefault(cols[4], 0), Model: cols[5], CreatedAt: cols[6]})
		}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "runs": out})
}

func atoiDefault(s string, d int) int {
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d", &n); err != nil {
		return d
	}
	return n
}

// handleAgentRunDetail — GET /agents/runs?run_id=X → resumo + passos (tracing).

// runEngineTool — tool que o orquestrador/subagente usa para delegar a um
// engine real do servidor (claude, opencode, hermes). Integra os engines
// existentes como "subagentes" de execução.
func runEngineTool() toolDef {
	t := toolDef{Type: "function"}
	t.Function.Name = "run_engine"
	t.Function.Description = "Executa uma tarefa em um engine real do servidor: claude (Claude Code), opencode (OpenCode Terminal) ou hermes. Use quando a tarefa exigir edicao de arquivos, execucao de comandos, deploy ou raciocinio profundo de um engine especializado."
	t.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"engine": map[string]interface{}{
				"type": "string",
				"enum": []string{"claude", "opencode", "hermes"},
				"description": "Qual engine executar.",
			},
			"task": map[string]interface{}{
				"type":        "string",
				"description": "A tarefa/prompt a enviar ao engine.",
			},
		},
		"required": []string{"engine", "task"},
	}
	return t
}

// runEngineToolExec — executa a tool run_engine (seguro: fluxos aprovados).
func runEngineToolExec(ctx context.Context, argsJSON string) string {
	var args struct {
		Engine string `json:"engine"`
		Task   string `json:"task"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "erro: argumentos invalidos: " + err.Error()
	}
	if args.Task == "" {
		return "erro: task obrigatoria"
	}
	ctxExec, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	switch args.Engine {
	case "claude":
		out, err := callClaudeCode(ctxExec, args.Task)
		if err != nil {
			return "erro claude: " + err.Error()
		}
		return out
	case "opencode":
		out, err := callOpenCode(ctxExec, args.Task, "orchestrator", "owner", "owner")
		if err != nil {
			return "erro opencode: " + err.Error()
		}
		return out
	case "hermes":
		out, err := callHermes(args.Task)
		if err != nil {
			return "erro hermes: " + err.Error()
		}
		return out
	default:
		return "erro: engine desconhecido (use claude, opencode ou hermes)"
	}
}
func handleAgentRunDetail(w http.ResponseWriter, runID string) {
	rows := sqliteExecQuoted(`SELECT id, agent_name, task, reply, steps, model, created_at
		FROM hok_agent_runs WHERE id=?;`, runID)
	type runRow struct {
		ID        string `json:"id"`
		AgentName string `json:"agent_name"`
		Task      string `json:"task"`
		Reply     string `json:"reply"`
		Steps     int    `json:"steps"`
		Model     string `json:"model"`
		CreatedAt string `json:"created_at"`
	}
	var run *runRow
	reader := strings.NewReader(rows)
	csvR := csv.NewReader(reader)
	csvR.FieldsPerRecord = -1
	if records, err := csvR.ReadAll(); err == nil && len(records) > 0 && len(records[0]) >= 7 {
		rec := records[0]
		run = &runRow{ID: rec[0], AgentName: rec[1], Task: rec[2], Reply: rec[3],
			Steps: atoiDefault(rec[4], 0), Model: rec[5], CreatedAt: rec[6]}
	}
	if run == nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "run nao encontrada"})
		return
	}

	steps := []AgentTraceEntry{}
	stepRows := sqliteExecQuoted(`SELECT step, kind, agent, tool, input, output, ts
		FROM hok_agent_run_steps WHERE run_id=? ORDER BY step ASC, id ASC;`, runID)
	stepReader := strings.NewReader(stepRows)
	sc := csv.NewReader(stepReader)
	sc.FieldsPerRecord = -1
	if records, err := sc.ReadAll(); err == nil {
		for _, rec := range records {
			if len(rec) < 7 {
				continue
			}
			si, _ := strconv.Atoi(strings.TrimSpace(rec[0]))
			steps = append(steps, AgentTraceEntry{
				Step: si, Kind: rec[1], Agent: rec[2], Tool: rec[3],
				Input: rec[4], Output: rec[5], Ts: rec[6],
			})
		}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "run": run, "steps": steps})
}

// nextFallbackModel — próximo modelo da cadeia de fallback após o atual.
func nextFallbackModel(chain []string, current string) string {
	seen := false
	for _, m := range chain {
		if seen && m != current {
			return m
		}
		if m == current {
			seen = true
		}
	}
	return ""
}