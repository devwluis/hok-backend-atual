package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func runAutoHealer() {
	time.Sleep(5 * time.Second)
	if out := sqliteExec("SELECT 1;"); strings.Contains(out, "Error") || out == "" {
		initSQLite()
	}
	for _, d := range []string{ROOT_PATH + "/backend/skills", ROOT_PATH + "/docs", SANDBOX_PATH} {
		os.MkdirAll(d, 0755)
	}
	// update imediato (com mutex)
	teleMu.Lock()
	cachedSkills = countSkillsOnDisk()
	cachedMemories = getSQLiteCount("memories")
	cachedTurns = getSQLiteCount("logs")
	cachedRAMPerc = getRAMUsedPercent()
	teleMu.Unlock()
	go func() {
		for {
			teleMu.Lock()
			cachedSkills = countSkillsOnDisk()
			cachedMemories = getSQLiteCount("memories")
			cachedTurns = getSQLiteCount("logs")
			cachedRAMPerc = getRAMUsedPercent()
			teleMu.Unlock()
			time.Sleep(60 * time.Second)
		}
	}()
	log.Println("✅ Auto-healer ativo")
}

// ════════════════════════════════════════════════════════════════════════════
// HTTP HANDLERS
// ════════════════════════════════════════════════════════════════════════════

func handleStats(w http.ResponseWriter) {
	teleMu.RLock()
	defer teleMu.RUnlock()
	respondJSON(w, map[string]interface{}{
		"status":           "online",
		"version":          "v25",
		"battery":          cachedBatteryPerc,
		"battery_stat":     cachedBatteryStat,
		"wifi_ssid":        cachedWifiSSID,
		"wifi_ip":          cachedWifiIP,
		"uptime":           cachedUptime,
		"skills":           cachedSkills,
		"memories":         cachedMemories,
		"turns":            cachedTurns,
		"ram_used_percent": cachedRAMPerc,
		"errors_fixed":     errorsFixed,
		"errors_detected":  errorsDetected,
	})
}

// ─── GET|POST / — Chat + Stats ───────────────────────────────────────────────
func handleRoot(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method == "GET" {
		if !requireHokAuth(w, r) {
			return
		}
		handleStats(w)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var req ClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if req.Action == "transcribe" || req.AudioB64 != "" {
		text, err := transcribeAudio(req.AudioB64, req.ApiKey)
		if err != nil {
			respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		respondJSON(w, map[string]string{"status": "ok", "text": text})
		return
	}
	if req.Action == "command" || req.Command != "" {
		cmd := req.Command
		if cmd == "" {
			cmd = req.Prompt
		}
		output := executeCommandWithSelfHealing(cmd)
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'INFO', 'terminal');`,
			"CMD: "+cmd[:minInt(80, len(cmd))])
		respondJSON(w, map[string]string{"status": "ok", "output": output})
		return
	}
	userMsg := req.Prompt

	if userMsg == "" {
		userMsg = req.Message
	}
	if userMsg == "" {

		respondJSON(w, map[string]string{"status": "error", "message": "mensagem vazia"})
		return
	}
	// ── gate de confirmação de ações pendentes ─────────────
	convId := convIdFromRequest(r)
	tenantID := tenantIdFromRequest(r)
	userID := userIdFromRequest(r)
	if pa := getPendingAction(convId, tenantID, userID); pa != nil {
		if isApprovalText(userMsg) {
			respondJSON(w, map[string]string{"status": "ok", "reply": resolvePendingAction(convId, tenantID, userID, true)})
			return
		}
		if isRejectionText(userMsg) {
			respondJSON(w, map[string]string{"status": "ok", "reply": resolvePendingAction(convId, tenantID, userID, false)})
			return
		}
	}
	// ── fim do gate ─────────────────────────────────────────
	// ── /edit command interceptor ──────────────────────────────
	if isEditCommand(userMsg) {
		handleEditCommand(w, r, userMsg)
		return
	}
	// ── fim do interceptor ──────────────────────────────
	// ── skill agent ──────────────────────────────────────
	if reply, _, handled := trySkillForMessage(userMsg, convId, tenantID, userID); handled {
		respondJSON(w, map[string]string{"status": "ok", "reply": reply})
		return
	}
	// ── fim skill agent ───────────────────────────────────────
	var msgs []Message
	sys := req.System
	if sys == "" {
		sys = "Você é o Hokma, assistente de IA pessoal. Responda em português. Seja preciso e direto."
	}
	// Injeta SOUL.md no system prompt
	if soul := getSoul(); soul != "" {
		sys = soul + "\n\n---\n\n" + sys
	}
	// ── web search antes do LLM ──────────────────────────
	if req.WebSearch {
		rs := searchDDG(userMsg)
		if rs != "" {
			sys += "\n\n🔍 Resultados da busca web (use isso para responder; se não cobrir o que foi perguntado, diga claramente que não encontrou a informação, não invente dados):\n" + rs
		} else {
			sys += "\n\n🔍 A busca web foi tentada para esta pergunta mas não retornou nenhum resultado relevante. Informe ao usuário que não foi possível encontrar essa informação via busca, e NÃO invente dados, placares, números ou fatos específicos. Sugira fontes confiáveis para ele checar manualmente."
		}
	}
	msgs = append(msgs, Message{Role: "system", Content: sys})
	for _, h := range req.History {
		msgs = append(msgs, Message{Role: h.Role, Content: h.Content})
	}
	msgs = append(msgs, Message{Role: "user", Content: userMsg})
	modelID := req.Model
	if modelID == "" {
		modelID = defaultChatModel
	}
	reply, err := routeModel(modelID, msgs, req)
	if err != nil {
		log.Printf("⚠ %s falhou: %v — OR fallback", modelID, err)
		reply, err = callOR(defaultChatModel, msgs)
		if err != nil {
			geminiKey := os.Getenv("GEMINI_KEY")
			if geminiKey != "" {
				reply, err = callGeminiText(geminiKey, "gemini-2.5-flash-lite", msgs)
			}
			if geminiKey == "" || err != nil {
				reply, err = callOR("meta-llama/llama-3.3-70b-instruct:free", msgs)
			}
			if err != nil {
				respondJSON(w, map[string]string{"status": "error", "reply": "Todos os modelos indisponíveis: " + err.Error()})
				return
			}
		}
	}
	safeUser := userMsg[:minInt(200, len(userMsg))]
	safeReply := reply[:minInt(200, len(reply))]
	sqliteExecParams(`INSERT INTO memory (role, content, ts) VALUES ('user', ?, ?);`, safeUser, time.Now().Unix())
	sqliteExecParams(`INSERT INTO memory (role, content, ts) VALUES ('assistant', ?, ?);`, safeReply, time.Now().Unix())
	// cachedTurns é recalculado pelo auto-healer a cada 60s a partir do count
	// real da tabela logs — incrementar aqui seria racy e redundante.
	if req.Stream {
		respondStreamNDJSON(w, reply)
		return
	}
	respondJSON(w, map[string]string{"status": "ok", "reply": reply})
}

// ─── POST /vision ────────────────────────────────────────────────────────────
func handleVision(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var req VisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}
	if req.Prompt == "" {
		req.Prompt = "Descreva esta imagem em português com o máximo de detalhes."
	}
	if req.MimeType == "" {
		req.MimeType = "image/jpeg"
	}
	var reply string
	attempted := []string{}

	// 1. OpenRouter (prioridade — grátis)
	orKey := req.ORKey
	if orKey == "" {
		orKey = OR_KEY
	}
	if orKey != "" {
		modelID := req.Model
		if modelID == "" {
			modelID = "google/gemini-2.0-flash-exp:free"
		}
		var err error
		reply, err = callORVision(orKey, modelID, req.ImageB64, req.MimeType, req.Prompt)
		attempted = append(attempted, modelID)
		if err != nil {
			log.Printf("⚠ OR Vision falhou (%s): %v", modelID, err)
			reply = ""
		}
	}

	// 2. Gemini fallback
	if reply == "" {
		geminiKey := req.GeminiKey
		if geminiKey == "" {
			geminiKey = GEMINI_KEY
		}
		if geminiKey != "" {
			var err error
			reply, err = callGeminiVision(geminiKey, req.ImageB64, req.MimeType, req.Prompt)
			attempted = append(attempted, "gemini-2.0-flash")
			if err != nil {
				log.Printf("⚠ Gemini Vision falhou: %v", err)
				reply = ""
			}
		}
	}

	// 3. OpenAI fallback
	if reply == "" {
		openaiKey := req.OpenAIKey
		if openaiKey == "" {
			openaiKey = OAI_KEY
		}
		if openaiKey != "" {
			var err error
			reply, err = callOpenAIVision(openaiKey, req.ImageB64, req.MimeType, req.Prompt)
			attempted = append(attempted, "gpt-4o")
			if err != nil {
				log.Printf("⚠ OpenAI Vision falhou: %v", err)
				reply = ""
			}
		}
	}

	if reply == "" {
		msg := "Configure uma chave OpenRouter (recomendado), Gemini ou OpenAI nas Configurações."
		if len(attempted) > 0 {
			msg = fmt.Sprintf("Falha nos providers: %s. Verifique suas chaves de API.", strings.Join(attempted, ", "))
		}
		respondJSON(w, map[string]string{"status": "error", "reply": msg})
		return
	}
	sqliteExec(`INSERT INTO logs (event, level, source) VALUES ('Vision OK', 'INFO', 'vision');`)
	respondJSON(w, map[string]string{"status": "ok", "reply": reply})
}

// ─── /memories ───────────────────────────────────────────────────────────────
func handleMemories(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	switch r.Method {
	case "GET":
		out := sqliteExecQuoted("SELECT key, value, timestamp FROM memories ORDER BY timestamp DESC LIMIT 100;")
		var items []map[string]string
		for _, fields := range parseQuotedRows(out, 3) {
			items = append(items, map[string]string{"key": fields[0], "value": fields[1], "ts": fields[2]})
		}
		if items == nil {
			items = []map[string]string{}
		}
		respondJSON(w, map[string]interface{}{"status": "ok", "memories": items})
	case "POST":
		var body struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Key == "" {
			respondJSON(w, map[string]string{"status": "error", "message": "key obrigatório"})
			return
		}
		sqliteExecParams(`INSERT OR REPLACE INTO memories (key, value) VALUES (?, ?);`, body.Key, body.Value)
		respondJSON(w, map[string]string{"status": "ok"})
	case "DELETE":
		key := r.URL.Query().Get("key")
		if key != "" {
			sqliteExecParams(`DELETE FROM memories WHERE key=?;`, key)
		}
		respondJSON(w, map[string]string{"status": "ok"})
	}
}

// ─── /logs ───────────────────────────────────────────────────────────────────
func handleLogs(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if !requireHokAuth(w, r) {
		return
	}
	out := sqliteExecQuoted(`SELECT timestamp, level, event, source FROM logs ORDER BY id DESC LIMIT 100;`)
	var items []map[string]string
	for _, fields := range parseQuotedRows(out, 4) {
		items = append(items, map[string]string{"ts": fields[0], "level": fields[1], "event": fields[2], "source": fields[3]})
	}
	if items == nil {
		items = []map[string]string{}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "logs": items})
}

// safePathInside — garante que o path resolvido fique dentro de ROOT_PATH
func safePathInside(p string) (string, bool) {
	if p == "" {
		p = ROOT_PATH
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(ROOT_PATH, p)
	}
	clean := filepath.Clean(p)
	rel, err := filepath.Rel(ROOT_PATH, clean)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return clean, true
}

// ─── /files ───────────────────────────────────────────────────────────────────
func handleFiles(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method == "GET" {
		path := r.URL.Query().Get("path")
		fp, ok := safePathInside(path)
		if !ok {
			http.Error(w, "path inválido", 400)
			return
		}
		entries, err := os.ReadDir(fp)
		if err != nil {
			respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		var files []map[string]interface{}
		for _, e := range entries {
			info, _ := e.Info()
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			files = append(files, map[string]interface{}{"name": e.Name(), "is_dir": e.IsDir(), "size": size})
		}
		if files == nil {
			files = []map[string]interface{}{}
		}
		respondJSON(w, map[string]interface{}{"status": "ok", "files": files, "path": fp})
		return
	}
	if r.Method == "POST" {
		var body struct {
			Action  string `json:"action"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		fp, ok := safePathInside(body.Path)
		if !ok {
			http.Error(w, "path inválido", 400)
			return
		}
		switch body.Action {
		case "read":
			b, err := os.ReadFile(fp)
			if err != nil {
				respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
				return
			}
			respondJSON(w, map[string]string{"status": "ok", "content": string(b)})
		case "write":
			_, _ = saveBackup(fp)
			if err := os.WriteFile(fp, []byte(body.Content), 0644); err != nil {
				respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
				return
			}
			respondJSON(w, map[string]string{"status": "ok"})
		case "delete":
			os.Remove(fp)
			respondJSON(w, map[string]string{"status": "ok"})
		default:
			http.Error(w, "action inválida", 400)
		}
	}
}

// ─── /skills ─────────────────────────────────────────────────────────────────
func handleSkills(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if !requireHokAuth(w, r) {
		return
	}
	skillsDir := ROOT_PATH + "/backend/skills"
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "ok", "skills": []string{}})
		return
	}
	var skills []map[string]interface{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			b, err := os.ReadFile(filepath.Join(skillsDir, e.Name()))
			if err != nil {
				continue
			}
			var s map[string]interface{}
			if json.Unmarshal(b, &s) == nil {
				skills = append(skills, s)
			}
		}
	}
	if skills == nil {
		skills = []map[string]interface{}{}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "skills": skills})
}

// ─── /codex ───────────────────────────────────────────────────────────────────
func handleCodex(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method == "GET" {
		out := sqliteExecQuoted(`SELECT timestamp, tag, title, content FROM codex ORDER BY id DESC LIMIT 50;`)
		var items []map[string]string
		for _, fields := range parseQuotedRows(out, 4) {
			items = append(items, map[string]string{"ts": fields[0], "tag": fields[1], "title": fields[2], "content": fields[3]})
		}
		if items == nil {
			items = []map[string]string{}
		}
		respondJSON(w, map[string]interface{}{"status": "ok", "codex": items})
		return
	}
	if r.Method == "POST" {
		var body struct {
			Tag     string `json:"tag"`
			Title   string `json:"title"`
			Content string `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		sqliteExecParams(`INSERT INTO codex (tag, title, content) VALUES (?, ?, ?);`, body.Tag, body.Title, body.Content)
		respondJSON(w, map[string]string{"status": "ok"})
	}
}

// ─── /webhook — N8N Cloud Integration ────────────────────────────────────────
func handleWebhook(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	// Valida token
	token := r.Header.Get("X-N8N-Token")
	if N8N_TOKEN == "" {
		w.WriteHeader(503)
		respondJSON(w, map[string]string{"status": "unavailable", "message": "webhook desabilitado (N8N_TOKEN nao configurada)"})
		return
	}
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(N8N_TOKEN)) != 1 {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"status": "unauthorized"})
		return
	}

	if r.Method == "GET" {
		respondJSON(w, map[string]interface{}{
			"status":  "ok",
			"service": "HOK OS",
			"version": "v25",
			"actions": []string{"chat", "command", "memory_save", "memory_get", "status"},
		})
		return
	}

	var payload struct {
		Action  string                 `json:"action"`
		Prompt  string                 `json:"prompt"`
		Model   string                 `json:"model"`
		Command string                 `json:"command"`
		Data    map[string]interface{} `json:"data"`
	}
	json.NewDecoder(r.Body).Decode(&payload)

	t0 := time.Now()

	switch payload.Action {
	case "chat", "":
		if payload.Prompt == "" {
			respondJSON(w, map[string]string{"status": "error", "reply": "prompt vazio"})
			return
		}
		msgs := []Message{
			{Role: "system", Content: "Você é o Hokma, assistente de IA pessoal. Responda em português."},
			{Role: "user", Content: payload.Prompt},
		}
		modelID := payload.Model
		if modelID == "" {
			modelID = selectBestModel(payload.Prompt)
		}
		req := ClientRequest{Model: modelID}
		reply, err := routeModel(modelID, msgs, req)
		if err != nil {
			reply, _ = callDeepSeek("deepseek-chat", msgs)
		}
		respondJSON(w, map[string]interface{}{
			"status": "success", "reply": reply,
			"model": modelID, "time_ms": time.Since(t0).Milliseconds(),
		})

	case "command":
		if payload.Command == "" {
			respondJSON(w, map[string]string{"status": "error", "reply": "command vazio"})
			return
		}
		out := executeCommandWithSelfHealing(payload.Command)
		respondJSON(w, map[string]interface{}{"status": "success", "output": out})

	case "memory_save":
		key, _ := payload.Data["key"].(string)
		val, _ := payload.Data["value"].(string)
		if key == "" {
			respondJSON(w, map[string]string{"status": "error", "reply": "key obrigatório"})
			return
		}
		sqliteExecParams(`INSERT OR REPLACE INTO memories (key, value) VALUES (?, ?);`, key, val)
		respondJSON(w, map[string]string{"status": "success", "reply": "Salvo: " + key})

	case "memory_get":
		query, _ := payload.Data["query"].(string)
		out := sqliteExecParams(
			`SELECT key, value FROM memories WHERE key LIKE ? OR value LIKE ? LIMIT 10;`,
			"%"+query+"%", "%"+query+"%")
		respondJSON(w, map[string]interface{}{"status": "success", "result": out})

	case "status":
		handleStats(w)

	default:
		respondJSON(w, map[string]string{
			"status": "error",
			"reply":  fmt.Sprintf("Action desconhecida: %s. Use: chat, command, memory_save, memory_get, status", payload.Action),
		})
	}
}

// ─── unused var prevention ────────────────────────────────────────────────────
var _ = regexp.MustCompile
var _ = monitorActive

// ════════════════════════════════════════════════════════════════════════════
// MAIN — PORT via env (default 8082, compat. start_hokos.sh)
// ════════════════════════════════════════════════════════════════════════════
func handleTerminal(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != "OPTIONS" && !requireHokAuth(w, r) {
		return
	}

	var body struct {
		Command string `json:"command"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.Command == "" {
		respondJSON(w, map[string]string{"status": "error", "message": "comando vazio"})
		return
	}

	// Segurança: bloqueia comandos destrutivos
	blocked := []string{"rm -rf /", "mkfs", "dd if=", "format", ":(){:|:&};:"}
	for _, b := range blocked {
		if strings.Contains(body.Command, b) {
			respondJSON(w, map[string]string{"status": "error", "message": "comando bloqueado por segurança"})
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", body.Command)
	cmd.Env = append(os.Environ(), "HOME="+os.Getenv("HOME"))
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		respondJSON(w, map[string]interface{}{
			"status": "error",
			"output": string(out),
			"error":  "comando excedeu o limite de 30s",
		})
		return
	}
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"status": "error",
			"output": string(out),
			"error":  err.Error(),
		})
		return
	}
	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"output": string(out),
	})
}

// memoryStatsHandler — GET /memory/stats
// Retorna ranking de modelos com win rate para o frontend
func memoryStatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	type Response struct {
		TopModel string      `json:"top_model"`
		Stats    []ModelStat `json:"stats"`
		Total    int         `json:"total_models"`
	}

	stats := getModelStats()
	resp := Response{
		TopModel: getTopModel(),
		Stats:    stats,
		Total:    len(stats),
	}

	if resp.Stats == nil {
		resp.Stats = []ModelStat{}
	}

	json.NewEncoder(w).Encode(resp)
}

// ─── /summarize-history ───────────────────────────────────────────────────────
func countSkillsOnDisk() int {
	entries, err := os.ReadDir("/root/hokma/backend/skills")
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			count++
		}
	}
	return count
}
