package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// ─── Base HTTP ────────────────────────────────────────────────────────────────

func callAPI(url, key string, req APIRequest, extraHeaders map[string]string) (string, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+key)
	for k, v := range extraHeaders {
		httpReq.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var apiResp APIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", err
	}
	if apiResp.Error != nil {
		return "", fmt.Errorf("API error: %s", apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("sem resposta da API")
	}
	return apiResp.Choices[0].Message.Content, nil
}

// ─── Providers individuais ────────────────────────────────────────────────────

// callDeepHat -- modelo de seguranca "uncensored" (pentest/red team).
// Rota opt-in: so roda se modelID comecar com "deephat", nunca entra no fallback automatico.
func callDeepHat(model string, msgs []Message) (string, error) {
	key := os.Getenv("DEEPHAT_API_KEY")
	if key == "" {
		return "", fmt.Errorf("DEEPHAT_API_KEY nao configurado")
	}
	if model == "" {
		model = "Qwen/Qwen2.5-Coder-7B-Instruct"
	}
	return callAPI(
		"https://router.huggingface.co/v1/chat/completions",
		key,
		APIRequest{Model: model, Messages: msgs, MaxTokens: 4096},
		nil,
	)
}

func callCerebras(model string, msgs []Message) (string, error) {
	if CEREBRAS_KEY == "" {
		return "", fmt.Errorf("CEREBRAS_API_KEY não configurado")
	}
	if model == "" {
		model = "gpt-oss-120b"
	}
	return callAPI(CEREBRAS_URL, CEREBRAS_KEY,
		APIRequest{Model: model, Messages: msgs, MaxTokens: 4096}, nil)
}

func callGroq(model string, msgs []Message, groqKey string) (string, error) {
	key := groqKey
	if key == "" {
		key = GROQ_KEY
	}
	if key == "" {
		return "", fmt.Errorf("GROQ_KEY não configurado")
	}
	return callAPI(GROQ_URL, key,
		APIRequest{Model: model, Messages: msgs, MaxTokens: 4096}, nil)
}

func callOR(model string, msgs []Message) (string, error) {
	key := OR_KEY
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY não configurado")
	}
	return callAPI(OR_URL, key,
		APIRequest{Model: model, Messages: msgs, MaxTokens: 4096},
		map[string]string{"HTTP-Referer": "https://hokma.ai", "X-Title": "Hokma"})
}

// callDeepSeek — mantido para compatibilidade com routes.go/agent_loop.go
// Redireciona para o pool gratuito (DeepSeek sem créditos)
func callDeepSeek(model string, msgs []Message) (string, error) {
	log.Printf("[ai] callDeepSeek → pool gratuito (DeepSeek desativado)")
	converted := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		converted = append(converted, map[string]string{
			"role":    m.Role,
			"content": fmt.Sprintf("%v", m.Content),
		})
	}
	text, _, err := callLLMWithFallback(converted, 4096)
	return text, err
}

func callOpenAI(model string, msgs []Message, openaiKey string) (string, error) {
	key := openaiKey
	if key == "" {
		key = OAI_KEY
	}
	if key == "" {
		return "", fmt.Errorf("OPENAI_KEY não configurado")
	}
	return callAPI(OAI_URL, key,
		APIRequest{Model: model, Messages: msgs, MaxTokens: 4096}, nil)
}

func callGeminiText(apiKey, model string, msgs []Message) (string, error) {
	if apiKey == "" {
		apiKey = GEMINI_KEY
	}
	if apiKey == "" {
		return "", fmt.Errorf("GEMINI_KEY não configurado")
	}
	type GContent struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}
	type GReq struct {
		Contents []GContent `json:"contents"`
	}
	var contents []GContent
	for _, m := range msgs {
		if m.Role == "system" {
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		text := fmt.Sprintf("%v", m.Content)
		contents = append(contents, GContent{
			Role: role,
			Parts: []struct {
				Text string `json:"text"`
			}{{Text: text}},
		})
	}
	body, _ := json.Marshal(GReq{Contents: contents})
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		model, apiKey,
	)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != nil {
		return "", fmt.Errorf("Gemini: %s", result.Error.Message)
	}
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		return result.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("Gemini sem resposta")
}

// ─── Vision ───────────────────────────────────────────────────────────────────

func callORVision(orKey, modelID, imageB64, mimeType, prompt string) (string, error) {
	if orKey == "" {
		orKey = OR_KEY
	}
	if orKey == "" {
		orKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if modelID == "" {
		modelID = "google/gemini-2.5-flash:free"
	}
	dataURI := "data:" + mimeType + ";base64," + imageB64
	type VMsg struct {
		Role    string        `json:"role"`
		Content []ContentPart `json:"content"`
	}
	type VReq struct {
		Model    string `json:"model"`
		Messages []VMsg `json:"messages"`
	}
	payload := VReq{
		Model: modelID,
		Messages: []VMsg{{
			Role: "user",
			Content: []ContentPart{
				{Type: "image_url", ImageURL: &ImageURLObj{URL: dataURI}},
				{Type: "text", Text: prompt},
			},
		}},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", OR_URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+orKey)
	req.Header.Set("HTTP-Referer", "https://hokma.ai")
	req.Header.Set("X-Title", "Hokma Vision")
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var apiResp APIResponse
	json.NewDecoder(resp.Body).Decode(&apiResp)
	if apiResp.Error != nil {
		return "", fmt.Errorf("OR Vision: %s", apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("OR Vision: 0 choices")
	}
	return apiResp.Choices[0].Message.Content, nil
}

func callGeminiVision(geminiKey, imageB64, mimeType, prompt string) (string, error) {
	if geminiKey == "" {
		geminiKey = GEMINI_KEY
	}
	if geminiKey == "" {
		return "", fmt.Errorf("GEMINI_KEY não configurado")
	}
	type InlineData struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	type Part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *InlineData `json:"inlineData,omitempty"`
	}
	type Content struct {
		Parts []Part `json:"parts"`
	}
	type GVReq struct {
		Contents []Content `json:"contents"`
	}
	payload := GVReq{Contents: []Content{{Parts: []Part{
		{InlineData: &InlineData{MimeType: mimeType, Data: imageB64}},
		{Text: prompt},
	}}}}
	body, _ := json.Marshal(payload)
	url := fmt.Sprintf(
		"https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=%s",
		geminiKey,
	)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != nil {
		return "", fmt.Errorf("Gemini Vision: %s", result.Error.Message)
	}
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		return result.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("Gemini Vision: sem resposta")
}

func callOpenAIVision(openaiKey, imageB64, mimeType, prompt string) (string, error) {
	if openaiKey == "" {
		openaiKey = OAI_KEY
	}
	if openaiKey == "" {
		return "", fmt.Errorf("OPENAI_KEY não configurado")
	}
	dataURI := "data:" + mimeType + ";base64," + imageB64
	type VMsg struct {
		Role    string        `json:"role"`
		Content []ContentPart `json:"content"`
	}
	type OAIVReq struct {
		Model     string `json:"model"`
		Messages  []VMsg `json:"messages"`
		MaxTokens int    `json:"max_tokens"`
	}
	payload := OAIVReq{
		Model:     "gpt-4o-mini",
		MaxTokens: 1024,
		Messages: []VMsg{{
			Role: "user",
			Content: []ContentPart{
				{Type: "image_url", ImageURL: &ImageURLObj{URL: dataURI}},
				{Type: "text", Text: prompt},
			},
		}},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", OAI_URL, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+openaiKey)
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var apiResp APIResponse
	json.NewDecoder(resp.Body).Decode(&apiResp)
	if apiResp.Error != nil {
		return "", fmt.Errorf("OpenAI Vision: %s", apiResp.Error.Message)
	}
	if len(apiResp.Choices) == 0 {
		return "", fmt.Errorf("OpenAI Vision: 0 choices")
	}
	return apiResp.Choices[0].Message.Content, nil
}

// callDeepSeekVision → redireciona para Gemini Vision
func callDeepSeekVision(imageB64, mimeType, prompt string) (string, error) {
	log.Printf("[ai] callDeepSeekVision → Gemini Vision (DeepSeek desativado)")
	return callGeminiVision(GEMINI_KEY, imageB64, mimeType, prompt)
}

// ─── Áudio ────────────────────────────────────────────────────────────────────

func transcribeAudio(audioB64, apiKey string) (string, error) {
	audioBytes, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		return "", fmt.Errorf("decode base64: %v", err)
	}
	key := apiKey
	if key == "" {
		key = GROQ_KEY
	}
	transcribeURL := "https://api.groq.com/openai/v1/audio/transcriptions"
	modelName := "whisper-large-v3"
	if key == "" {
		key = OAI_KEY
		transcribeURL = "https://api.openai.com/v1/audio/transcriptions"
		modelName = "whisper-1"
	}
	if key == "" {
		return "", fmt.Errorf("nenhuma chave configurada para transcrição")
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, _ := w.CreateFormFile("file", "audio.m4a")
	fw.Write(audioBytes)
	w.WriteField("model", modelName)
	w.WriteField("language", "pt")
	w.Close()
	req, _ := http.NewRequest("POST", transcribeURL, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+key)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Text  string `json:"text"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Error != nil {
		return "", fmt.Errorf("Whisper: %s", result.Error.Message)
	}
	return result.Text, nil
}

func callGroqASR(audioB64 string, mimeType ...string) (string, error) {
	if GROQ_KEY == "" {
		return "", fmt.Errorf("GROQ_KEY não configurada")
	}
	audioBytes, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		return "", fmt.Errorf("ASR decode: %v", err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	ext := "webm"
	if len(mimeType) > 0 {
		switch {
		case strings.Contains(mimeType[0], "mp4"), strings.Contains(mimeType[0], "m4a"):
			ext = "m4a"
		case strings.Contains(mimeType[0], "ogg"):
			ext = "ogg"
		case strings.Contains(mimeType[0], "wav"):
			ext = "wav"
		case strings.Contains(mimeType[0], "mp3"), strings.Contains(mimeType[0], "mpeg"):
			ext = "mp3"
		}
	}
	fw, _ := mw.CreateFormFile("file", "audio."+ext)
	fw.Write(audioBytes)
	mw.WriteField("model", "whisper-large-v3")
	mw.WriteField("language", "pt")
	mw.WriteField("response_format", "json")
	mw.Close()
	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/audio/transcriptions", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+GROQ_KEY)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var result struct {
		Text string `json:"text"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Text == "" {
		return "", fmt.Errorf("ASR: transcrição vazia")
	}
	return result.Text, nil
}

// ─── Roteador de Modelo ───────────────────────────────────────────────────────
// Modelo unico global (fonte de verdade central, persistido em app_settings).
// Antes modelA/ModelB estavam hardcoded separadamente por motor;
// agora activeModel e' o modelo ativo global, definido via /models/select
// e propagado aos 4 motores (Hok chat, Claude Code, Hermes, OpenCode).
// ModelA e ModelB continuam como constantes para compatibilidade,
// mas o modelo \"ativo\" e' determinado por setActiveModel/getActiveModel.

// defaultChatModel e' agora um wrapper que lê do activeModel central.
// Isso garante que qualquer motor que use defaultChatModel pegue o modelo
// atualmente selecionado pelo usuario (e nao hardcoded ModelA).
func getDefaultChatModel() string {
	return getActiveModel()
}

// fallbackChatModel eh o modelo de seguranca quando o ativo falha.
// permanece como ModelB (google/gemini-2.5-flash) por padrao, mas sera
// substituido pelo segundo modelo da lista de fallbacks do callLLMWithFallback.
const fallbackChatModel = ModelB

// activeModelMutex protege activeModel (modelo selecionado via frontend/endpoint).
var (
	activeModelMu sync.RWMutex
	activeModel   = ModelA // inicializa como ModelA para manter backward compatibility
)

// getActiveModel retorna o modelo ativo (persista via setActiveModel).
// Falla para ModelA se nao definido.
func getActiveModel() string {
	activeModelMu.RLock()
	m := activeModel
	activeModelMu.RUnlock()
	return m
}

// setActiveModel atualiza o modelo ativo, persiste em app_settings (key="activeModel")
// e propaga para os arquivos de config dos motores (Claude Code / OpenCode).
func setActiveModel(m string) {
	activeModelMu.Lock()
	activeModel = m
	activeModelMu.Unlock()
	now := time.Now().Unix()
	sqliteExecParams(
		`INSERT OR REPLACE INTO app_settings (key, value, updated_at) VALUES (?, ?, ?);`,
		"activeModel", m, now)
	propagateActiveModelToMotors(m)
}

// activeModelTag devolve uma tag curta pro log (ex: "modelA" / "modelB" / nome curto).
func activeModelTag() string {
	m := getActiveModel()
	if m == ModelA {
		return "modelA"
	}
	if m == ModelB {
		return "modelB"
	}
	return m
}

// logModelIncompatibility registra falha de motor possivelmente causada por
// modelo incompativel (ex: sem tool-use/function-calling). Usa a tabela logs
// (auditoria/historico) para acompanhar quais combinacoes modelo+motor falham.
func logModelIncompatibility(engine, model string, err error) {
	event := "model_incompat|" + model
	sqliteExecParams(
		`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', ?);`,
		event, engine,
	)
	log.Printf("[model_incompatibility] engine=%s model=%s err=%v", engine, model, err)
}

// initActiveModel carrega o modelo ativo persistido em app_settings (se houver)
// e sincroniza os configs dos motores no boot.
func initActiveModel() {
	out := sqliteExecQuoted(`SELECT value FROM app_settings WHERE key = 'activeModel';`)
	rows := parseQuotedRows(out, 1)
	if len(rows) > 0 && strings.TrimSpace(rows[0][0]) != "" {
		m := strings.TrimSpace(rows[0][0])
		activeModelMu.Lock()
		activeModel = m
		activeModelMu.Unlock()
		propagateActiveModelToMotors(m)
	}
}

func selectBestModel(prompt string) string {
	return getActiveModel()
}

// normalizeModelSlugForAPI remove sufixos de tier/billing do slug do catalogo
// antes de montar a chamada ao provedor. O catalogo unificado mescla ids do
// OpenCode Zen (ex: "deepseek-v4-flash-free") com ids do OpenRouter. O sufixo
// "-free" e' metadado da variante gratuita do catalogo Zen, NAO parte do
// model ID aceito pelas APIs OpenAI-compatives (OpenRouter/Cerebras/etc) —
// "deepseek-v4-flash-free" e' rejeitado ("not a valid model ID"), enquanto
// "deepseek-v4-flash" resolve normalmente. A regra so' remove o sufixo em ids
// SEM "/" (formato Zen); ids OpenRouter com "/" e sufixo ":free" (ex:
// "meta-llama/llama-3.3-70b-instruct:free") sao ids reais e ficam intactos.
func normalizeModelSlugForAPI(slug string) string {
	s := strings.TrimSpace(slug)
	if s == "" || strings.Contains(s, "/") {
		return s
	}
	if strings.HasSuffix(strings.ToLower(s), "-free") {
		return s[:len(s)-len("-free")]
	}
	return s
}

func routeModel(modelID string, msgs []Message, req ClientRequest) (string, string, error) {
	// DeepSeek via OpenRouter (modelo padrão do HOK — DeepSeek v4 flash)
	if strings.HasPrefix(modelID, "deepseek") {
		orKey := req.OrKey
		if orKey == "" {
			orKey = OR_KEY
		}
		if orKey == "" {
			orKey = os.Getenv("OPENROUTER_API_KEY")
		}
		apiModel := normalizeModelSlugForAPI(modelID)
		out, err := callAPI(OR_URL, orKey,
			APIRequest{Model: apiModel, Messages: msgs, MaxTokens: 4096},
			map[string]string{"HTTP-Referer": "https://hokma.ai", "X-Title": "Hokma"})
		if err != nil {
			// TRAVA DE SEGURANÇA (29/08): modelo expirado/virou pago/
			// inválido (402/404/410/400) — NÃO cai na cascata nem troca o
			// modelo; a seleção do usuário permanece registrada. Rate-limit
			// (429) é transitório — cai no pool em cascata.
			if status, _ := classifyPermanentModelStatus(err.Error()); status != "" {
				msg, _ := modelBlockReply(status)
				auditModelBlock("chat", modelID, status)
				return msg, "", nil
			}
			return out, modelID, err
		}
		return out, modelID, nil
	}
	// Cerebras explícito
	if strings.HasPrefix(modelID, "cerebras/") {
		out, err := callCerebras(strings.TrimPrefix(modelID, "cerebras/"), msgs)
		if err != nil {
			// TRAVA DE SEGURANÇA (29/08): idem bloco deepseek.
			if status, _ := classifyPermanentModelStatus(err.Error()); status != "" {
				msg, _ := modelBlockReply(status)
				auditModelBlock("chat", modelID, status)
				return msg, "", nil
			}
			return out, modelID, err
		}
		return out, modelID, nil
	}
	// Gemini nativo
	if strings.HasPrefix(modelID, "gemini") {
		out, err := callGeminiText(req.GeminiKey, modelID, msgs)
		return out, modelID, err
	}
	// GPT
	if strings.HasPrefix(modelID, "gpt") {
		out, err := callOpenAI(modelID, msgs, req.OpenAIKey)
		return out, modelID, err
	}
	// DeepHat -- opt-in explicito, nunca entra no fallback automatico
	if strings.HasPrefix(modelID, "deephat") {
		m := strings.TrimPrefix(modelID, "deephat")
		m = strings.TrimPrefix(m, "/")
		out, err := callDeepHat(m, msgs)
		return out, modelID, err
	}
	// AIHubMix (31/08): gateway OpenAI-compatible. Modelos selecionados no
	// catálogo têm ID "aihubmix/<modelo>". Chama o /v1/chat/completions do
	// AIHubMix com a key do .env. TRAVA DE SEGURANÇA idêntica às demais:
	// se o modelo expirar/virar pago/ficar indisponível (402/404/410/400),
	// mostra "Modelo expirou" e NÃO troca sozinho.
	if strings.HasPrefix(modelID, "aihubmix/") {
		apiModel := strings.TrimPrefix(modelID, "aihubmix/")
		key := AIHUBMIX_KEY
		if key == "" {
			key = os.Getenv("AIHUBMIX_API_KEY")
		}
		if key == "" {
			msg, _ := modelUnsupportedReply(modelID)
			auditModelBlock("chat", modelID, modelStatusUnavailable)
			return msg, "", fmt.Errorf("AIHUBMIX_API_KEY nao configurada")
		}
		out, err := callAPI(AIHUBMIX_URL, key,
			APIRequest{Model: apiModel, Messages: msgs, MaxTokens: 4096}, nil)
		if err == nil {
			return out, modelID, nil
		}
		// TRAVA DE SEGURANÇA (29/08): modelo expirado/virou pago/inválido —
		// não cai na cascata nem troca o modelo; a seleção permanece.
		if status, _ := classifyPermanentModelStatus(err.Error()); status != "" {
			msg, _ := modelBlockReply(status)
			auditModelBlock("chat", modelID, status)
			return msg, "", nil
		}
		return out, modelID, err
	}
	// Modelos com "/" (OpenRouter ou prefixado) — com fallback em cascata:
	// se o modelo ativo falhar (429, indisponivel, etc), o pool assume
	// automaticamente e syncActiveModel atualiza a fonte central.
	if strings.Contains(modelID, "/") {
		// TRAVA DE SEGURANÇA (29/08): o routeModel fala direto com o
		// OpenRouter — modelos do tier Zen/Go do opencode (opencode/*,
		// opencode-go/*) não são aceitos. Sem fallback silencioso: mensagem
		// clara e a seleção do usuário permanece registrada.
		if modelForOpenRouter(modelID) == "" {
			msg, _ := modelUnsupportedReply(modelID)
			auditModelBlock("chat", modelID, modelStatusUnavailable)
			return msg, "", nil
		}
		orKey := req.OrKey
		if orKey == "" {
			orKey = OR_KEY
		}
		if orKey == "" {
			orKey = os.Getenv("OPENROUTER_API_KEY")
		}
		apiModel := normalizeModelSlugForAPI(modelID)
		out, err := callAPI(OR_URL, orKey,
			APIRequest{Model: apiModel, Messages: msgs, MaxTokens: 4096},
			map[string]string{"HTTP-Referer": "https://hokma.ai", "X-Title": "Hokma"})
		if err == nil {
			return out, modelID, nil
		}
		// TRAVA DE SEGURANÇA (29/08): modelo expirado/virou pago/inválido
		// (402/404/410/400) — NÃO cai na cascata nem troca o modelo. A
		// seleção do usuário fica registrada e o envio bloqueado até troca
		// manual. Erros transitórios (429/5xx) seguem para o pool em cascata.
		if status, _ := classifyPermanentModelStatus(err.Error()); status != "" {
			msg, _ := modelBlockReply(status)
			auditModelBlock("chat", modelID, status)
			return msg, "", nil
		}
		log.Printf("⚠ routeModel %s falhou: %v — pool em cascata", modelID, err)
		out, modelUsed, ferr := callLLMWithFallback(msgsToMaps(msgs), 4096)
		if ferr == nil && modelUsed != "" {
			syncActiveModel(modelUsed)
		}
		return out, modelUsed, ferr
	}
	// Fallback: usa o modelo ativo global (getDefaultChatModel que le de activeModel)
	modelID = getDefaultChatModel()
	out, modelUsed, err := callLLMWithFallback(msgsToMaps(msgs), 4096)
	if err == nil && modelUsed != "" {
		syncActiveModel(modelUsed)
	}
	return out, modelUsed, err
}

// msgsToMaps converte []Message em []map[string]string para o pool em cascata.
func msgsToMaps(msgs []Message) []map[string]string {
	out := make([]map[string]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, map[string]string{
			"role":    m.Role,
			"content": fmt.Sprintf("%v", m.Content),
		})
	}
	return out
}

// syncActiveModel — FIX 29/08 (trava de segurança): registra quando o modelo
// que realmente respondeu difere do ativo, mas NUNCA persiste a troca — o
// modelo ativo só muda via /models/select (escolha manual do usuário).
func syncActiveModel(modelUsed string) {
	if modelUsed == "" || modelUsed == getActiveModel() {
		return
	}
	log.Printf("[syncActiveModel] fallback disparou: %s → %s — seleção do usuário MANTIDA (sem troca automática)", getActiveModel(), modelUsed)
}

// ─── Pool em Cascata (Fallback) ───────────────────────────────────────────────
// Ordem: DeepSeek v4 flash (OR, modelo padrão) → Cerebras → Gemini Flash-Lite → OpenRouter Free

// callLLMWithFallback retorna (texto, modeloUsado, erro).
// Quando o fallback dispara (provider que respondeu != modelo ativo),
// o modelo usado e' devolvido para que o chamador sincronize a fonte
// de verdade central (setActiveModel) sem perder o contexto da conversa.
func callLLMWithFallback(messages []map[string]string, maxTokens int) (string, string, error) {
	type Provider struct {
		Name         string
		URL          string
		AuthEnv      string
		Model        string
		ExtraHeaders map[string]string
	}

	// Determina o modelo ativo e o fallback baseado nele
	activeModel = getActiveModel()
	var fallbackModel string
	if activeModel == ModelA {
		fallbackModel = ModelB // se ativo for ModelA, fallback e' ModelB
	} else if activeModel == ModelB {
		fallbackModel = ModelA // se ativo for ModelB, fallback e' ModelA
	} else {
		fallbackModel = ModelB // seguranca: se desconhecido, usa ModelB
	}

	providers := []Provider{
		{
			Name:    "HOK/Ativo-" + activeModel,
			URL:     OR_URL,
			AuthEnv: "OPENROUTER_API_KEY",
			Model:   activeModel,
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://hokma.ai",
				"X-Title":      "Hokma",
			},
		},
		{
			Name:    "HOK/Fallback-" + fallbackModel,
			URL:     OR_URL,
			AuthEnv: "OPENROUTER_API_KEY",
			Model:   fallbackModel,
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://hokma.ai",
				"X-Title":      "Hokma",
			},
		},
		{
			Name:    "Cerebras/Llama-70B",
			URL:     CEREBRAS_URL,
			AuthEnv: "CEREBRAS_API_KEY",
			Model:   "gpt-oss-120b",
		},
		{
			Name:    "Gemini/Flash-Lite",
			URL:     "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
			AuthEnv: "GEMINI_KEY",
			Model:   "gemini-2.5-flash-lite",
		},
		{
			Name:    "OR/Llama-70B",
			URL:     OR_URL,
			AuthEnv: "OPENROUTER_API_KEY",
			Model:   "meta-llama/llama-3.3-70b-instruct:free",
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://hokma.ai",
				"X-Title":      "Hokma",
			},
		},
		{
			Name:    "OR/Gemma-4-31B",
			URL:     OR_URL,
			AuthEnv: "OPENROUTER_API_KEY",
			Model:   "google/gemma-4-31b-it:free",
			ExtraHeaders: map[string]string{
				"HTTP-Referer": "https://hokma.ai",
				"X-Title":      "Hokma",
			},
		},
		{
			Name:    "AIHubMix/GPT-5.5-free",
			URL:     AIHUBMIX_URL,
			AuthEnv: "AIHUBMIX_API_KEY",
			Model:   "gpt-5.5-free",
		},
		{
			Name:    "AIHubMix/GLM-5.3-Flash-free",
			URL:     AIHUBMIX_URL,
			AuthEnv: "AIHUBMIX_API_KEY",
			Model:   "coding-glm-5.3-free",
		},
	}

	type ChatRequest struct {
		Model     string              `json:"model"`
		Messages  []map[string]string `json:"messages"`
		MaxTokens int                 `json:"max_tokens"`
	}
	type ChatChoice struct {
		Message map[string]string `json:"message"`
	}
	type ChatResponse struct {
		Choices []ChatChoice `json:"choices"`
		Error   *struct {
			Message string      `json:"message"`
			Code    interface{} `json:"code"`
		} `json:"error"`
	}

	if maxTokens <= 0 {
		maxTokens = 1024
	}

	for _, p := range providers {
		apiKey := os.Getenv(p.AuthEnv)
		if apiKey == "" {
			log.Printf("[fallback] %s: sem chave (%s), pulando", p.Name, p.AuthEnv)
			continue
		}

		reqBody := ChatRequest{Model: normalizeModelSlugForAPI(p.Model), Messages: messages, MaxTokens: maxTokens}
		bodyBytes, _ := json.Marshal(reqBody)
		httpReq, _ := http.NewRequest("POST", p.URL, bytes.NewReader(bodyBytes))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		for k, v := range p.ExtraHeaders {
			httpReq.Header.Set(k, v)
		}

		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(httpReq)
		if err != nil {
			log.Printf("[fallback] %s: erro conexão: %v", p.Name, err)
			continue
		}

		bodyRead, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 {
			log.Printf("[fallback] %s: rate limit (429), próximo", p.Name)
			continue
		}

		var chatResp ChatResponse
		json.Unmarshal(bodyRead, &chatResp)

		if chatResp.Error != nil {
			log.Printf("[fallback] %s: erro API: %s", p.Name, chatResp.Error.Message)
			continue
		}

		if len(chatResp.Choices) > 0 {
			text := chatResp.Choices[0].Message["content"]
			if text != "" {
				log.Printf("[fallback] ✓ %s respondeu", p.Name)
				return text, p.Model, nil
			}
		}

		log.Printf("[fallback] %s: corpo bruto: %s", p.Name, string(bodyRead))
		log.Printf("[fallback] %s: resposta vazia", p.Name)
	}

	return "", "", fmt.Errorf("todos os provedores falharam")
}
