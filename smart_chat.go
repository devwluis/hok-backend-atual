package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path"
	"strings"
	"time"
)

type SmartChatResp struct {
	Reply         string         `json:"reply"`
	Mode          string         `json:"mode"`
	EngineUsed    string         `json:"engine_used,omitempty"`
	SkillUsed     string         `json:"skill_used,omitempty"`
	ModelUsed     string         `json:"model_used,omitempty"`
	LatencyMs     int64          `json:"latency_ms"`
	PendingAction *PendingAction `json:"pendingAction,omitempty"`
	JobID         string         `json:"job_id,omitempty"`
}

func handleSmartChat(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var req ClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	start := time.Now()
	resp := SmartChatResp{}
	convId := convIdFromRequest(r)
	tenantID := tenantIdFromRequest(r)
	userID := userIdFromRequest(r)
	// ORPHAN KILL (29/08 21:xx): se o cliente HTTP desconectar (troca de aba,
	// reload, rede) enquanto o handler ainda está processando, ctx.Done() dispara
	// e os jobs registrados via autonomousJobRegister são cancelados. Evita
	// o job órfão que continua rodando no backend sem ninguém ler a resposta.
	autonomousJobWatchOrphan(r.Context(), convId)
	// MODO AUTÔNOMO (29/08) — decisão 1: o session_mode (setado pelos 3
	// botões via POST /session/mode) é a fonte do modo quando o request não
	// traz mode. O request continua vencendo quando presente (compat com o
	// frontend atual, que ainda envia mode no body).
	if req.Mode == "" {
		if m, _, _, _, ok := sessionModeLoad(convId, tenantID, userID); ok && m != "" {
			req.Mode = m
		}
	}
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = strings.TrimSpace(req.Prompt)
	}
	// ROLLBACK VIA CHAT (29/08, decisão 3): "volte pro checkpoint" restaura
	// o checkpoint da conversa (recovery.sh standalone — o serviço reinicia).
	if strings.Contains(strings.ToLower(msg), "volte pro checkpoint") {
		checkpointID := sessionModeCheckpoint(convId, tenantID, userID)
		if checkpointID == "" || !checkpointExists(checkpointID) {
			resp.Reply = "Nenhum checkpoint encontrado para esta conversa. Ative o modo autônomo total (POST /session/mode) para criar um."
			resp.Mode = "recovery_none"
		} else if err := triggerRecovery(checkpointID); err != nil {
			resp.Reply = "Falha ao disparar o rollback: " + err.Error()
			resp.Mode = "recovery_error"
		} else {
			resp.Reply = "Rollback do checkpoint " + checkpointID + " iniciado — o serviço será reiniciado (recovery.sh standalone)."
			resp.Mode = "recovery_started"
		}
		resp.LatencyMs = time.Since(start).Milliseconds()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}
	if pa := getPendingAction(convId, tenantID, userID); pa != nil && msg != "" {
if isApprovalText(msg) {
		log.Printf("[AUDIT] Aprovacao via chat conv=%s tenant=%s msg=%q actionID=%s", convId, tenantID, msg, pa.ID)
		resp.Reply = resolvePendingAction(r.Context(), convId, tenantID, userID, true)
		resp.Mode = "action_approved"
		resp.LatencyMs = time.Since(start).Milliseconds()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}
	if isRejectionText(msg) {
		log.Printf("[AUDIT] Rejeicao via chat conv=%s tenant=%s msg=%q actionID=%s", convId, tenantID, msg, pa.ID)
		resp.Reply = resolvePendingAction(r.Context(), convId, tenantID, userID, false)
			resp.Mode = "action_rejected"
			resp.LatencyMs = time.Since(start).Milliseconds()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
	}

	switch {
	case req.AudioB64 != "" && req.ImageB64 != "":
		transcript, asrErr := callGroqASR(req.AudioB64, req.AudioMime)
		if asrErr != nil {
			resp.Reply = "Erro ASR: " + asrErr.Error()
			resp.Mode = "error"
			break
		}
		prompt := transcript
		if msg != "" {
			prompt = msg + " " + transcript
		}
		prompt += " Responda em português do Brasil (PT-BR)."
		mimeType := req.ImageMime
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		reply, vErr := callDeepSeekVision(req.ImageB64, mimeType, prompt)
		if vErr != nil {
			log.Printf("DeepSeek VL audio+img falhou: %v", vErr)
			reply, vErr = callGeminiVision(req.GeminiKey, req.ImageB64, mimeType, prompt)
			if vErr != nil {
				resp.Reply = "Erro visao+audio: " + vErr.Error()
				resp.Mode = "error"
				break
			}
			resp.Mode = "voice_vision_gemini"
		} else {
			resp.Mode = "voice_vision_deepseek_vl"
		}
		resp.Reply = "[ASR: " + transcript + "] " + reply
	case req.AudioB64 != "":
		transcript, asrErr := callGroqASR(req.AudioB64, req.AudioMime)
		if asrErr != nil {
			resp.Reply = "Erro ASR: " + asrErr.Error()
			resp.Mode = "error"
			break
		}
		reply, mode, skill, engine, modelUsed := runSmartText(r.Context(), transcript, req, convId, tenantID, userID)
		resp.Reply = "[ASR: " + transcript + "] " + reply
		resp.Mode = "voice_" + mode
		resp.SkillUsed = skill
		resp.EngineUsed = engine
		resp.ModelUsed = modelUsed
	case req.ImageB64 != "":
		prompt := msg
		if prompt == "" {
			prompt = "Descreva esta imagem detalhadamente."
		}
		prompt += " Responda em português do Brasil (PT-BR)."
		mimeType := req.ImageMime
		if mimeType == "" {
			mimeType = "image/jpeg"
		}
		reply, vErr := callDeepSeekVision(req.ImageB64, mimeType, prompt)
		if vErr != nil {
			log.Printf("DeepSeek VL falhou: %v", vErr)
			reply, vErr = callGeminiVision(req.GeminiKey, req.ImageB64, mimeType, prompt)
			if vErr != nil {
				log.Printf("Gemini falhou: %v", vErr)
				// FIX 03/09: fallbacks do OpenRouter eram modelos PAGOS
				// (qwen2.5-vl-72b-instruct, claude-haiku-4.5) — gastavam crédito
				// sempre que o Gemini caía em rate-limit. Trocar para ModelB
				// (minimax-m3:free, pricing 0/0) com visão confirmada.
				reply, vErr = callORVision(req.OrKey, ModelB, req.ImageB64, mimeType, prompt)
				if vErr != nil {
					log.Printf("Minimax M3 VL free falhou: %v", vErr)
					reply, vErr = callGeminiVision(req.GeminiKey, req.ImageB64, mimeType, prompt)
					if vErr != nil {
						reply, vErr = callOpenAIVision(req.OpenAIKey, req.ImageB64, mimeType, prompt)
						if vErr != nil {
							resp.Reply = "Erro visao: " + vErr.Error()
							resp.Mode = "error"
							break
						}
						resp.Mode = "vision_gpt4o_mini"
					} else {
						resp.Mode = "vision_gemini"
					}
				} else {
					resp.Mode = "vision_minimax_m3_free"
				}
			} else {
				resp.Mode = "vision_gemini"
			}
		} else {
			resp.Mode = "vision_deepseek_vl"
		}
		resp.Reply = reply
	default:
		if isSelfModCommand(msg) {
			extracted := extractBashCommand(msg)
			if isReadOnlySafeCommand(extracted) {
				autoID := fmt.Sprintf("auto_ro_%s_%d", convId, time.Now().UnixNano())
				output, err := ExecuteApprovedCommand(autoID, extracted)
				if err != nil {
					resp.Reply = fmt.Sprintf("❌ Erro: %v\nOutput: %s", err, output)
				} else {
					resp.Reply = fmt.Sprintf("✅ (auto-aprovado, leitura) \n\nOutput:\n%s", output)
				}
				resp.Mode = "bash_exec_auto"
				resp.EngineUsed = "bash_exec_auto"
				goto finalizeResponse
			}
			action, err := registerFsExecPendingAction(convId, tenantID, userID, extracted, true)
			if err == nil {
				resp.Reply = "🔒 Comando de automodificacao detectado. Aguardando aprovacao."
				resp.Mode = "bash_exec"
				resp.EngineUsed = "bash_exec"
				resp.PendingAction = action
				goto finalizeResponse
			}
			log.Printf("⚠️  falha ao criar pending action: %v", err)
		}
		if msg == "" {
			// message required removido — msg já tem fallback
			return
		}
		if req.Async {
			// FASE MAIOR (27/08): processamento em background com contexto
			// próprio — sobrevive à desconexão da aba/app. O frontend faz
			// polling em GET /chat/job e retoma ao voltar.
			jobID := startChatJob(convId, tenantID, userID, func(ctx context.Context) (string, string, string, string, string) {
				r, m, s, e, mu := runSmartText(ctx, msg, req, convId, tenantID, userID)
				persistChatJobMessages(convId, msg, r)
				return r, m, s, e, mu
			})
			resp.JobID = jobID
			resp.Mode = "job_running"
			goto finalizeResponse
		}
		reply, mode, skill, engine, modelUsed := runSmartText(r.Context(), msg, req, convId, tenantID, userID)
		resp.Reply = reply
		resp.Mode = mode
		resp.SkillUsed = skill
		resp.EngineUsed = engine
		resp.ModelUsed = modelUsed
	}

finalizeResponse:
	resp.PendingAction = getPendingAction(convId, tenantID, userID)
	resp.LatencyMs = time.Since(start).Milliseconds()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func containsN8nKeyword(msg string) bool {
	lower := strings.ToLower(msg)
	keywords := []string{"n8n", "workflow", "workflows", "fluxo de trabalho", "fluxos de trabalho", "diagnostique", "diagnostico", "diagnóstico", "ambiente", ".env", "credencial", "credenciais", "backup", "node", "nodes"}
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// containsTerminalKeyword detecta pedido de execução de comando no terminal
// (gatilho explícito "/terminal "/" /term " ou heurística de linguagem natural).
// Standalone por enquanto: a ativação no fluxo do chat fica para depois
// (integração chat→terminal pendente de decisão de arquitetura).
func containsTerminalKeyword(msg string) bool {
	lower := strings.ToLower(msg)
	if strings.HasPrefix(lower, "/terminal ") || strings.HasPrefix(lower, "/term ") {
		return true
	}
	nl := []string{
		"qual o status",
		"roda ", "rodar ", "executa", "executar",
		"comando", "shell",
	}
	for _, h := range nl {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return false
}

// classifyEngine decide qual engine vai processar a mensagem.
// Mantem a MESMA ordem de prioridade que ja existe em runSmartText:
// n8n (por keyword) > skill router > claude_code > hermes > chat padrao.
// promptNeedsApproval decide se um prompt destinado ao Claude Code precisa
// de aprovacao manual. So exige aprovacao se houver sinal real de
// escrita/execucao (edicao de arquivo, git, comandos destrutivos, etc).
// Mensagens triviais/conversacionais ("Oi", "tudo bem?", perguntas simples)
// executam direto, sem gate.
var destructiveSignals = []string{
	"delete", "deletar", "apague", "apagar", "remova", "remover",
	"rm ", "sudo", "chmod", "chown", "systemctl", "kill ", "dd ", "mkfs",
	"git push", ">>", ">",
}

var writeSignals = []string{
	"edite", "edita", "editar", "modifique", "modificar",
	"escreva", "escrever", "crie o arquivo", "criar arquivo",
	"git commit", "git add", "sed ", "mv ", "cp ",
	"corrija", "corrigir", "conserte", "refatore", "refatorar",
	"implemente", "implementar", "rode o comando", "roda o comando",
	"execute o comando", "instale", "instalar",
}

func promptIsDestructive(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, w := range destructiveSignals {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

func promptNeedsApproval(prompt string) bool {
	return promptIsDestructive(prompt)
}

// needsRealTools decide se uma mensagem de fato exige o engine claude_code
// (ferramentas/ações reais: arquivos, terminal, deploy, git, n8n, logs...).
// Fix 16/08 (item C): ForceClaudeCode só é respeitado quando a pergunta
// precisa de ferramentas — perguntas triviais/conversacionais ("Hok?", "Oi",
// "tudo bem?", perguntas gerais) voltam pro engine chat normal, evitando
// uso desnecessário do claude_code (que regurgitava o system prompt do SDK).
func needsRealTools(msg string) bool {
	lower := strings.ToLower(msg)
	toolKeywords := []string{
		// arquivos
		"arquivo", "edite", "edita", "editar", "modifique", "modificar",
		"crie", "criar", "remova", "remover", "apague", "apagar",
		"renomeie", "renomear", "mova", "mover", "copie", "copiar",
		"conteudo do arquivo", "conteúdo do arquivo", "leia o arquivo",
		// terminal/comandos
		"rode o comando", "roda o comando", "execute", "terminal", "bash",
		"comando", "shell", "script", "instale", "instalar", "executar",
		// deploy/serviço
		"deploy", "deployar", "build", "compilar", "rebuild", "redeploy",
		"nginx", "systemctl", "servidor", "serviço", "servico",
		// git
		"git ", "commit", "push", "pull", "branch", "merge",
		// n8n/automação
		"workflow", "n8n", "fluxo de trabalho",
		// testes/logs/diagnóstico
		"teste", "testar", "testes", "log", "logs", "erro", "bug", "falha",
		"corrija", "corrigir", "conserte", "refatore", "refatorar",
		"implemente", "implementar", "analise o reposit",
		// banco de dados
		"banco de dados", "sqlite", "query", "migração", "migracao",
	}
	for _, k := range toolKeywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func classifyEngine(msg string, req ClientRequest) string {
	if containsSecurityKeyword(msg) {
		return "security"
	}
	if containsN8nKeyword(msg) {
		return "n8n_agent"
	}
	if req.ForceOrchestrator {
		return "orchestrator"
	}
	if (req.ForceClaudeCode && needsRealTools(msg)) || isClaudeCodeTask(msg) {
		return "claude_code"
	}
	if req.ForceOpenCode || isOpenCodeTask(msg) {
		return "opencode"
	}
	if req.ForceHermes || isComplexTask(msg) {
		return "hermes"
	}
	return "chat"
}

// ─────────────────────────────────────────────────────────────────────────
// REFACTOR 22/08: runSmartText reestruturada em cascata de helpers-guard.
// Cada helper retorna *smartTextResult quando o branch SE APLICA e resolve a
// resposta; nil significa "não aplicado / falhou — tenta o próximo engine na
// MESMA ordem de prioridade de antes". Nenhuma lógica, log, string ou ordem
// de prioridade foi alterada — apenas estrutura (ponto único de saída).
// Novos engines no futuro: adicionar um tryXxx() na posição de prioridade
// desejada dentro de runSmartTextCascade.
// ─────────────────────────────────────────────────────────────────────────

// smartTextResult é o resultado interno do roteamento do chat. Os campos
// correspondem 1:1 à tupla pública (reply, mode, skill, engine, modelUsed).
type smartTextResult struct {
	reply     string
	mode      string
	skill     string
	engine    string
	modelUsed string
}

// runSmartText mantém a assinatura pública original — chamadores não mudam.
func runSmartText(ctx context.Context, msg string, req ClientRequest, convId string, tenantID string, userID string) (reply, mode, skill, engine, modelUsed string) {
	r := runSmartTextCascade(ctx, msg, req, convId, tenantID, userID)
	return r.reply, r.mode, r.skill, r.engine, r.modelUsed
}

// runSmartTextCascade executa a cadeia de prioridade e tem UM único return.
func runSmartTextCascade(ctx context.Context, msg string, req ClientRequest, convId string, tenantID string, userID string) smartTextResult {
	var res *smartTextResult
	// agentFailure guarda o motivo da falha do agente n8n para que o
	// fallback nunca silencie a falha (bug: criação de workflow falhava
	// e o usuário recebia resposta genérica sem saber que nada foi feito).
	agentFailure := ""

	if res == nil {
		res = trySecurity(msg)
	}
	if res == nil {
		// ORQUESTRADOR (03/09): quando o usuário força o engine no seletor,
		// tem prioridade imediata (acima do n8n_agent e demais). Só cai para
		// a cascata se não estiver forçado.
		res = tryOrchestrator(ctx, msg, req, convId, tenantID)
	}
	if res == nil {
		res, agentFailure = tryN8nAgent(ctx, msg, req, convId, tenantID)
	}
	if res == nil {
		// FASE 3 (27/08): opencode serve como canal principal do Chat Web —
		// substitui a ponte PTY/tmux (bug estrutural OpenTUI). Se o servidor
		// estiver fora do ar ou a mensagem não se aplicar, retorna nil e a
		// cascata segue para o tryTerminalExec legado (comportamento antigo).
		res = tryOpenCodeServe(msg, req, convId, tenantID, userID)
	}
	if res == nil {
		// FIX 22/08: execucao no PTY via chat admin (/terminal, /term ou
		// verbo+cmd em linguagem natural). ADITIVO — nenhum branch existente
		// alterado. Posicao: apos n8n, ANTES do skill router, para comando
		// explicito nao ser sequestrado pelo fuzzy match de skills.
		res = tryTerminalExec(msg, userID, req.TerminalSession)
	}
	if res == nil {
		res = trySkillRouter(msg, convId, tenantID, userID)
	}
	if res == nil {
		res = tryClaudeCode(ctx, msg, req, convId, tenantID, userID)
	}
	if res == nil {
		res = tryOpenCode(ctx, msg, req, convId, tenantID, userID)
	}
	if res == nil {
		res = tryHermes(msg, req, convId, tenantID, userID)
	}
	if res == nil {
		res = buildFallbackChat(msg, req, agentFailure)
	}
	return *res
}

// trySecurity (#1): DeepHat quando há keyword de segurança.
// Falha do DeepHat só loga — cai pro próximo engine como antes.
func trySecurity(msg string) *smartTextResult {
	if !containsSecurityKeyword(msg) {
		return nil
	}
	msgs := []Message{{Role: "user", Content: msg}}
	out, err := callDeepHat("", msgs)
	if err != nil {
		log.Printf("⚠️ DeepHat falhou: %v — fallback LLM", err)
		return nil
	}
	return &smartTextResult{reply: out, mode: "deephat_security", skill: "DeepHat-V1-7B", engine: "security"}
}

// tryN8nAgent (#2/#3/#15): agent loop com 2 tentativas quando há keyword n8n.
// Retorna também agentFailure ("" se não se aplicou ou se teve sucesso) para
// o fallback final montar o aviso de automação não concluída.
func tryN8nAgent(ctx context.Context, msg string, req ClientRequest, convId string, tenantID string) (*smartTextResult, string) {
	if !containsN8nKeyword(msg) {
		return nil, ""
	}
	out, err := RunAgentLoop(ctx, msg, req.Mode, req.History, convId, tenantID)
	if err == nil {
		return &smartTextResult{reply: out, mode: "n8n_agent_loop", engine: "n8n_agent"}, ""
	}
	log.Printf("⚠️ n8n agent loop falhou (1a tentativa): %v — retentando uma vez", err)
	out, err = RunAgentLoop(ctx, msg, req.Mode, req.History, convId, tenantID)
	if err == nil {
		return &smartTextResult{reply: out, mode: "n8n_agent_loop", engine: "n8n_agent"}, ""
	}
	log.Printf("⚠️ n8n agent loop falhou (2a tentativa): %v — fallback normal com aviso", err)
	return nil, err.Error()
}

// trySkillRouter (#4).
func trySkillRouter(msg string, convId string, tenantID string, userID string) *smartTextResult {
	if output, _, found := trySkillForMessage(msg, convId, tenantID, userID); found {
		return &smartTextResult{reply: output, mode: "skill", skill: msg, engine: "skill"}
	}
	return nil
}

// tryOrchestrator — engine dedicado (03/09): roda o orquestrador que delega
// a subagentes configurados e/ou a claude/opencode/hermes via tool run_engine.
func tryOrchestrator(ctx context.Context, msg string, req ClientRequest, convId string, tenantID string) *smartTextResult {
	if !req.ForceOrchestrator {
		return nil
	}
	resp := RunOrchestrator(ctx, OrchestratorRequest{
		Task:     msg,
		Model:    req.Model,
		MaxSteps: 6,
		ConvID:   convId,
		TenantID: tenantID,
	})
	return &smartTextResult{
		reply:     resp.Reply,
		mode:      "orchestrator",
		skill:     "",
		engine:    "orchestrator",
		modelUsed: resp.ModelUsed,
	}
}

// tryClaudeCode (#5–#8). Preserva as quedas originais:
//   - plan falhou → LOG e segue pro próximo engine (opencode), SEM pending;
//   - direct falhou com erro comum → cai no pending (gate de aprovação);
//   - direct falhou com leak → blocked, sem tocar pending action.
func tryClaudeCode(ctx context.Context, msg string, req ClientRequest, convId string, tenantID string, userID string) *smartTextResult {
	if !((req.ForceClaudeCode && needsRealTools(msg)) || isClaudeCodeTask(msg)) {
		return nil
	}
	prompt := buildClaudeCodePrompt(msg, req)
	if req.Mode == "plan" {
		// GATE PLAN (28/08): roda o CLI claude com --permission-mode plan
		// (modo nativo — descreve sem executar). Antes, o plan era decorativo
		// e o claude EXECUTAVA as ferramentas mesmo assim.
		preview, err := callClaudeCodePlan(ctx, prompt)
		if err == nil {
			return &smartTextResult{reply: preview + "\n\n(Modo planejar: nenhuma acao foi executada com permissoes elevadas.)", mode: "claude_code_plan", engine: "claude_code"}
		}
		log.Printf("⚠️ Claude Code (plan) falhou: %v — fallback normal", err)
		return nil
	}
	if isAutonomousLike(req.Mode) {
		// GATE AUTÔNOMO (28/08) + AUTÔNOMO TOTAL (29/08): roda sem
		// aprovação por ação — mas a blocklist Hokma barra destrutivos com
		// aviso direto (decisão 4, sem pendência automática), o budget e o
		// circuit breaker param o fluxo, e cada chamada é auditada. O TOTAL
		// cria snapshot automático antes da 1ª execução e tem budget 50.
		replyMode := "claude_code_autonomous"
		if req.Mode == "autonomous_total" {
			autonomousTotalEnsureSnapshot(convId, tenantID, userID)
			replyMode = "claude_code_autonomous_total"
		}
		// ALLOWLIST DO MODO AUTÔNOMO (31/08): proibido → bloqueado; fora da
		// allowlist → aprovação humana; só allowlist → executa sem aprovação.
		dec, gateReason := autonomousGate(prompt)
		switch dec {
		case autonomousForbidden:
			autonomousAuditLog(convId, tenantID, userID, "claude_code", prompt, "blocked", autonomousBudgetLeft(convId, tenantID, userID))
			return &smartTextResult{reply: "⛔ Modo autônomo: " + gateReason + ". Troque para o modo build se quiser executar isso manualmente.", mode: replyMode + "_blocked", engine: "claude_code"}
		case autonomousNeedsApproval:
			autonomousAuditLog(convId, tenantID, userID, "claude_code", prompt, "pending_allowlist", autonomousBudgetLeft(convId, tenantID, userID))
			argsJSON, _ := json.Marshal(map[string]string{"prompt": prompt})
			desc := describeClaudeCodeAction(prompt)
			setPendingAction(convId, tenantID, userID, "claude_code", string(argsJSON), desc)
			return &smartTextResult{reply: desc + "\n\n(Modo autônomo: ação fora da allowlist) Confirma? (responda sim/nao)", mode: replyMode + "_pending", engine: "claude_code"}
		}
		allowed, reason, budgetLeft := autonomousAllow(convId, tenantID, userID, "claude_code", prompt)
		if !allowed {
			return &smartTextResult{reply: "⛔ Modo autônomo: " + reason, mode: replyMode + "_blocked", engine: "claude_code"}
		}
		out, err := callClaudeCodeAutonomous(ctx, prompt)
		autonomousCBFor(convId).recordResult(err)
		if err != nil {
			// TRAVA DE SEGURANÇA (29/08): modelo expirado/pago/inválido →
			// mensagem clara, sem fallback/troca automática.
			if r := modelBlockIfExpired(err, "claude_code"); r != nil {
				return r
			}
			return &smartTextResult{reply: "Modo autônomo: a execução falhou — " + err.Error(), mode: replyMode + "_error", engine: "claude_code"}
		}
		return &smartTextResult{reply: out + fmt.Sprintf("\n\n(Modo autônomo — budget restante: %d)", budgetLeft), mode: replyMode, engine: "claude_code"}
	}
	if !promptNeedsApproval(prompt) || promptContainsOnlyReadOnlyCommands(prompt) {
		out, err := callClaudeCode(ctx, prompt)
		if err == nil {
			return &smartTextResult{reply: out, mode: "claude_code_direct", engine: "claude_code"}
		}
		if errors.Is(err, errSystemPromptLeak) {
			return &smartTextResult{reply: "Hmm, não consegui processar isso com segurança agora. Tente reformular o pedido ou volte a perguntar de outra forma.", mode: "claude_code_blocked", engine: "claude_code"}
		}
		// TRAVA DE SEGURANÇA (29/08): modelo expirado/pago/inválido →
		// mensagem clara, sem pending nem fallback (a seleção do usuário
		// permanece registrada).
		if r := modelBlockIfExpired(err, "claude_code"); r != nil {
			return r
		}
		log.Printf("⚠️ Claude Code (direto, trivial) falhou: %v — fallback aprovacao", err)
	}
	argsJSON, _ := json.Marshal(map[string]string{"prompt": prompt})
	desc := describeClaudeCodeAction(prompt)
	setPendingAction(convId, tenantID, userID, "claude_code", string(argsJSON), desc)
	return &smartTextResult{reply: desc + "\n\nConfirma? (responda sim/nao)", mode: "claude_code_pending", engine: "claude_code"}
}

// tryOpenCode (#9–#12): espelho do claude_code; bloqueio detectado por
// substring "blocked" no erro (diferente do errors.Is do claude).
func tryOpenCode(ctx context.Context, msg string, req ClientRequest, convId string, tenantID string, userID string) *smartTextResult {
	if !(req.ForceOpenCode || isOpenCodeTask(msg)) {
		return nil
	}
	prompt := buildOpenCodePrompt(msg, req)
	if req.Mode == "plan" {
		// GATE PLAN (28/08) + TRAVA REAL (31/08): roda o CLI opencode com o
		// agente "plan" (permissões deny) + OPENCODE_PERMISSION=deny no motor
		// (env aplicada por último na carga de config) + verificador
		// pós-execução no stream. Se a trava detectar execução de tool de
		// escrita/execução, fail-closed: resposta descartada, nada reconhecido.
		preview, err := callOpenCodePlan(ctx, prompt, convId, tenantID, userID)
		if err == nil {
			return &smartTextResult{reply: preview + "\n\n(Modo planejar: nenhuma acao foi executada — tools de escrita/execução estão bloqueadas por trava real do motor.)", mode: "opencode_plan", engine: "opencode"}
		}
		if errors.Is(err, errOpenCodePlanLock) {
			return &smartTextResult{reply: "⛔ Modo plan: a trava de segurança detectou tentativa de executar ferramenta de escrita/execução e bloqueou a resposta. Nenhum arquivo foi criado ou alterado.", mode: "opencode_plan_blocked", engine: "opencode"}
		}
		log.Printf("⚠️ OpenCode (plan) falhou: %v — fallback normal", err)
		return nil
	}
	if isAutonomousLike(req.Mode) {
		// GATE AUTÔNOMO (28/08) + TOTAL (29/08): espelho do claude_code —
		// blocklist barra com aviso direto (decisão 4), budget + circuit
		// breaker param, cada chamada é auditada; TOTAL cria snapshot e tem
		// budget 50. Roda com --auto (sem pendência).
		replyMode := "opencode_autonomous"
		if req.Mode == "autonomous_total" {
			autonomousTotalEnsureSnapshot(convId, tenantID, userID)
			replyMode = "opencode_autonomous_total"
		}
		// ALLOWLIST DO MODO AUTÔNOMO (31/08): proibido → bloqueado; fora da
		// allowlist → aprovação humana; só allowlist → executa sem aprovação.
		dec, gateReason := autonomousGate(prompt)
		switch dec {
		case autonomousForbidden:
			autonomousAuditLog(convId, tenantID, userID, "opencode", prompt, "blocked", autonomousBudgetLeft(convId, tenantID, userID))
			return &smartTextResult{reply: "⛔ Modo autônomo: " + gateReason + ". Troque para o modo build se quiser executar isso manualmente.", mode: replyMode + "_blocked", engine: "opencode"}
		case autonomousNeedsApproval:
			autonomousAuditLog(convId, tenantID, userID, "opencode", prompt, "pending_allowlist", autonomousBudgetLeft(convId, tenantID, userID))
			argsJSON, _ := json.Marshal(map[string]string{"prompt": prompt})
			desc := describeOpenCodeAction(prompt)
			setPendingAction(convId, tenantID, userID, "opencode", string(argsJSON), desc)
			return &smartTextResult{reply: desc + "\n\n(Modo autônomo: ação fora da allowlist) Confirma? (responda sim/nao)", mode: replyMode + "_pending", engine: "opencode"}
		}
		allowed, reason, budgetLeft := autonomousAllow(convId, tenantID, userID, "opencode", prompt)
		if !allowed {
			return &smartTextResult{reply: "⛔ Modo autônomo: " + reason, mode: replyMode + "_blocked", engine: "opencode"}
		}
		out, err := callOpenCodeAutonomous(ctx, prompt, convId, tenantID, userID)
		autonomousCBFor(convId).recordResult(err)
		if err != nil {
			// TRAVA DE SEGURANÇA (29/08): modelo expirado/pago/inválido →
			// mensagem clara, sem fallback/troca automática.
			if r := modelBlockIfExpired(err, "opencode"); r != nil {
				return r
			}
			return &smartTextResult{reply: "Modo autônomo: a execução falhou — " + err.Error(), mode: replyMode + "_error", engine: "opencode"}
		}
		return &smartTextResult{reply: out + fmt.Sprintf("\n\n(Modo autônomo — budget restante: %d)", budgetLeft), mode: replyMode, engine: "opencode"}
	}
	if !promptNeedsApproval(prompt) || promptContainsOnlyReadOnlyCommands(prompt) {
		out, err := callOpenCode(ctx, prompt, convId, tenantID, userID)
		if err == nil {
			return &smartTextResult{reply: out, mode: "opencode_direct", engine: "opencode"}
		}
		if strings.Contains(strings.ToLower(err.Error()), "blocked") {
			return &smartTextResult{reply: "Hmm, não consegui processar isso com segurança agora. Tente reformular o pedido.", mode: "opencode_blocked", engine: "opencode"}
		}
		// TRAVA DE SEGURANÇA (29/08): modelo expirado/pago/inválido →
		// mensagem clara, sem pending nem fallback.
		if r := modelBlockIfExpired(err, "opencode"); r != nil {
			return r
		}
		log.Printf("⚠️ OpenCode (direto, trivial) falhou: %v — fallback aprovacao", err)
	}
	argsJSON, _ := json.Marshal(map[string]string{"prompt": prompt})
	desc := describeOpenCodeAction(prompt)
	setPendingAction(convId, tenantID, userID, "opencode", string(argsJSON), desc)
	return &smartTextResult{reply: desc + "\n\nConfirma? (responda sim/nao)", mode: "opencode_pending", engine: "opencode"}
}

// tryHermes (#13): erro só loga e cai pro chat normal, como antes.
func tryHermes(msg string, req ClientRequest, convId, tenantID, userID string) *smartTextResult {
	if !(req.ForceHermes || isComplexTask(msg)) {
		return nil
	}
	prompt := buildHermesPrompt(msg, req)
	var out string
	var err error
	if isAutonomousLike(req.Mode) {
		// GATE AUTÔNOMO (28/08) + TOTAL (29/08): blocklist barra com aviso
		// direto (decisão 4), budget + circuit breaker param o fluxo, cada
		// chamada é auditada. Execução: container efêmero com o volume REAL
		// rw (decisão 3) — o hermes executa de verdade, dentro do limite do
		// budget e sem acesso ao host/docker.sock. TOTAL: snapshot
		// automático antes da 1ª execução e budget 50.
		replyMode := "hermes_autonomous"
		if req.Mode == "autonomous_total" {
			autonomousTotalEnsureSnapshot(convId, tenantID, userID)
			replyMode = "hermes_autonomous_total"
		}
		// ALLOWLIST DO MODO AUTÔNOMO (31/08): hermes NÃO tem fluxo de pendência
		// próprio — fora da allowlist = bloqueado fail-closed (nunca executa
		// direto com --yolo fora da allowlist).
		dec, gateReason := autonomousGate(prompt)
		switch dec {
		case autonomousForbidden, autonomousNeedsApproval:
			autonomousAuditLog(convId, tenantID, userID, "hermes", prompt, "blocked", autonomousBudgetLeft(convId, tenantID, userID))
			return &smartTextResult{reply: "⛔ Modo autônomo: " + gateReason + ". Troque para o modo build se quiser executar isso manualmente.", mode: replyMode + "_blocked", engine: "hermes"}
		}
		allowed, reason, budgetLeft := autonomousAllow(convId, tenantID, userID, "hermes", prompt)
		if !allowed {
			return &smartTextResult{reply: "⛔ Modo autônomo: " + reason, mode: replyMode + "_blocked", engine: "hermes"}
		}
		out, err = callHermesWithMode(getActiveModel(), prompt, "autonomous")
		autonomousCBFor(convId).recordResult(err)
		if err != nil {
			// TRAVA DE SEGURANÇA (29/08): modelo expirado/pago/inválido ou
			// não suportado pelo Hermes → mensagem clara, sem fallback e sem
			// troca automática.
			if r := hermesModelResult(err); r != nil {
				return r
			}
			log.Printf("❌ Hermes (autônomo) erro: %v", err)
			return &smartTextResult{reply: "Modo autônomo: a execução falhou — " + err.Error(), mode: replyMode + "_error", engine: "hermes"}
		}
		return &smartTextResult{reply: hermesVerifyOutput(out) + fmt.Sprintf("\n\n(Modo autônomo — budget restante: %d)", budgetLeft), mode: replyMode, engine: "hermes"}
	}
	if req.Mode == "plan" {
		// GATE PLAN (28/08): hermes SEM --yolo + prompt de plano (descrever
		// apenas). Antes rodava sempre com --yolo (auto-aprova) sem check.
		out, err = callHermesWithMode(getActiveModel(), prompt, "plan")
	} else {
		out, err = callHermes(prompt)
	}
	if err != nil {
		// TRAVA DE SEGURANÇA (29/08): modelo expirado/pago/inválido ou não
		// suportado pelo Hermes → mensagem clara, sem cascata/fallback.
		if r := hermesModelResult(err); r != nil {
			return r
		}
		log.Printf("❌ Hermes erro: %v", err)
		return nil
	}
	// Verificação pós-execução (28/08): o hermes pode AFIRMAR ter criado/
	// alterado arquivos sem ter feito nada (alucinação — caso observado).
	// Anexa aviso ao reply quando a alegação não confere com o disco.
	out = hermesVerifyOutput(out)
	if req.Mode == "plan" {
		return &smartTextResult{reply: out + "\n\n(Modo planejar: nenhuma acao foi executada — o Hermes foi chamado sem auto-aprovacao de ferramentas.)", mode: "hermes_plan", engine: "hermes"}
	}
	return &smartTextResult{reply: out, mode: "hermes", engine: "hermes"}
}

// buildFallbackChat (#14/#15/#16): chat normal com web search opcional.
// #14 (erro do routeModel) vence sobre o aviso de agentFailure;
// #15 preserva modelUsed="" mesmo após routeModel bem-sucedido.
func buildFallbackChat(msg string, req ClientRequest, agentFailure string) *smartTextResult {
	model := selectBestModel(msg)
	webMode := "chat"
	msgs := make([]Message, 0, len(req.History)+2)
	msgs = append(msgs, Message{Role: "system", Content: smartChatSystemPrompt()})
	if req.WebSearch {
		if rs := searchDDG(msg); rs != "" {
			sysContent := "Use os dados abaixo (busca web) para responder com precisão:\n\n🔍 Busca:\n" + rs
			msgs = append(msgs, Message{Role: "system", Content: sysContent})
			webMode = "web_chat"
		}
	}
	hist := req.History
	if n := len(hist); n > 0 && hist[n-1].Role == "user" && hist[n-1].Content == msg {
		hist = hist[:n-1]
	}
	for _, t := range hist {
		msgs = append(msgs, Message{Role: t.Role, Content: t.Content})
	}
	msgs = append(msgs, Message{Role: "user", Content: msg})
	out, modelUsed, err := routeModel(model, msgs, req)
	if err != nil {
		return &smartTextResult{reply: "Erro no chat: " + err.Error(), mode: "error", engine: "chat"}
	}
	if agentFailure != "" {
		return &smartTextResult{
			reply: "⚠️ Nao consegui concluir a acao de automacao (erro: " + agentFailure +
				"). Nada foi criado ou alterado. Tente reformular o pedido ou pedir novamente.\n\n" + out,
			mode: "n8n_agent_fallback", engine: "n8n_agent",
		}
	}
	return &smartTextResult{reply: out, mode: webMode, engine: webMode, modelUsed: modelUsed}
}

// === FASE 2b: Extrair comando bash da mensagem do usuario ===
func extractBashCommand(msg string) string {
	msg = strings.TrimSpace(msg)
	// Remove prefixos comuns (case-insensitive, seguro para UTF-8)
	prefixes := []string{"execute ", "run ", "rode ", "faca ", "exec "}
	for _, p := range prefixes {
		if strings.HasPrefix(strings.ToLower(msg), p) {
			msg = strings.TrimSpace(msg[len(p):])
			break
		}
	}
	// Remove sufixos comuns — busca case-insensitive na string original
	suffixes := []string{
		" no backend", " no frontend", " no arquivo", " no diretorio",
		" no diretório", " no projeto", " no codigo", " no código",
		" no sistema", " no servidor",
	}
	for _, s := range suffixes {
		lowerMsg := strings.ToLower(msg)
		if idx := strings.Index(lowerMsg, s); idx >= 0 && idx < len(msg) {
			msg = strings.TrimSpace(msg[:idx])
			break
		}
	}
	return strings.TrimSpace(msg)
}

// === FASE 2b: Detector de Self-Mod ===

// === Auto-aprovacao: comandos de LEITURA pulam o gate de aprovacao ===
// Whitelist estrita: so os prefixos abaixo, sem redirecionamento/pipe/encadeamento
// para comandos fora da whitelist. Qualquer coisa fora disso cai no fluxo normal
// (PendingAction / aprovacao manual).
func isReadOnlySafeCommand(cmd string) bool {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)

	// Bloqueia qualquer sinal de escrita, encadeamento ou redirecionamento
	dangerous := []string{">", ">>", "|", ";", "&&", "||", "`", "$(", "rm ", "mv ", "cp ", "chmod", "chown", "kill", "systemctl", "sudo", "su ", "dd ", "mkfs"}
	for _, d := range dangerous {
		if strings.Contains(lower, d) {
			return false
		}
	}

	readonlyPrefixes := []string{"ls ", "ls", "cat ", "grep ", "find ", "pwd", "whoami", "df ", "df", "du ", "du", "ps ", "ps"}
	for _, p := range readonlyPrefixes {
		if lower == strings.TrimSpace(p) || strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// autoModBinaries: binários reconhecidos para detecção de comando shell
// literal no Chat (auto-mod/execução direta). Texto em linguagem natural NÃO
// passa — a mensagem vai para o modelo como instrução.
var autoModBinaries = map[string]bool{
	"ls": true, "cat": true, "find": true, "grep": true, "echo": true,
	"git": true, "sed": true, "mv": true, "cp": true, "mkdir": true,
	"rm": true, "touch": true, "chmod": true, "chown": true, "go": true,
	"pwd": true, "whoami": true, "ps": true, "kill": true, "df": true,
	"du": true, "systemctl": true, "curl": true, "docker": true,
}

// isSelfModCommand só dispara para comando shell LITERAL de uma linha — nunca
// para texto colado em prosa/markdown (blocos ```, cabeçalhos #, listas, URLs).
// FIX 20/08: a heurística antiga (substring "ls", "cat", "backend"... em
// qualquer posição) classificava prompts longos em markdown como comando bash,
// disparando "Comando de automodificação detectado" com o texto inteiro colado
// como script (erro de sintaxe bash). Agora: mensagem multi-linha, com
// markdown/prosa ou pontuação de frase NUNCA cai aqui — vai para o modelo como
// instrução em linguagem natural.
func isSelfModCommand(msg string) bool {
	t := strings.TrimSpace(msg)
	if t == "" || strings.Contains(t, "\n") {
		return false // multi-linha = prosa/markdown
	}
	// descarta markdown/prosa explícita
	for _, mk := range []string{"```", "#", "**", "##", "Contexto:", "Tarefa:", "- [", "* ", "|"} {
		if strings.Contains(t, mk) {
			return false
		}
	}
	// descarta pontuação de frase/markdown inline (frases, URLs com :, etc.)
	if strings.ContainsAny(t, "!?:()—") {
		return false
	}
	// remove marcador shell explícito, se houver
	if strings.HasPrefix(t, "$ ") {
		t = strings.TrimSpace(strings.TrimPrefix(t, "$ "))
	}
	parts := strings.Fields(t)
	if len(parts) < 2 {
		return false // precisa de comando + argumento (contexto de projeto)
	}
	if !autoModBinaries[path.Base(parts[0])] {
		return false // primeiro token não é um binário conhecido
	}
	// requisito de projeto mantido: comando precisa referenciar o contexto HOK
	tl := strings.ToLower(t)
	return strings.Contains(tl, "/root/hokma") ||
		strings.Contains(tl, "backend") ||
		strings.Contains(tl, "frontend")
}

// smartChatSystemPrompt — identidade e estilo de conversa do HOK:
// parceiro técnico, dev sênior, PT-BR com termos técnicos em inglês.
func smartChatSystemPrompt() string {
	base := `# Identidade

Você é o Hok, parceiro técnico do Hokmá (Washington Luis) no desenvolvimento do HOK OS —
um sistema operacional de IA em Go rodando numa VPS Debian, com automações via n8n,
e um CRM imobiliário paralelo (Imóveis Chaves).

# Como conversar

- Fale como um dev sênior conversando com outro dev, não como assistente atendendo usuário.
  Nada de "Claro! Vou te ajudar com isso" ou "Espero que isso ajude!". Vá direto ao ponto.
- Português do Brasil, mas termos técnicos ficam em inglês quando é assim que devs falam
  no dia a dia: commit, build, deploy, patch, merge, rollback, endpoint, gate, backup.
  Não traduza esses termos à força.
- O Hokmá opera via SSH mobile (Termius), então comandos devem vir em blocos prontos pra
  copiar e colar — sem passos intermediários desnecessários.
- Seja direto sobre risco e trade-off antes de executar algo. Se um plano tem um problema,
  aponte antes de rodar, não depois. Discordar tecnicamente é esperado, não é falta de
  educação.
- Evite recapitular o que o Hokmá acabou de pedir. Ele sabe o que pediu.
- Quando for investigar algo (bug, erro, comportamento estranho), primeiro diga o que vai
  checar e por quê, peça o output, e só depois conclua — não invente causa sem ver o dado.
- Contexto do projeto (stack, nomes de arquivo, padrões de commit) deve ser assumido como
  já conhecido; não peça pro Hokmá reexplicar o que já está no histórico da conversa.

# O que evitar

- Tom de suporte técnico genérico ("Sinto muito pelo inconveniente", "Estou aqui para
  ajudar no que precisar").
- Respostas vagas tipo "isso pode ser causado por vários motivos" sem investigar primeiro.
- Confirmar sucesso sem ver o log/output real que prova o sucesso.`
	if soul := getSoul(); soul != "" {
		return soul + "\n\n---\n\n" + base
	}
	return base
}
