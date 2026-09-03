package main

// API structures for AI calls
type APIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
}
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type APIResponse struct {
	Error   *APIError `json:"error,omitempty"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices,omitempty"`
}
type APIError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}

// ContentPart for vision
type ContentPart struct {
	Type     string       `json:"type"`
	Text     string       `json:"text,omitempty"`
	ImageURL *ImageURLObj `json:"image_url,omitempty"`
}
type ImageURLObj struct {
	URL string `json:"url"`
}

// Client structures
type ClientRequest struct {
	Message         string `json:"message"`
	TerminalSession string `json:"terminalSession,omitempty"` // PONTE: sessão ttyd ativa no envio
	Model           string `json:"model,omitempty"`
	History         []Turn `json:"messages,omitempty"`
	WebSearch       bool   `json:"webSearch,omitempty"`
	ForceClaudeCode bool   `json:"forceClaudeCode,omitempty"`
	ForceHermes     bool   `json:"forceHermes,omitempty"`
	ForceOpenCode   bool   `json:"forceOpenCode,omitempty"`
	ForceOrchestrator bool `json:"forceOrchestrator,omitempty"` // engine orquestrador (03/09)
	AgentID string `json:"agent_id,omitempty"` // subagente manual do orquestrador (03/09)
	ImageB64        string `json:"image_b64,omitempty"`
	ImageMime       string `json:"image_mime,omitempty"`
	AudioB64        string `json:"audio_b64,omitempty"`
	AudioMime       string `json:"audio_mime,omitempty"`
	Action          string `json:"action,omitempty"`
	Mode            string `json:"mode,omitempty"`
	ApiKey          string `json:"api_key,omitempty"`
	Command         string `json:"command,omitempty"`
	Prompt          string `json:"prompt,omitempty"`
	System          string `json:"system,omitempty"`
	GroqKey         string `json:"groq_key,omitempty"`
	OrKey           string `json:"or_key,omitempty"`
	GeminiKey       string `json:"gemini_key,omitempty"`
	OpenAIKey       string `json:"openai_key,omitempty"`
	Stream          bool   `json:"stream,omitempty"`
	Async           bool   `json:"async,omitempty"`
}
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Conversation struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Messages  string `json:"messages"`
	CreatedAt string `json:"created_at"`
}

// Skill struct removida para evitar duplicata (já definida em skills_routes.go)

type VisionRequest struct {
	ImageB64  string `json:"image_b64"`
	MimeType  string `json:"mime_type,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	Model     string `json:"model,omitempty"`
	ORKey     string `json:"or_key,omitempty"`
	GeminiKey string `json:"gemini_key,omitempty"`
	OpenAIKey string `json:"openai_key,omitempty"`
}

type HistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
