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
	Message   string `json:"message"`
	Model     string `json:"model,omitempty"`
	History   []Turn `json:"messages,omitempty"`
	WebSearch bool   `json:"webSearch,omitempty"`
	ImageB64  string `json:"image_b64,omitempty"`
	ImageMime string `json:"image_mime,omitempty"`
	AudioB64  string `json:"audio_b64,omitempty"`
	AudioMime string `json:"audio_mime,omitempty"`
	Action    string `json:"action,omitempty"`
	Mode      string `json:"mode,omitempty"`
	ApiKey    string `json:"api_key,omitempty"`
	Command   string `json:"command,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	System    string `json:"system,omitempty"`
	GroqKey   string `json:"groq_key,omitempty"`
	OrKey     string `json:"or_key,omitempty"`
	GeminiKey string `json:"gemini_key,omitempty"`
	OpenAIKey string `json:"openai_key,omitempty"`
	Stream    bool   `json:"stream,omitempty"`
}
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ClientResponse struct {
	Response  string `json:"response"`
	Model     string `json:"model,omitempty"`
	SkillUsed string `json:"skill_used,omitempty"`
	Mode      string `json:"mode"`
}

type Memory struct {
	ID        int    `json:"id"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Category  string `json:"category"`
	CreatedAt string `json:"created_at"`
}
type Conversation struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	Messages  string `json:"messages"`
	CreatedAt string `json:"created_at"`
}
type HealthResponse struct {
	Status   string `json:"status"`
	Version  string `json:"version"`
	Skills   int    `json:"skills_count"`
	Memories int    `json:"memories_count"`
	Uptime   string `json:"uptime"`
}
type ResourcesResponse struct {
	CPUPercent  float64 `json:"cpu_percent"`
	RAMPercent  float64 `json:"ram_percent"`
	DiskPercent float64 `json:"disk_percent"`
	RAMUsedMB   int64   `json:"ram_used_mb"`
	RAMTotalMB  int64   `json:"ram_total_mb"`
}
type N8NTriggerRequest struct {
	Workflow string                 `json:"workflow"`
	Payload  map[string]interface{} `json:"payload"`
}
type TerminalRequest struct {
	Command string `json:"command"`
}
type TerminalResponse struct {
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
	Code   int    `json:"code"`
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
