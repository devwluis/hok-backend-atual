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

func selectBestModel(prompt string) string {
	p := strings.ToLower(prompt)
	if containsAny(p, []string{"plano", "planej", "agent", "autom", "tarefa", "execute", "organize", "workflow", "roleplay", "personagem"}) {
		return "llama-3.3-70b-versatile"
	}
	if containsAny(p, []string{"rápido", "rapido", "resumo", "resumir", "curto", "breve"}) {
		return "llama-3.1-8b-instant"
	}
	if containsAny(p, []string{"código", "codigo", "bug", "erro", "função", "script", "python", "golang", "fix", "corrig"}) {
		return "llama-3.3-70b-versatile"
	}
	return "llama-3.3-70b-versatile"
}

func routeModel(modelID string, msgs []Message, req ClientRequest) (string, error) {
	// Modelos Groq nativos
	if modelID == "llama-3.3-70b-versatile" || modelID == "llama-3.1-8b-instant" ||
		modelID == "gemma2-9b-it" || strings.HasPrefix(modelID, "llama") ||
		strings.HasPrefix(modelID, "gemma") || strings.HasPrefix(modelID, "mixtral") {
		converted := make([]map[string]string, 0, len(msgs))
		for _, m := range msgs {
			converted = append(converted, map[string]string{
				"role":    m.Role,
				"content": fmt.Sprintf("%v", m.Content),
			})
		}
		text, provider, errFb := callLLMWithFallback(converted, 4096)
		if errFb == nil {
			log.Printf("[ai] routeModel: respondido via %s", provider)
		}
		return text, errFb
	}
	// Cerebras explícito
	if strings.HasPrefix(modelID, "cerebras/") {
		return callCerebras(strings.TrimPrefix(modelID, "cerebras/"), msgs)
	}
	// DeepSeek → pool gratuito
	if strings.HasPrefix(modelID, "deepseek") {
		return callDeepSeek(modelID, msgs)
	}
	// Gemini nativo
	if strings.HasPrefix(modelID, "gemini") {
		return callGeminiText(req.GeminiKey, modelID, msgs)
	}
	// GPT
	if strings.HasPrefix(modelID, "gpt") {
		return callOpenAI(modelID, msgs, req.OpenAIKey)
	}
	// DeepHat -- opt-in explicito, nunca entra no fallback automatico
	if strings.HasPrefix(modelID, "deephat") {
		m := strings.TrimPrefix(modelID, "deephat")
		m = strings.TrimPrefix(m, "/")
		return callDeepHat(m, msgs)
	}
	// Modelos com "/" (OpenRouter ou prefixado)
	if strings.Contains(modelID, "/") {
		orKey := req.OrKey
		if orKey == "" {
			orKey = OR_KEY
		}
		if orKey == "" {
			orKey = os.Getenv("OPENROUTER_API_KEY")
		}
		return callAPI(OR_URL, orKey,
			APIRequest{Model: modelID, Messages: msgs, MaxTokens: 4096},
			map[string]string{"HTTP-Referer": "https://hokma.ai", "X-Title": "Hokma"})
	}
	// Default: pool gratuito
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

// ─── Pool em Cascata (Gratuito) ───────────────────────────────────────────────
// Ordem: Cerebras (1M tok/dia) → Groq (1K req/dia) → Gemini Flash-Lite (1.5K/dia) → OpenRouter Free

func callLLMWithFallback(messages []map[string]string, maxTokens int) (string, string, error) {
	type Provider struct {
		Name         string
		URL          string
		AuthEnv      string
		Model        string
		ExtraHeaders map[string]string
	}
	providers := []Provider{
		{
			Name:    "Cerebras/Llama-70B",
			URL:     CEREBRAS_URL,
			AuthEnv: "CEREBRAS_API_KEY",
			Model:   "gpt-oss-120b",
		},
		{
			Name:    "Groq/Llama-70B",
			URL:     GROQ_URL,
			AuthEnv: "GROQ_KEY",
			Model:   "llama-3.3-70b-versatile",
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

		reqBody := ChatRequest{Model: p.Model, Messages: messages, MaxTokens: maxTokens}
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
				return text, p.Name, nil
			}
		}

		log.Printf("[fallback] %s: corpo bruto: %s", p.Name, string(bodyRead))
		log.Printf("[fallback] %s: resposta vazia", p.Name)
	}

	return "", "", fmt.Errorf("todos os provedores falharam")
}
