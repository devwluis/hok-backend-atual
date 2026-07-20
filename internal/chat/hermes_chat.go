// Package chat provides the HokMa <-> Hermes bridge.
//
// HTTP client skill-aware que conecta o HokMa (orquestrador) ao Hermes
// (sub-agente Dockerizado). Camada fina: pega uma mensagem do usuario,
// injeta o system prompt (SOUL.md), dispara o hermes-gateway e devolve
// o resultado pro chat handler.
//
// Stack alvo: Go 1.22+, sem dependencias externas alem do stdlib.
// Compativel com net/http, chi, gin.
//
// Author: TON / Mavis
// Data: 2026-07-03
package chat

import (
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

// ============================================================================
// CONFIG
// ============================================================================

type HermesConfig struct {
	BaseURL     string        // ex.: http://localhost:3000 ou http://hermes-gateway:3000
	SOULPath    string        // caminho do SOUL.md (system prompt canonico)
	APIKey      string        // opcional, header X-Hermes-Key
	Timeout     time.Duration // timeout por request
	MaxContext  int           // max msgs no historico que enviamos pro Hermes
	MockMode    bool          // se true, nao chama o hermes (dev offline)
	SkillsAllow []string      // skills que o HokMa EXPOE ao usuario (whitelist)
	SkillsBlock []string      // skills que o HokMa NUNCA expoe (blacklist)
	Models      []string      // modelos disponiveis: ["hermes-3", "llama-3.1-8b"]
}

func DefaultHermesConfig() HermesConfig {
	return HermesConfig{
		BaseURL:    envOrDefault("HERMES_GATEWAY_URL", "http://localhost:3000"),
		SOULPath:   envOrDefault("HOKMA_SOUL_PATH", "/root/hermes-test/.hermes/SOUL.md"),
		APIKey:     os.Getenv("HERMES_GATEWAY_KEY"),
		Timeout:    120 * time.Second,
		MaxContext: 20,
		MockMode:   os.Getenv("HOKMA_MOCK_HERMES") == "1",
		SkillsAllow: []string{
			"agent-reach",
			"impeccable",
			"hokma-n8n-debug",
			"hokma-seo-orchestrator",
			"hokma-security-audit",
			"file-read",
			"shell-run",
		},
		SkillsBlock: []string{
			"system-rm",
			"cred-exfil",
			"db-drop",
		},
		Models: []string{"hermes-3", "llama-3.1-8b-instant"},
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// ============================================================================
// DOMAIN TYPES
// ============================================================================

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ChatMessage struct {
	Role       Role           `json:"role"`
	Content    string         `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Skills      []string      `json:"skills"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	UserID      string        `json:"user_id,omitempty"`
	SessionID   string        `json:"session_id,omitempty"`
}

type ChatResponse struct {
	ID      string      `json:"id"`
	Model   string      `json:"model"`
	Message ChatMessage `json:"message"`
	Usage   Usage       `json:"usage"`
	Error   *APIError   `json:"error,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ============================================================================
// CLIENT
// ============================================================================

type HermesClient struct {
	cfg    HermesConfig
	http   *http.Client
	soul   string
	logger *log.Logger
}

func NewHermesClient(cfg HermesConfig, logger *log.Logger) (*HermesClient, error) {
	if logger == nil {
		logger = log.Default()
	}
	soul, err := os.ReadFile(cfg.SOULPath)
	if err != nil {
		logger.Printf("WARN: SOUL.md nao encontrado em %s: %v", cfg.SOULPath, err)
		soul = []byte(defaultSOUL)
	}
	return &HermesClient{
		cfg:    cfg,
		http:   &http.Client{Timeout: cfg.Timeout},
		soul:   string(soul),
		logger: logger,
	}, nil
}

func (c *HermesClient) SetSOUL(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("ler SOUL: %w", err)
	}
	c.soul = string(b)
	c.cfg.SOULPath = path
	return nil
}

func (c *HermesClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	if req.Model == "" {
		req.Model = "hermes-3"
	}
	if req.Temperature == 0 {
		req.Temperature = 0.7
	}
	if req.MaxTokens == 0 {
		req.MaxTokens = 2048
	}

	req.Skills = c.filterSkills(req.Skills)
	req.Messages = c.injectSystemPrompt(req.Messages)
	req.Messages = c.truncateContext(req.Messages)

	if c.cfg.MockMode {
		return c.mockResponse(req)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	if c.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
		httpReq.Header.Set("X-Hermes-Key", c.cfg.APIKey)
	}
	if req.UserID != "" {
		// // httpReq.Header.Set("X-User-Id", req.UserID) // disabled: breaks OpenRouter // disabled: breaks OpenRouter
	}
	if req.SessionID != "" {
		// // httpReq.Header.Set("X-Session-Id", req.SessionID) // disabled: breaks OpenRouter // disabled: breaks OpenRouter
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("hermes status %d: %s", resp.StatusCode, string(raw))
	}

	var out ChatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("unmarshal: %w (body=%s)", err, string(raw))
	}
	if out.Error != nil {
		return nil, fmt.Errorf("hermes api error [%s]: %s", out.Error.Code, out.Error.Message)
	}
	c.logger.Printf("OK hermes chat user=%s model=%s tokens=%d",
		req.UserID, req.Model, out.Usage.TotalTokens)
	return &out, nil
}

func (c *HermesClient) StreamChat(ctx context.Context, req ChatRequest) (<-chan StreamChunk, <-chan error) {
	chunks := make(chan StreamChunk, 16)
	errs := make(chan error, 1)

	go func() {
		defer close(chunks)
		defer close(errs)

		if c.cfg.MockMode {
			chunks <- StreamChunk{Content: "[mock] Ola do HokMa mock mode\n"}
			chunks <- StreamChunk{Done: true}
			return
		}

		req.Stream = true
		req.Messages = c.injectSystemPrompt(req.Messages)
		req.Messages = c.truncateContext(req.Messages)
		req.Skills = c.filterSkills(req.Skills)

		body, _ := json.Marshal(req)
		url := strings.TrimRight(c.cfg.BaseURL, "/") + "/v1/chat/completions"
		httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "text/event-stream")
		if c.cfg.APIKey != "" {
			httpReq.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
			httpReq.Header.Set("X-Hermes-Key", c.cfg.APIKey)
		}

		resp, err := c.http.Do(httpReq)
		if err != nil {
			errs <- err
			return
		}
		defer resp.Body.Close()

		buf := make([]byte, 4096)
		leftover := []byte{}
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				leftover = append(leftover, buf[:n]...)
				for {
					idx := bytes.Index(leftover, []byte("\n\n"))
					if idx < 0 {
						break
					}
					event := string(leftover[:idx])
					leftover = leftover[idx+2:]
					if strings.HasPrefix(event, "data: ") {
						payload := strings.TrimPrefix(event, "data: ")
						if payload == "[DONE]" {
							chunks <- StreamChunk{Done: true}
							return
						}
						var piece struct {
							Choices []struct {
								Delta struct {
									Content string `json:"content"`
								} `json:"delta"`
								FinishReason *string `json:"finish_reason"`
							} `json:"choices"`
						}
						if err := json.Unmarshal([]byte(payload), &piece); err == nil {
							var content string
							var done bool
							if len(piece.Choices) > 0 {
								content = piece.Choices[0].Delta.Content
								if piece.Choices[0].FinishReason != nil && *piece.Choices[0].FinishReason != "" && *piece.Choices[0].FinishReason != "null" {
									done = true
								}
							}
							select {
							case chunks <- StreamChunk{Content: content, Done: done}:
							case <-ctx.Done():
								return
							}
							if done {
								return
							}
						}
					}
				}
			}
			if err != nil {
				if err != io.EOF {
					errs <- err
				}
				return
			}
		}
	}()

	return chunks, errs
}

type StreamChunk struct {
	Content string
	Done    bool
}

// ============================================================================
// HELPERS
// ============================================================================

func (c *HermesClient) injectSystemPrompt(msgs []ChatMessage) []ChatMessage {
	if len(msgs) > 0 && msgs[0].Role == RoleSystem {
		return msgs
	}
	out := make([]ChatMessage, 0, len(msgs)+1)
	out = append(out, ChatMessage{Role: RoleSystem, Content: c.soul})
	out = append(out, msgs...)
	return out
}

func (c *HermesClient) truncateContext(msgs []ChatMessage) []ChatMessage {
	if len(msgs) <= c.cfg.MaxContext {
		return msgs
	}
	sys := msgs[0]
	tail := msgs[len(msgs)-(c.cfg.MaxContext-1):]
	return append([]ChatMessage{sys}, tail...)
}

func (c *HermesClient) filterSkills(req []string) []string {
	if len(req) == 0 {
		return c.cfg.SkillsAllow
	}
	allowSet := make(map[string]struct{}, len(c.cfg.SkillsAllow))
	for _, s := range c.cfg.SkillsAllow {
		allowSet[s] = struct{}{}
	}
	blockSet := make(map[string]struct{}, len(c.cfg.SkillsBlock))
	for _, s := range c.cfg.SkillsBlock {
		blockSet[s] = struct{}{}
	}
	out := make([]string, 0, len(req))
	for _, s := range req {
		if _, blocked := blockSet[s]; blocked {
			continue
		}
		if _, allowed := allowSet[s]; !allowed {
			continue
		}
		out = append(out, s)
	}
	return out
}

func (c *HermesClient) mockResponse(req ChatRequest) (*ChatResponse, error) {
	last := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == RoleUser {
			last = req.Messages[i].Content
			break
		}
	}
	return &ChatResponse{
		ID:    "mock-" + req.SessionID,
		Model: req.Model,
		Message: ChatMessage{
			Role: RoleAssistant,
			Content: fmt.Sprintf(
				"[mock mode]\nVoce disse: %q\n\nHabilite o hermes-gateway pra eu responder de verdade.",
				last,
			),
		},
		Usage: Usage{PromptTokens: len(last) / 4, CompletionTokens: 32, TotalTokens: len(last)/4 + 32},
	}, nil
}

// ============================================================================
// DEFAULT SOUL (fallback)
// ============================================================================

const defaultSOUL = `Voce e o HokMa, assistente de IA focado em automacao com n8n, SEO e seguranca.
Tom: PT-BR direto, parceiro, sem enrolacao.
Resposta curta antes de longa. Codigo > prosa. JSON limpo > YAML.`

// ============================================================================
// EXEMPLO DE USO
// ============================================================================
//
//	cfg := chat.DefaultHermesConfig()
//	client, _ := chat.NewHermesClient(cfg, log.Default())
//
//	req := chat.ChatRequest{
//		Model:  "hermes-3",
//		UserID: "ton",
//		Skills: []string{"hokma-n8n-debug"},
//		Messages: []chat.ChatMessage{
//			{Role: chat.RoleUser, Content: "me ajuda a criar um workflow"},
//		},
//	}
//	resp, err := client.Chat(context.Background(), req)
//	if err != nil { log.Fatal(err) }
//	fmt.Println(resp.Message.Content)
//
// UnmarshalJSON extrai a mensagem de choices[0].message (formato OpenAI-compat)
// e popula o campo Message no formato flat que o resto do código espera.
// Necessário porque o OpenRouter/OpenAI devolvem a mensagem aninhada em choices[0],
// mas a bridge expõe Message no top-level pra API interna.
func (r *ChatResponse) UnmarshalJSON(data []byte) error {
	type rawChoice struct {
		Message ChatMessage `json:"message"`
	}
	type rawResponse struct {
		ID      string      `json:"id"`
		Model   string      `json:"model"`
		Choices []rawChoice `json:"choices"`
		Usage   Usage       `json:"usage"`
		Error   *APIError   `json:"error,omitempty"`
	}
	var raw rawResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.ID = raw.ID
	r.Model = raw.Model
	r.Usage = raw.Usage
	r.Error = raw.Error
	if len(raw.Choices) > 0 {
		r.Message = raw.Choices[0].Message
	}
	return nil
}
