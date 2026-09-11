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
	"regexp"
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
	Task       string `json:"task"`
	AgentID    string `json:"agent_id,omitempty"`
	Model      string `json:"model,omitempty"`
	MaxSteps   int    `json:"max_steps,omitempty"`
	ConvID     string `json:"conv_id,omitempty"`
	TenantID   string `json:"tenant_id,omitempty"`
	Mode       string `json:"mode,omitempty"`
	ResumeFrom *OrchestratorState `json:"resume_from,omitempty"`
	ApprovalResult string `json:"approval_result,omitempty"`
}

// OrchestratorState — snapshot do loop do orquestrador para retomada
// após aprovação de pending_action no modo build.
type OrchestratorState struct {
	Messages  []chatMessage `json:"messages"`
	Step      int           `json:"step"`
	UsedModel string        `json:"used_model"`
	Plan      []string      `json:"plan,omitempty"`
	Completed int           `json:"completed"`
	NextTask  string        `json:"next_task,omitempty"`
}

// orchestratorStateRow — linha da tabela orchestrator_state (DB).
type orchestratorStateRow struct {
	ConvID   string
	TenantID string
	Task     string
	Messages string // JSON
	Step     int
	Model    string
	Mode     string
	AgentID  string
	MaxSteps int
	ExpiresAt string
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
	if a != nil && a.Model != "" && isFreeModel(a.Model) {
		return a.Model
	}
	if m := os.Getenv("MINIMAX_AGENT_MODEL"); m != "" && isFreeModel(m) {
		return m
	}
	if m := getActiveModel(); m != "" && isFreeModel(m) {
		return m
	}
	return ModelB
}

// safeDefaultTools — catálogo SEGURO (somente leitura/diagnóstico) para
// subagentes criados SEM `tools` explícito no DB. Substitui o antigo default
// perigoso, que devolvia o catálogo COMPLETO (n8n_create_workflow,
// n8n_update_workflow, n8n_activate_workflow, n8n_delete_workflow,
// n8n_execute_workflow, n8n_test_workflow, add_imovel) e bash_exec.
// Um subagente que precise de escrita/execução deve listar as tools
// explicitamente em hok_agents.tools. `run_engine` NÃO entra aqui — ela é
// anexada separadamente pelo runSubagent (comportamento mantido).
var safeDefaultTools = map[string]bool{
	"read_file":                true,
	"env_diagnose_config":      true,
	"n8n_list_workflows":       true,
	"n8n_diagnose_workflow":    true,
	"n8n_get_workflow_detail":  true,
	"n8n_get_execution_errors": true,
	"n8n_expert_lookup":        true,
}

// agentAllowedTools — tools do agente. `tools` vazio no DB = catálogo SEGURO
// (read-only); `tools` preenchido = interseção com o catálogo base.
func agentAllowedTools(a *HOKAgent) []toolDef {
	base := agentTools()
	allow := safeDefaultTools
	if a != nil && len(a.Tools) > 0 {
		allow = map[string]bool{}
		for _, t := range a.Tools {
			allow[t] = true
		}
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

// parsePlan - converte task string em slice de mutacoes.
func parsePlan(task string) []string {
	if task == "" {
		return nil
	}
	re := regexp.MustCompile(`(\d+)\)\s*([^,]+)`)
	matches := re.FindAllStringSubmatch(task, -1)
	if len(matches) > 0 {
		plan := make([]string, len(matches))
		for i, m := range matches {
			plan[i] = strings.TrimSpace(m[2])
		}
		return plan
	}
	return []string{task}
}

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

	// ── RETOMADA: restaurar estado do orquestrador após aprovação ──
	var messages []chatMessage
	startStep := 1
	usedModel := model
	subagents := listActiveSubagents()
	if req.ResumeFrom != nil && len(req.ResumeFrom.Messages) > 0 {
		messages = make([]chatMessage, len(req.ResumeFrom.Messages))
		copy(messages, req.ResumeFrom.Messages)
		startStep = req.ResumeFrom.Step + 1
		usedModel = req.ResumeFrom.UsedModel
		resp.ModelUsed = usedModel
		// Atualizar system message com task de progresso
		if req.Task != "" && len(messages) > 0 && messages[0].Role == "system" {
			messages[0] = chatMessage{Role: "system", Content: req.Task}
		}
		// Injetar resultado da aprovação + instrução de continuação
		if req.ApprovalResult != "" {
			for i := len(messages) - 1; i >= 0; i-- {
				if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
					for _, tc := range messages[i].ToolCalls {
						messages = append(messages, chatMessage{
							Role:       "tool",
							ToolCallID: tc.ID,
							Name:       tc.Function.Name,
							Content:    req.ApprovalResult,
						})
					}
					break
				}
			}
		}
		log.Printf("[orchestrator] retomando conv=%s step=%d model=%s (state restaurado, %d messages)",
			req.ConvID, startStep, usedModel, len(messages))
	} else {
		// Fluxo normal: montar messages do zero
		messages = []chatMessage{
			{Role: "system", Content: orchestratorInstructions(orchestrator, req.Task)},
		}
		subagentDesc := buildSubagentCatalog(subagents)
		messages = append(messages, chatMessage{Role: "system", Content: subagentDesc})
	}

	// Modo "roda agente específico": pula a orquestração, executa direto.
	if req.AgentID != "" {
		if a := getAgent(req.AgentID); a != nil {
			start := time.Now()
			out, err := runSubagent(ctx, a, req.Task, model, req.Mode, req.ConvID, req.TenantID)
			sub := SubagentResult{Agent: a.Name, Output: out, Seconds: time.Since(start).Seconds()}
			if err != nil {
				sub.Error = err.Error()
			}
			resp.Subagents = append(resp.Subagents, sub)
			// FIX: Se o agente retornou um pending_action (build mode),
			// retorna imediatamente ao usuário para aprovação.
			if strings.Contains(out, "Confirma?") && req.Mode == "build" {
				resp.Reply = out
				resp.Steps = 1
				resp.ModelUsed = model
				resp.Tracing = append(resp.Tracing, AgentTraceEntry{
					Step: 1, Kind: "pending_action", Agent: a.Name, Input: req.Task,
					Output: truncateStr(out, 600), Ts: time.Now().Format(time.RFC3339),
				})
				saveAgentRun(req, resp)
				return resp
			}
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
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		resp.Reply = "OPENROUTER_API_KEY nao definida"
		return resp
	}

	// Em build mode, restringir tools do orquestrador para forçar uso de run_engine.
	// Sem isso, o modelo usa read_file/bash_exec diretamente e ignora run_engine.
	orchestratorTools := agentTools()
	if req.Mode == "build" {
		var filtered []toolDef
		for _, t := range orchestratorTools {
			if t.Function.Name == "read_file" || t.Function.Name == "bash_exec" {
				continue // remover tools que bypassam run_engine
			}
			filtered = append(filtered, t)
		}
		orchestratorTools = filtered
	}

	// Cadeia de fallback para o orquestrador (FIX 11/09: SÓ modelos free —
	// ModelA é pago e ModelB foi descontinuado no OpenRouter; ambos causavam
	// cobrança silenciosa). ModelC (Nemotron-3-super-120b:free) suporta
	// tool-use e é o free verificado. O modelo pedido (se != ModelC) continua
	// como primário via usedModel; a cadeia é só para quando ele falha.
	fallbackChain := []string{ModelC}
	if model != ModelC {
		fallbackChain = append([]string{model}, fallbackChain...)
	}

	// FIX end-of-plan: se step >= len(Plan), responder direto sem chamar o LLM
	if req.ResumeFrom != nil && req.ResumeFrom.Step >= len(req.ResumeFrom.Plan) {
		log.Printf("[orchestrator] conv=%s ja concluido (step=%d >= len(Plan)=%d) — sem chamada LLM",
			req.ConvID, req.ResumeFrom.Step, len(req.ResumeFrom.Plan))
		resp.Reply = fmt.Sprintf("Plano concluido — %d de %d arquivos processados.", req.ResumeFrom.Step, len(req.ResumeFrom.Plan))
		resp.Steps = req.ResumeFrom.Step
		resp.ModelUsed = req.ResumeFrom.UsedModel
		return resp
	}
	// Se nao ha pendencia e o plano ja esta marcado como concluido
	if req.ResumeFrom != nil && req.ResumeFrom.Step >= len(req.ResumeFrom.Plan) && len(req.ResumeFrom.Plan) > 0 {
		log.Printf("[orchestrator] conv=%s Completed=%d >= len(Plan)=%d — sem chamada LLM",
			req.ConvID, req.ResumeFrom.Completed, len(req.ResumeFrom.Plan))
		resp.Reply = fmt.Sprintf("Nenhuma acao pendente — o plano ja foi concluido.")
		return resp
	}

	// Detecção de loop em três níveis:
	// 1. Pattern exato (nome+args) repetido 3x seguidas — modelo copiando chamada
	// 2. Mesmo conjunto de nomes de tools repetido nas últimas 3 steps — modelo
	//    variando argumentos mas sem progresso (ex: listar, listar com filtro, listar)
	// 3. Spinning: modelo chamando tools com content vazio por 4+ steps seguidos
	//    — não está raciocinando, só executando cegamente
	var lastExactPatterns [3]string
	var lastNamePatterns [3]string
	patternIdx := 0
	emptyContentCount := 0 // consecutive steps with empty content + tool calls

	for step := startStep; step <= maxSteps; step++ {
		// Gate de budget: autonomous_total exige budget disponível.
		if isAutonomousLike(req.Mode) {
			left := autonomousBudgetLeft(req.ConvID, req.TenantID, "anonymous")
			if left <= 0 {
				resp.Reply = "Budget esgotado. Aumente o budget via UI ou mude para modo Construir."
				resp.Steps = step - 1
				return resp
			}
		}
		respMsg, finish, err := callGroqAgentLoop(ctx, apiKey, usedModel, messages, append(orchestratorTools, runEngineTool()))
		if err != nil {
			// troca para o próximo modelo da cadeia e reprocessa o passo
			if next := nextFallbackModel(fallbackChain, usedModel); next != "" {
				log.Printf("[orchestrator] modelo %s falhou (%v) — fallback %s", usedModel, err, next)
				usedModel = next
				resp.ModelUsed = next
				step--
				continue
			}
			resp.Reply = fmt.Sprintf("Modelo %s não disponível via OpenRouter. Tente trocar o modelo em ⚡ IA.", usedModel)
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

		// Monta patterns deste step
		var exactBuilder, nameBuilder strings.Builder
		nameSet := make(map[string]bool)
		for _, tc := range respMsg.ToolCalls {
			exactBuilder.WriteString(tc.Function.Name)
			exactBuilder.WriteString("|")
			exactBuilder.WriteString(tc.Function.Arguments)
			exactBuilder.WriteString(";")
			if !nameSet[tc.Function.Name] {
				nameSet[tc.Function.Name] = true
				if nameBuilder.Len() > 0 {
					nameBuilder.WriteString(",")
				}
				nameBuilder.WriteString(tc.Function.Name)
			}
		}
		currExact := exactBuilder.String()
		currNames := nameBuilder.String()

		// DETECÇÃO DE LOOP NÍVEL 1: pattern exato repetido 3x
		exactLoop := currExact != "" && currExact == lastExactPatterns[0] && currExact == lastExactPatterns[1] && currExact == lastExactPatterns[2]
		// DETECÇÃO DE LOOP NÍVEL 2: mesmo conjunto de tools (só nomes) repetido 3x
		// (só dispara a partir do step 4, para dar tempo do modelo começar)
		nameLoop := step >= 4 && currNames != "" && currNames == lastNamePatterns[0] && currNames == lastNamePatterns[1] && currNames == lastNamePatterns[2]

		if exactLoop || nameLoop {
			loopType := "exato (mesmas tool calls+args)"
			if nameLoop && !exactLoop {
				loopType = "conjunto de tools (mesmas tools, args variando)"
			}
			log.Printf("[orchestrator] loop detectado no step %d (%s) — forçando resposta final", step, loopType)
			messages = append(messages, chatMessage{Role: "system", Content: "Detectei que você está repetindo as mesmas ações sem progresso. Pare de chamar ferramentas e dê sua resposta final em texto puro agora, resumindo o que você descobriu até aqui."})
			finalMsg, _, finalErr := callGroqAgentLoop(ctx, apiKey, usedModel, messages, nil)
			if finalErr == nil && strings.TrimSpace(finalMsg.Content) != "" {
				resp.Reply = strings.TrimSpace(finalMsg.Content) + "\n\n(Orquestrador: loop detectado após " + fmt.Sprintf("%d", step) + " passos — resposta forçada)"
				resp.Steps = step
				resp.ModelUsed = usedModel
				resp.Tracing = append(resp.Tracing, AgentTraceEntry{
					Step: step, Kind: "orchestrator_loop_break", Agent: "orchestrator",
					Output: truncateStr(finalMsg.Content, 600), Ts: time.Now().Format(time.RFC3339),
				})
				saveAgentRun(req, resp)
				return resp
			}
			resp.Reply = "Orquestrador entrou em loop (" + loopType + "). Tente reformular a tarefa ou usar um modelo diferente."
			resp.Steps = step
			resp.ModelUsed = usedModel
			resp.Tracing = append(resp.Tracing, AgentTraceEntry{
				Step: step, Kind: "orchestrator_loop_abort", Agent: "orchestrator",
				Output: loopType, Ts: time.Now().Format(time.RFC3339),
			})
			saveAgentRun(req, resp)
			return resp
		}
		lastExactPatterns[patternIdx%3] = currExact
		lastNamePatterns[patternIdx%3] = currNames
		patternIdx++

		// DETECÇÃO DE SPINNING: modelo chamando tools com content vazio por 4+ steps
		// — não está raciocinando, só executando cegamente. Força resposta final.
		if strings.TrimSpace(respMsg.Content) == "" {
			emptyContentCount++
		} else {
			emptyContentCount = 0
		}
		if emptyContentCount >= 2 {
			log.Printf("[orchestrator] spinning detectado no step %d (%d steps com content vazio) — forçando resposta final", step, emptyContentCount)
			messages = append(messages, chatMessage{Role: "system", Content: "Detectei que você está chamando ferramentas repetidamente sem gerar texto explicativo. Pare de usar ferramentas agora e dê sua resposta final em texto puro, resumindo o que você fez e o que descobriu."})
			finalMsg, _, finalErr := callGroqAgentLoop(ctx, apiKey, usedModel, messages, nil)
			if finalErr == nil && strings.TrimSpace(finalMsg.Content) != "" {
				resp.Reply = strings.TrimSpace(finalMsg.Content) + "\n\n(Orquestrador: spinning detectado após " + fmt.Sprintf("%d", step) + " passos — resposta forçada)"
				resp.Steps = step
				resp.ModelUsed = usedModel
				resp.Tracing = append(resp.Tracing, AgentTraceEntry{
					Step: step, Kind: "orchestrator_spinning_break", Agent: "orchestrator",
					Output: truncateStr(finalMsg.Content, 600), Ts: time.Now().Format(time.RFC3339),
				})
				saveAgentRun(req, resp)
				return resp
			}
			resp.Reply = "Orquestrador ficou em spinning (chamando tools sem raciocinar). Tente reformular a tarefa."
			resp.Steps = step
			resp.ModelUsed = usedModel
			resp.Tracing = append(resp.Tracing, AgentTraceEntry{
				Step: step, Kind: "orchestrator_spinning_abort", Agent: "orchestrator",
				Output: "spinning detectado", Ts: time.Now().Format(time.RFC3339),
			})
			saveAgentRun(req, resp)
			return resp
		}

		messages = append(messages, respMsg)
		toolCallsLimit := 5
		if len(respMsg.ToolCalls) > toolCallsLimit {
			log.Printf("[orchestrator] step=%d: modelo retornou %d tool_calls — limitando a %d", step, len(respMsg.ToolCalls), toolCallsLimit)
			respMsg.ToolCalls = respMsg.ToolCalls[:toolCallsLimit]
		}
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
			out, err := runSubagent(ctx, target, subTask, usedModel, req.Mode, req.ConvID, req.TenantID)
			sub := SubagentResult{Agent: target.Name, Output: truncateStr(out, 800), Seconds: time.Since(start).Seconds()}
			if err != nil {
				sub.Error = err.Error()
			}
			resp.Subagents = append(resp.Subagents, sub)
			// FIX: Se o subagente retornou um pending_action (build mode),
			// retorna imediatamente ao usuário para aprovação em vez de
			// continuar o loop do orquestrador.
			if strings.Contains(out, "Confirma?") && req.Mode == "build" {
				resp.Reply = out
				resp.Steps = step
				resp.ModelUsed = usedModel
				resp.Tracing = append(resp.Tracing, AgentTraceEntry{
					Step: step, Kind: "pending_action", Agent: target.Name, Input: subTask,
					Output: truncateStr(out, 600), Ts: time.Now().Format(time.RFC3339),
				})
				// Salvar estado do orquestrador para retomada após aprovação
				plan := parsePlan(req.Task)
				completed := 0
				if req.ResumeFrom != nil {
					plan = req.ResumeFrom.Plan
					completed = req.ResumeFrom.Completed + 1
				}
				saveOrchestratorState(req.ConvID, req.TenantID, OrchestratorState{
					Messages: messages, Step: step, UsedModel: usedModel,
					Plan: plan, Completed: completed,
				})
				saveAgentRun(req, resp)
				return resp
			}
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

			// ── run_engine: gate por modo ──
			if tc.Function.Name == "run_engine" {
				switch req.Mode {
				case "plan":
					result := "Modo planejar: execução via engines não permitida. Somente análise/leitura."
					messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: "run_engine", Content: result})
					resp.Tracing = append(resp.Tracing, AgentTraceEntry{Step: step, Kind: "tool_blocked", Tool: "run_engine", Output: result, Ts: time.Now().Format(time.RFC3339)})
					continue
			case "build":
				desc := describeRunEngineAction(tc.Function.Arguments)
				setPendingAction(req.ConvID, req.TenantID, "", "run_engine", tc.Function.Arguments, desc)
				// Salvar estado do orquestrador para retomada após aprovação
				plan := parsePlan(req.Task)
				completed := 0
				if req.ResumeFrom != nil {
					plan = req.ResumeFrom.Plan
					completed = req.ResumeFrom.Completed + 1
				}
				saveOrchestratorState(req.ConvID, req.TenantID, OrchestratorState{
					Messages: messages, Step: step, UsedModel: usedModel,
					Plan: plan, Completed: completed,
				})
				return OrchestratorResponse{
					Reply: desc + "\n\nConfirma? (responda sim/nao)", Steps: step, ModelUsed: usedModel,
					Tracing: resp.Tracing,
				}
				default:
					if isAutonomousLike(req.Mode) {
						allowed, reason, _ := autonomousAllow(req.ConvID, req.TenantID, "anonymous", "orchestrator", tc.Function.Arguments)
						if !allowed {
							result := "Autônomo: ação bloqueada — " + reason
							messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: "run_engine", Content: result})
							resp.Tracing = append(resp.Tracing, AgentTraceEntry{Step: step, Kind: "tool_blocked", Tool: "run_engine", Output: result, Ts: time.Now().Format(time.RFC3339)})
							continue
						}
					}
				}
				result := runEngineToolExec(ctx, tc.Function.Arguments)
				messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: "run_engine", Content: result})
				resp.Tracing = append(resp.Tracing, AgentTraceEntry{Step: step, Kind: "tool", Tool: "run_engine", Input: tc.Function.Arguments, Output: truncateStr(result, 600), Ts: time.Now().Format(time.RFC3339)})
				continue
			}

			// ── tools mutantes: gate por modo ──
			if isMutantTool(tc.Function.Name) {
				if req.Mode == "plan" {
					desc := describeMutantAction(tc.Function.Name, tc.Function.Arguments)
					result := desc + "\n\n(Modo planejar: nenhuma ação foi executada.)"
					messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
					resp.Tracing = append(resp.Tracing, AgentTraceEntry{Step: step, Kind: "tool_blocked", Tool: tc.Function.Name, Output: result, Ts: time.Now().Format(time.RFC3339)})
					continue
				}
				if isAutonomousLike(req.Mode) {
					allowed, reason, _ := autonomousAllow(req.ConvID, req.TenantID, "", "orchestrator", tc.Function.Arguments)
					if !allowed {
						result := "Autônomo: ação bloqueada — " + reason
						messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
						resp.Tracing = append(resp.Tracing, AgentTraceEntry{Step: step, Kind: "tool_blocked", Tool: tc.Function.Name, Output: result, Ts: time.Now().Format(time.RFC3339)})
						continue
					}
					result := executeTool(ctx, tc.Function.Name, tc.Function.Arguments)
					messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: result})
					resp.Tracing = append(resp.Tracing, AgentTraceEntry{Step: step, Kind: "tool", Tool: tc.Function.Name, Input: tc.Function.Arguments, Output: truncateStr(result, 600), Ts: time.Now().Format(time.RFC3339)})
					continue
				}
				// build (default): pending_action
				desc := describeMutantAction(tc.Function.Name, tc.Function.Arguments)
				setPendingAction(req.ConvID, req.TenantID, "", tc.Function.Name, tc.Function.Arguments, desc)
				// Salvar estado do orquestrador para retomada após aprovação
				plan := parsePlan(req.Task)
				completed := 0
				if req.ResumeFrom != nil {
					plan = req.ResumeFrom.Plan
					completed = req.ResumeFrom.Completed + 1
				}
				saveOrchestratorState(req.ConvID, req.TenantID, OrchestratorState{
					Messages: messages, Step: step, UsedModel: usedModel,
					Plan: plan, Completed: completed,
				})
				return OrchestratorResponse{
					Reply: desc + "\n\nConfirma? (responda sim/nao)", Steps: step, ModelUsed: usedModel,
					Tracing: resp.Tracing,
				}
			}

			// Tool normal (executada direto).
			result := executeTool(ctx, tc.Function.Name, tc.Function.Arguments)
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
func runSubagent(ctx context.Context, a *HOKAgent, task string, model string, mode string, convID string, tenantID string) (string, error) {
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
	// FIX 11/09: fallback free-only (ModelB foi descontinuado) — ver nota acima.
	fallbackChain := []string{ModelC}
	if model != ModelC {
		fallbackChain = append([]string{model}, fallbackChain...)
	}
	emptyContentCount := 0
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
		// Detecção de spinning no subagente (mesmo critério do orquestrador principal)
		if strings.TrimSpace(respMsg.Content) == "" {
			emptyContentCount++
		} else {
			emptyContentCount = 0
		}
		if emptyContentCount >= 2 {
			log.Printf("[subagent %s] spinning detectado no step %d (%d steps com content vazio) — forçando resposta final", a.Name, step, emptyContentCount)
			messages = append(messages, chatMessage{Role: "system", Content: "Detectei que você está chamando ferramentas repetidamente sem gerar texto explicativo. Pare de usar ferramentas agora e dê sua resposta final em texto puro, resumindo o que você fez e o que descobriu."})
			finalMsg, _, finalErr := callGroqAgentLoop(ctx, apiKey, usedModel, messages, nil)
			if finalErr == nil && strings.TrimSpace(finalMsg.Content) != "" {
				return strings.TrimSpace(finalMsg.Content) + fmt.Sprintf("\n\n(Subagente %s: spinning detectado após %d passos — resposta forçada)", a.Name, step), nil
			}
			return "", fmt.Errorf("subagente %s entrou em spinning (chamando tools sem raciocinar)", a.Name)
		}
		messages = append(messages, respMsg)
		// Limitar tool_calls por step (mesmo critério do orquestrador)
		toolCallsLimit := 5
		if len(respMsg.ToolCalls) > toolCallsLimit {
			log.Printf("[subagent %s] step=%d: modelo retornou %d tool_calls — limitando a %d", a.Name, step, len(respMsg.ToolCalls), toolCallsLimit)
			respMsg.ToolCalls = respMsg.ToolCalls[:toolCallsLimit]
		}
		for _, tc := range respMsg.ToolCalls {
			if tc.Function.Name == "run_engine" {
				switch mode {
				case "plan":
					result := "Modo planejar: execução via engines não permitida."
					messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: "run_engine", Content: result})
					continue
				case "build":
					desc := describeRunEngineAction(tc.Function.Arguments)
					setPendingAction(convID, tenantID, "", "run_engine", tc.Function.Arguments, desc)
					return desc + "\n\nConfirma? (responda sim/nao)", nil
				default:
					if isAutonomousLike(mode) {
						allowed, reason, _ := autonomousAllow(convID, tenantID, "anonymous", "orchestrator_subagent", tc.Function.Arguments)
						if !allowed {
							result := "Autônomo: ação bloqueada — " + reason
							messages = append(messages, chatMessage{Role: "tool", ToolCallID: tc.ID, Name: "run_engine", Content: result})
							continue
						}
					}
				}
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
	return fmt.Sprintf(`Voce e o orquestrador do HOK. Tarefa: %s

%s

REGRAS IMPORTANTES:
- Use delegate_to_<nome> para delegar a um subagente.
- Para edicao/criacao de arquivos, use run_engine diretamente (NAO delegue).
- Cada mutacao deve ser feita em separado (UM run_engine por vez).
- Depois que os resultados chegarem, responda em portugues (PT-BR).

ANTES DE PLANEJAR, EXPANDA A TAREFA:
- Identifique cada mutacao individual na tarefa recebida.
- Resolva caminhos relativos/implicitos para caminho absoluto (baseie-se no diretorio de trabalho conhecido ou pergunte se nao houver contexto suficiente).
- Numere a sequencia explicitamente (1, 2, 3...).
- Ao criar cada pending_action, sua mensagem/task interna deve conter o caminho absoluto do arquivo e a posicao dele na sequencia (ex: "arquivo 2 de 3").
- Nunca pare o plano silenciosamente apos uma aprovacao — se ha mais mutacoes pendentes na sequencia original, crie a proxima pending_action imediatamente.`, task, inst)
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
	if a.Name == "Especialista N8N" && a.Knowledge == "" {
		p += n8nContextSuffix()
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

// describeRunEngineAction gera descrição legível para pending_action do run_engine.
func describeRunEngineAction(argsJSON string) string {
	var args struct {
		Engine string `json:"engine"`
		Task   string `json:"task"`
	}
	json.Unmarshal([]byte(argsJSON), &args)
	taskPreview := args.Task
	if len(taskPreview) > 120 {
		taskPreview = taskPreview[:120] + "..."
	}
	desc := fmt.Sprintf("Executar tarefa no engine %s: %s", args.Engine, taskPreview)
	// Extrai informacao de sequencia (ex: "arquivo N de M") da task para a descricao do pending_action
	if idx := strings.Index(args.Task, "arquivo "); idx >= 0 {
		remainder := args.Task[idx:]
		endIdx := strings.Index(remainder, " de ")
		if endIdx >= 0 {
			seqPart := remainder[:endIdx+len(" de ")+2] // até após o número
			desc += " (" + strings.TrimSpace(seqPart) + ")"
		}
	}
	return desc
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

// ─── Orchestrator State (persistência entre aprovações) ─────────────────────

const orchestratorStateTTL = 30 * time.Minute

// saveOrchestratorState — salva o estado do loop do orquestrador para retomada
// após aprovação de pending_action. Chamado quando build mode cria pending_action.
func saveOrchestratorState(convID, tenantID string, state OrchestratorState) {
	if convID == "" {
		return
	}
	msgsJSON, err := json.Marshal(state.Messages)
	if err != nil {
		log.Printf("[orchestrator_state] falha ao serializar messages: %v", err)
		return
	}
	planJSON, _ := json.Marshal(state.Plan)
	expiresAt := time.Now().Add(orchestratorStateTTL).Format(time.RFC3339)
	sqliteExecParams(`INSERT INTO orchestrator_state
		(conv_id, tenant_id, task, messages, step, model, mode, agent_id, max_steps, created_at, expires_at, plan)
		VALUES (?, ?, '', ?, ?, ?, '', '', 15, CURRENT_TIMESTAMP, ?, ?)
		ON CONFLICT(conv_id) DO UPDATE SET
			messages=excluded.messages, step=excluded.step, model=excluded.model,
			mode=excluded.mode, agent_id=excluded.agent_id, max_steps=excluded.max_steps,
			expires_at=excluded.expires_at, created_at=CURRENT_TIMESTAMP, plan=excluded.plan;`,
		convID, tenantID, string(msgsJSON), state.Step, state.UsedModel, expiresAt, string(planJSON))
	log.Printf("[orchestrator_state] salvo conv=%s step=%d model=%s plan=%d", convID, state.Step, state.UsedModel, len(state.Plan))
}

// loadOrchestratorState — carrega estado salvo. Retorna nil se não existe ou expirou.
func loadOrchestratorState(convID, tenantID string) *OrchestratorState {
	if convID == "" {
		return nil
	}
	row := sqliteExecParams(`SELECT messages, step, model, expires_at, plan
		FROM orchestrator_state WHERE conv_id=? AND tenant_id=?;`,
		convID, tenantID)
	if row == "" {
		log.Printf("[orchestrator_state] load: nada encontrado para conv=%s tenant=%s", convID, tenantID)
		return nil
	}
	cols := strings.SplitN(row, "|", 5)
	if len(cols) < 5 {
		log.Printf("[orchestrator_state] load: parse falhou (%d colunas) row=%s", len(cols), truncateStr(row, 200))
		return nil
	}
	// Verificar TTL
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(cols[3]))
	if err != nil {
		log.Printf("[orchestrator_state] load: parse expires_at falhou: %v (raw=%q)", err, cols[3])
		return nil
	}
	if time.Now().After(expiresAt) {
		log.Printf("[orchestrator_state] load: expirado conv=%s — limpando", convID)
		clearOrchestratorState(convID, tenantID)
		return nil
	}
	var msgs []chatMessage
	if err := json.Unmarshal([]byte(cols[0]), &msgs); err != nil {
		log.Printf("[orchestrator_state] load: deserialize messages falhou conv=%s: %v", convID, err)
		clearOrchestratorState(convID, tenantID)
		return nil
	}
	step := atoiDefault(cols[1], 0)
	model := strings.TrimSpace(cols[2])
	if step <= 0 || model == "" {
		log.Printf("[orchestrator_state] load: dados invalidos step=%d model=%q", step, model)
		return nil
	}
	log.Printf("[orchestrator_state] load: OK conv=%s step=%d model=%s (%d msgs)", convID, step, model, len(msgs))
	var plan []string
	_ = json.Unmarshal([]byte(cols[4]), &plan)
	return &OrchestratorState{
		Messages:  msgs,
		Step:      step,
		UsedModel: model,
		Plan:      plan,
	}
}

// clearOrchestratorState — remove estado salvo (após conclusão ou falha).
func clearOrchestratorState(convID, tenantID string) {
	if convID == "" {
		return
	}
	sqliteExecParams(`DELETE FROM orchestrator_state WHERE conv_id=? AND tenant_id=?;`,
		convID, tenantID)
}

// buildResumeMessages — reconstrói o array de messages a partir do estado salvo.
// Injeta o resultado da aprovação como tool result no step anterior.
func buildResumeMessages(state *OrchestratorState, approvalResult string) []chatMessage {
	msgs := make([]chatMessage, len(state.Messages))
	copy(msgs, state.Messages)
	// Encontrar a última tool call sem resultado e injetar o resultado
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" && len(msgs[i].ToolCalls) > 0 {
			// Esta mensagem tem tool calls — injetar resultado como tool result
			for _, tc := range msgs[i].ToolCalls {
				msgs = append(msgs, chatMessage{
					Role:       "tool",
					ToolCallID: tc.ID,
					Name:       tc.Function.Name,
					Content:    approvalResult,
				})
			}
			break
		}
	}
	return msgs
}