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
	msg := strings.TrimSpace(req.Message)
	if msg == "" {
		msg = strings.TrimSpace(req.Prompt)
	}
	if pa := getPendingAction(); pa != nil && msg != "" {
		if isApprovalText(msg) {
			resp.Reply = resolvePendingAction(true)
			resp.Mode = "action_approved"
			resp.LatencyMs = time.Since(start).Milliseconds()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		if isRejectionText(msg) {
			resp.Reply = resolvePendingAction(false)
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
		reply, mode, skill := runSmartText(transcript, req)
		resp.Reply = "[ASR: " + transcript + "] " + reply
		resp.Mode = "voice_" + mode
		resp.SkillUsed = skill
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
		if msg == "" {
			// message required removido — msg já tem fallback
			return
		}
		reply, mode, skill := runSmartText(msg, req)
		resp.Reply = reply
		resp.Mode = mode
		resp.SkillUsed = skill
	}

	resp.PendingAction = getPendingAction()
	resp.LatencyMs = time.Since(start).Milliseconds()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func containsN8nKeyword(msg string) bool {
	lower := strings.ToLower(msg)
	keywords := []string{"n8n", "workflow", "workflows", "fluxo de trabalho", "fluxos de trabalho", "diagnostique", "diagnostico", "diagnóstico", "ambiente", ".env", "credencial", "credenciais", "backup"}
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func runSmartText(msg string, req ClientRequest) (reply, mode, skill string) {
	// Modo security → DeepHat V1-7B (cibersegurança)
	if containsSecurityKeyword(msg) {
		msgs := []Message{{Role: "user", Content: msg}}
		out, err := callDeepHat("", msgs)
		if err == nil {
			return out, "deephat_security", "DeepHat-V1-7B"
		}
		log.Printf("⚠️ DeepHat falhou: %v — fallback LLM", err)
	}
	if output, _, found := trySkillForMessage(msg); found {
		return output, "skill", msg
	}
	if containsN8nKeyword(msg) {
		out, err := RunAgentLoop(context.Background(), msg, req.Mode, req.History)
		if err == nil {
			return out, "n8n_agent_loop", ""
		}
		log.Printf("⚠️ n8n agent loop falhou: %v — fallback normal", err)
	}
	if isClaudeCodeTask(msg) {
		prompt := buildClaudeCodePrompt(msg, req)
		if req.Mode == "plan" {
			preview, err := callClaudeCode(prompt)
			if err == nil {
				return preview + "\n\n(Modo planejar: nenhuma acao foi executada com permissoes elevadas.)", "claude_code_plan", ""
			}
			log.Printf("⚠️ Claude Code (plan) falhou: %v — fallback normal", err)
		} else {
			argsJSON, _ := json.Marshal(map[string]string{"prompt": prompt})
			desc := describeClaudeCodeAction(prompt)
			setPendingAction("claude_code", string(argsJSON), desc)
			return desc + "\n\nConfirma? (responda sim/nao)", "claude_code_pending", ""
		}
	}
	if isComplexTask(msg) {
		out, err := callHermes(buildHermesPrompt(msg, req))
		if err != nil {
			log.Printf("❌ Hermes erro: %v", err)
		}
		if err == nil {
			return out, "hermes", ""
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
		return "Erro no chat: " + err.Error(), "error", ""
	}
	return out, webMode, ""
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
