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
		reply, mode, skill, engine := runSmartText(transcript, req, convId, tenantID, userID)
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
			action, err := registerFsExecPendingAction(convId, tenantID, userID, extracted)
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
		reply, mode, skill, engine := runSmartText(msg, req, convId, tenantID, userID)
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

func runSmartText(msg string, req ClientRequest, convId string, tenantID string, userID string) (reply, mode, skill, engine string) {
	// Modo security → DeepHat V1-7B (cibersegurança)
	if containsSecurityKeyword(msg) {
		msgs := []Message{{Role: "user", Content: msg}}
		out, err := callDeepHat("", msgs)
		if err == nil {
			return out, "deephat_security", "DeepHat-V1-7B", "security"
		}
		log.Printf("⚠️ DeepHat falhou: %v — fallback LLM", err)
	}
	if output, _, found := trySkillForMessage(msg); found {
		return output, "skill", msg, "skill"
	}
	if containsN8nKeyword(msg) {
		out, err := RunAgentLoop(context.Background(), msg, req.Mode, req.History, convId, tenantID)
		if err == nil {
			return out, "n8n_agent_loop", "", "n8n_agent"
		}
		log.Printf("⚠️ n8n agent loop falhou: %v — fallback normal", err)
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
	msgs = append(msgs, Message{Role: "system", Content: "Responda sempre em português do Brasil (PT-BR), a menos que o usuário peça explicitamente outro idioma."})
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
	out, err := routeModel(model, msgs, req)
	if err != nil {
		return "Erro no chat: " + err.Error(), "error", "", "chat"
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
