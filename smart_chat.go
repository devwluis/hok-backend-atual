package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type SmartChatResp struct {
	Reply         string         `json:"reply"`
	Mode          string         `json:"mode"`
	EngineUsed    string         `json:"engine_used,omitempty"`
	SkillUsed     string         `json:"skill_used,omitempty"`
	LatencyMs     int64          `json:"latency_ms"`
	PendingAction *PendingAction `json:"pendingAction,omitempty"`
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
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = strings.TrimSpace(req.Prompt)
	}
	if pa := getPendingAction(convId, tenantID, userID); pa != nil && msg != "" {
		if isApprovalText(msg) {
			resp.Reply = resolvePendingAction(convId, tenantID, userID, true)
			resp.Mode = "action_approved"
			resp.LatencyMs = time.Since(start).Milliseconds()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		if isRejectionText(msg) {
			resp.Reply = resolvePendingAction(convId, tenantID, userID, false)
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
		reply, mode, skill, engine := runSmartText(r.Context(), transcript, req, convId, tenantID, userID)
		resp.Reply = "[ASR: " + transcript + "] " + reply
		resp.Mode = "voice_" + mode
		resp.SkillUsed = skill
		resp.EngineUsed = engine
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
				reply, vErr = callORVision(req.OrKey, "qwen/qwen2.5-vl-72b-instruct", req.ImageB64, mimeType, prompt)
				if vErr != nil {
					log.Printf("Qwen VL falhou: %v", vErr)
					reply, vErr = callORVision(req.OrKey, "anthropic/claude-haiku-4.5", req.ImageB64, mimeType, prompt)
					if vErr != nil {
						log.Printf("Claude Haiku falhou: %v", vErr)
						reply, vErr = callOpenAIVision(req.OpenAIKey, req.ImageB64, mimeType, prompt)
						if vErr != nil {
							resp.Reply = "Erro visao: " + vErr.Error()
							resp.Mode = "error"
							break
						}
						resp.Mode = "vision_gpt4o_mini"
					} else {
						resp.Mode = "vision_claude_haiku"
					}
				} else {
					resp.Mode = "vision_qwen_vl"
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
		reply, mode, skill, engine := runSmartText(r.Context(), msg, req, convId, tenantID, userID)
		resp.Reply = reply
		resp.Mode = mode
		resp.SkillUsed = skill
		resp.EngineUsed = engine
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

// classifyEngine decide qual engine vai processar a mensagem.
// Mantem a MESMA ordem de prioridade que ja existe em runSmartText:
// n8n (por keyword) > claude_code > hermes > chat padrao.
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

func classifyEngine(msg string, req ClientRequest) string {
	if containsSecurityKeyword(msg) {
		return "security"
	}
	if containsN8nKeyword(msg) {
		return "n8n_agent"
	}
	if req.ForceClaudeCode || isClaudeCodeTask(msg) {
		return "claude_code"
	}
	if req.ForceHermes || isComplexTask(msg) {
		return "hermes"
	}
	return "chat"
}

func runSmartText(ctx context.Context, msg string, req ClientRequest, convId string, tenantID string, userID string) (reply, mode, skill, engine string) {
	// agentFailure guarda o motivo da falha do agente n8n para que o
	// fallback nunca silencie a falha (bug: criação de workflow falhava
	// e o usuário recebia resposta genérica sem saber que nada foi feito).
	agentFailure := ""
	// Modo security → DeepHat V1-7B (cibersegurança)
	if containsSecurityKeyword(msg) {
		msgs := []Message{{Role: "user", Content: msg}}
		out, err := callDeepHat("", msgs)
		if err == nil {
			return out, "deephat_security", "DeepHat-V1-7B", "security"
		}
		log.Printf("⚠️ DeepHat falhou: %v — fallback LLM", err)
	}
	if output, _, found := trySkillForMessage(msg, convId, tenantID, userID); found {
		return output, "skill", msg, "skill"
	}
	if containsN8nKeyword(msg) {
		out, err := RunAgentLoop(ctx, msg, req.Mode, req.History, convId, tenantID)
		if err == nil {
			return out, "n8n_agent_loop", "", "n8n_agent"
		}
		log.Printf("⚠️ n8n agent loop falhou (1a tentativa): %v — retentando uma vez", err)
		out, err = RunAgentLoop(ctx, msg, req.Mode, req.History, convId, tenantID)
		if err == nil {
			return out, "n8n_agent_loop", "", "n8n_agent"
		}
		agentFailure = err.Error()
		log.Printf("⚠️ n8n agent loop falhou (2a tentativa): %v — fallback normal com aviso", err)
	}
	if req.ForceClaudeCode || isClaudeCodeTask(msg) {
		prompt := buildClaudeCodePrompt(msg, req)
		if req.Mode == "plan" {
			preview, err := callClaudeCode(prompt)
			if err == nil {
				return preview + "\n\n(Modo planejar: nenhuma acao foi executada com permissoes elevadas.)", "claude_code_plan", "", "claude_code"
			}
			log.Printf("⚠️ Claude Code (plan) falhou: %v — fallback normal", err)
		} else {
			if !promptNeedsApproval(prompt) {
				out, err := callClaudeCode(prompt)
				if err == nil {
					return out, "claude_code_direct", "", "claude_code"
				}
				log.Printf("⚠️ Claude Code (direto, trivial) falhou: %v — fallback aprovacao", err)
			}
			argsJSON, _ := json.Marshal(map[string]string{"prompt": prompt})
			desc := describeClaudeCodeAction(prompt)
			setPendingAction(convId, tenantID, userID, "claude_code", string(argsJSON), desc)
			return desc + "\n\nConfirma? (responda sim/nao)", "claude_code_pending", "", "claude_code"
		}
	}
	if req.ForceHermes || isComplexTask(msg) {
		out, err := callHermes(buildHermesPrompt(msg, req))
		if err != nil {
			log.Printf("❌ Hermes erro: %v", err)
		}
		if err == nil {
			return out, "hermes", "", "hermes"
		}
	}
	model := selectBestModel(msg)
	// ── web search ──────────────────────────────────────
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
	// ────────────────────────────────────────────────────
	hist := req.History
	if n := len(hist); n > 0 && hist[n-1].Role == "user" && hist[n-1].Content == msg {
		hist = hist[:n-1]
	}
	for _, t := range hist {
		msgs = append(msgs, Message{Role: t.Role, Content: t.Content})
	}
	msgs = append(msgs, Message{Role: "user", Content: msg})
	out, _, err := routeModel(model, msgs, req)
	if err != nil {
		return "Erro no chat: " + err.Error(), "error", "", "chat"
	}
	if agentFailure != "" {
		return "⚠️ Nao consegui concluir a acao de automacao (erro: " + agentFailure +
			"). Nada foi criado ou alterado. Tente reformular o pedido ou pedir novamente.\n\n" +
			out, "n8n_agent_fallback", "", "n8n_agent"
	}
	return out, webMode, "", webMode
}

// patch aplicado via hermes_client.go
// Adicionar este handler para upload com arquivos
func handleSmartChatWithFiles(w http.ResponseWriter, r *http.Request) {
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

	// Parse multipart form (max 10MB)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "invalid multipart form", http.StatusBadRequest)
		return
	}

	// Extrair mensagem
	message := r.FormValue("message")

	// Extrair arquivos
	files := r.MultipartForm.File["files"]
	var attachments []map[string]interface{}

	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			continue
		}
		defer file.Close()

		// Ler conteúdo do arquivo
		content, err := io.ReadAll(file)
		if err != nil {
			continue
		}

		// Determinar tipo do arquivo
		fileType := "file"
		if strings.HasPrefix(fileHeader.Header.Get("Content-Type"), "image/") {
			fileType = "image"
		} else if strings.HasPrefix(fileHeader.Header.Get("Content-Type"), "audio/") {
			fileType = "audio"
		}

		attachments = append(attachments, map[string]interface{}{
			"name":    fileHeader.Filename,
			"size":    fileHeader.Size,
			"type":    fileType,
			"content": base64.StdEncoding.EncodeToString(content),
		})
	}

	// Processar com IA (implementação existente)
	start := time.Now()

	// TODO: Integrar com o sistema de IA existente
	response := map[string]interface{}{
		"response": fmt.Sprintf("Recebi sua mensagem: %s\nAnexos: %d arquivo(s)", message, len(attachments)),
		"model":    "Hokma v22",
		"ms":       time.Since(start).Milliseconds(),
		"tokens":   len(message) / 4,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

func isSelfModCommand(msg string) bool {
	msgLower := strings.ToLower(msg)
	patterns := []string{"sed ", "mv ", "cp ", "go build", "git ", "chmod ", "chown ", "echo ", "ls ", "cat ", "find ", "grep ", "mkdir ", "rm ", "touch ", "pwd", "whoami", "ps ", "kill ", "df ", "du "}
	for _, p := range patterns {
		if strings.Contains(msgLower, p) {
			if strings.Contains(msgLower, "/root/hokma") ||
				strings.Contains(msgLower, "backend") ||
				strings.Contains(msgLower, "frontend") {
				return true
			}
		}
	}
	return false
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

// smartTextResult — retorno padrao dos engines de chat (usado por
// terminal_exec.go e outros engines). Mantido em um unico lugar para
// evitar dependencia circular.
type smartTextResult struct {
	reply      string
	mode       string
	skill      string
	engine     string
	modelUsed  string
}

// containsTerminalKeyword — true quando a mensagem pede execucao no
// terminal (comandos bash, tmux, etc.). Usado pelo gate de terminal_exec.
func containsTerminalKeyword(msg string) bool {
	lower := strings.ToLower(msg)
	// Palavras-chave de contexto: pedir para rodar/executar algo no terminal.
	keywords := []string{
		"terminal", "tmux", "bash", "executa ", "rode ", "rodar ", "execute ",
		"comando", "comandos", "shell", "cmd ", "powershell",
	}
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}
