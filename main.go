package main

import (
	"log"
	"net/http"
	"os"
)

// Variáveis globais (serão preenchidas pelas variáveis de ambiente)
var (
	PORT      = os.Getenv("PORT")
	DB_PATH   = os.Getenv("DB_PATH")
	ROOT_PATH = os.Getenv("ROOT_PATH")
	N8N_TOKEN = os.Getenv("N8N_TOKEN")

	// API Keys (preenchidas por env ou pelo payload do cliente)
	OR_KEY       = os.Getenv("OR_KEY")
	CEREBRAS_KEY = os.Getenv("CEREBRAS_API_KEY")
	DS_KEY       = os.Getenv("DS_KEY")
	GROQ_KEY     = os.Getenv("GROQ_KEY")
	OAI_KEY      = os.Getenv("OPENAI_API_KEY")
	GEMINI_KEY   = os.Getenv("GEMINI_KEY")
	GEMINI_URL   = "https://generativelanguage.googleapis.com/v1beta/models"

	// API URLs
	OR_URL       = "https://openrouter.ai/api/v1/chat/completions"
	CEREBRAS_URL = "https://api.cerebras.ai/v1/chat/completions"
	DS_URL       = "https://api.deepseek.com/v1/chat/completions"
	GROQ_URL     = "https://api.groq.com/openai/v1/chat/completions"
	OAI_URL      = "https://api.openai.com/v1/chat/completions"

	// Auth
	HOK_API_TOKEN = os.Getenv("HOK_TOKEN")
)

func init() {
	if PORT == "" {
		PORT = "8082"
	}
	if DB_PATH == "" {
		DB_PATH = "/root/hokma/backend/memory.db"
	}
	if ROOT_PATH == "" {
		ROOT_PATH = "/root/hokma"
	}
	if N8N_TOKEN == "" {
		log.Printf("WARN: N8N_TOKEN nao definida no .env — /webhook ficara desabilitado (fail closed).")
	}
	if HOK_API_TOKEN == "" {
		log.Fatal("ERRO CRITICO: variavel de ambiente HOK_TOKEN nao definida. Defina um valor forte e aleatorio em .env antes de iniciar o servidor.")
	}
}

// Variável para o monitor (usada em routes.go)
var monitorActive = false

func main() {
	initSQLite()
	loadPendingActionsFromDB()

	crmDB, err := openCRMDB()
	if err != nil {
		log.Fatalf("crm: erro ao abrir banco: %v", err)
	}
	defer crmDB.Close()
	RegisterCRMRoutes(http.DefaultServeMux, crmDB)
	RegisterEmpreendimentosRoutes(http.DefaultServeMux, crmDB)
	RegisterWhatsAppRoutes(http.DefaultServeMux, crmDB)

	go runAutoHealer()
	go runTriggerLoop()
	go startProactiveLoop()

	// ── Rotas principais ─────────────────────────────────────────────────
	http.HandleFunc("/", handleRoot)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		setCORS(w)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	http.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			setCORS(w)
			w.WriteHeader(204)
			return
		}
		if !requireHokAuth(w, r) {
			return
		}
		handleStats(w)
	})
	http.HandleFunc("/agents", handleAgents)
	http.HandleFunc("/deploy/status", handleDeployStatus)
	http.HandleFunc("/chat/smart", handleSmartChat)
	http.HandleFunc("/openrouter/credits", handleOpenRouterCredits)
	http.HandleFunc("/debug/tools", handleDebugTools)
	http.HandleFunc("/actions/approve", handleActionApprove)
	http.HandleFunc("/actions/reject", handleActionReject)
	http.HandleFunc("/auth/register", handleRegister)
	http.HandleFunc("/auth/login", handleLogin)
	http.HandleFunc("/auth/owner-check", handleOwnerCheck)
	http.HandleFunc("/auth/me", handleMe)
	http.HandleFunc("/conversations", handleConversations)
	http.HandleFunc("/conversations/", handleConversations)
	http.HandleFunc("/repos", handleRepos)
	http.HandleFunc("/repos/", handleRepos)

	// ── Memória, skills, agent ───────────────────────────────────────────
	http.HandleFunc("/memories", handleMemories)
	http.HandleFunc("/skills", handleSkills)
	http.HandleFunc("/pipelines", handlePipelines)
	http.HandleFunc("/pipeline/run", handlePipelineRun)
	http.HandleFunc("/skill/save", handleSaveSkill)
	http.HandleFunc("/agent/task", handleTaskAgent)
	http.HandleFunc("/triggers/status", handleTriggersStatus)
	http.HandleFunc("/agent-loop", handleAgentLoop)
	http.HandleFunc("/agent-loop/tools", handleAgentLoopTools)
	http.HandleFunc("/agent-history", handleAgentHistory)
	http.HandleFunc("/autopatch", autopatchLoopHandler)

	// ── Terminal, filesystem ─────────────────────────────────────────────
	http.HandleFunc("/terminal", handleTerminal)
	http.HandleFunc("/shell", handleTerminal)
	http.HandleFunc("/fs/read", handleFileRead)
	http.HandleFunc("/fs/write", handleFileWrite)
	http.HandleFunc("/fs/list", handleFileList)
	http.HandleFunc("/fs/exec", handleExec)
	http.HandleFunc("/fs/rebuild", handleRebuild)
	http.HandleFunc("/fs/patch", handleFsPatch)
	http.HandleFunc("/fs/rollback", handleFsRollback)
	http.HandleFunc("/fs/backups", handleFsBackupList)

	// ── Debug ────────────────────────────────────────────────────────────
	http.HandleFunc("/debug/resources", handleDebugResources)
	http.HandleFunc("/debug/assistant", handleDebugAssistant)
	http.HandleFunc("/debug/logs", handleDebugLogs)
	http.HandleFunc("/debug/status", handleDebugStatus)

	// ── N8N / automação ──────────────────────────────────────────────────
	http.HandleFunc("/n8n", handleN8N)
	http.HandleFunc("/api/n8n-proxy", handleN8NProxy)
	http.HandleFunc("/n8n/trigger", handleN8NTrigger)
	http.HandleFunc("/n8n/workflows", handleN8NWorkflows)
	http.HandleFunc("/n8n/status", handleN8NStatus)
	http.HandleFunc("/self-heal", selfHealHandler)

	// ── Visão, codex, misc ───────────────────────────────────────────────
	http.HandleFunc("/vision", handleVision)
	http.HandleFunc("/logs", handleLogs)
	http.HandleFunc("/files", handleFiles)
	http.HandleFunc("/codex", handleCodex)
	http.HandleFunc("/webhook", handleWebhook)
	http.HandleFunc("/settings", handleSettings)
	http.HandleFunc("/soul", handleGetSoul)
	http.HandleFunc("/introspect", handleIntrospect)
	http.HandleFunc("/frontend-loop", handleFrontendLoop)
	http.HandleFunc("/device/queue", handleDeviceQueue)
	http.HandleFunc("/device/result", handleDeviceResult)
	http.HandleFunc("/notify", handleNotify)
	http.HandleFunc("/agent/suggestions", handleAgentSuggestions)
	http.HandleFunc("/pipeline/flow", handleFlowPipeline)
	http.HandleFunc("/flows", handleFlows)

	addr := ":" + PORT
	log.Printf("🚀 Hokma v22 → http://localhost%s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal("Erro ao iniciar servidor:", err)
	}
}
