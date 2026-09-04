package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─── Tipos ────────────────────────────────────────────────────────────────────

type AutopatchReq struct {
	Task    string   `json:"task"`
	Files   []string `json:"files"`
	MaxIter int      `json:"max_iter"`
	OrKey   string   `json:"or_key"`
}

type PatchEntry struct {
	File    string `json:"file"`
	Content string `json:"content"`
}

type ModelPatchResponse struct {
	Explanation string       `json:"explanation"`
	Patches     []PatchEntry `json:"patches"`
}

// ─── SSE helper ──────────────────────────────────────────────────────────────

func sseSend(w http.ResponseWriter, event, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func sseJSON(w http.ResponseWriter, event string, payload interface{}) {
	b, _ := json.Marshal(payload)
	sseSend(w, event, string(b))
}

// ─── Handler principal ────────────────────────────────────────────────────────

func autopatchLoopHandler(w http.ResponseWriter, r *http.Request) {
	// CORS sempre primeiro
	if r.Method == http.MethodOptions {
		setCORS(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	setCORS(w)
	if !requireHokAuth(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("\U0001F534 PANIC em autopatchLoopHandler: %v", rec)
			sseJSON(w, "error", map[string]string{
				"message": fmt.Sprintf("panic interno: %v", rec),
			})
		}
	}()
	log.Printf("\U0001F4E5 autopatch: request recebido")
	var req AutopatchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sseJSON(w, "error", map[string]string{"message": "JSON inv\u00e1lido: " + err.Error()})
		return
	}
	log.Printf("\U0001F4CB autopatch: task=%q files=%v max_iter=%d", req.Task, req.Files, req.MaxIter)
	if req.Task == "" || len(req.Files) == 0 {
		sseJSON(w, "error", map[string]string{"message": "task e files s\u00e3o obrigat\u00f3rios"})
		return
	}
	if req.MaxIter <= 0 {
		req.MaxIter = 4
	}

	// GATE DE APROVACAO
	reqJSON, _ := json.Marshal(req)
	diff := fmt.Sprintf("Autopatch vai tentar modificar automaticamente os arquivos:\n%v\n\nTarefa: %s\nMax iteracoes: %d",
		req.Files, req.Task, req.MaxIter)
	pa := setPendingAction(convIdFromRequest(r), tenantIdFromRequest(r), "", "autopatch", string(reqJSON), diff)
	sseJSON(w, "pending_approval", map[string]interface{}{
		"status":       "pending_approval",
		"action_id":    pa.ID,
		"description":  pa.Description,
		"diff_preview": pa.DiffPreview,
		"message":      "Autopatch requer aprovacao. Use POST /actions/approve com este action_id para executar.",
	})
}

// executeAutopatch roda o loop de patch real. So deve ser chamada apos aprovacao.
func executeAutopatch(req AutopatchReq) string {
	orKey := loadORKey(req.OrKey)
	if orKey == "" {
		return "Erro: OPENROUTER_API_KEY nao encontrada"
	}
	log.Printf("\U0001F511 autopatch: orKey carregada (len=%d)", len(orKey))
	home, _ := os.UserHomeDir()
	backupDir := filepath.Join("/root/hokma", "autopatch_backups",
		time.Now().Format("20060102_150405"))
	os.MkdirAll(backupDir, 0755)
	log.Printf("\U0001F680 autopatch aprovado: task=%q files=%v max_iter=%d backup=%s",
		req.Task, req.Files, req.MaxIter, backupDir)

	fileContents := map[string]string{}
	for _, fp := range req.Files {
		full := resolveFilePath(fp, home)
		data, err := os.ReadFile(full)
		if err != nil {
			fileContents[fp] = ""
		} else {
			fileContents[fp] = string(data)
			log.Printf("\U0001F4C4 autopatch: lido %s (%d bytes)", fp, len(data))
		}
	}

	var lastError string
	for iter := 1; iter <= req.MaxIter; iter++ {
		log.Printf("\U0001F9E0 autopatch: iteracao %d/%d", iter, req.MaxIter)
		prompt := buildPatchPrompt(req.Task, req.Files, fileContents, lastError, iter)
		modelResp, rawResp, err := callHermesForPatch(orKey, prompt)
		if err != nil {
			log.Printf("\u274C autopatch: iter=%d model_error=%v", iter, err)
			lastError = "Erro ao chamar modelo: " + err.Error()
			time.Sleep(2 * time.Second)
			continue
		}
		_ = rawResp
		if len(modelResp.Patches) == 0 {
			buildErr := runGoBuild(home)
			if buildErr == "" {
				return fmt.Sprintf("\u2705 Build OK sem patches na iteracao %d - nada precisava ser alterado.", iter)
			}
			lastError = buildErr
			continue
		}
		for _, patch := range modelResp.Patches {
			full := resolveFilePath(patch.File, home)
			bakPath := filepath.Join(backupDir, fmt.Sprintf("iter%d_%s",
				iter, strings.ReplaceAll(patch.File, "/", "_")))
			existingData, readErr := os.ReadFile(full)
			if readErr == nil {
				os.WriteFile(bakPath, existingData, 0644)
			}
		}
		if applyErr := applyPatches(modelResp.Patches, home); applyErr != nil {
			restoreBackups(modelResp.Patches, backupDir, iter, home)
			lastError = "Erro ao aplicar patch: " + applyErr.Error()
			continue
		}
		buildErr := runGoBuild(home)
		if buildErr == "" {
			for _, patch := range modelResp.Patches {
				fileContents[patch.File] = patch.Content
			}
			log.Printf("[AUDIT] autopatch executado com sucesso - iter=%d backup=%s", iter, backupDir)
			return fmt.Sprintf("\u2705 Build OK na iteracao %d!\n%s\nBackup: %s\nReinicie o servidor: systemctl restart hokma",
				iter, modelResp.Explanation, backupDir)
		}
		restoreBackups(modelResp.Patches, backupDir, iter, home)
		for _, patch := range modelResp.Patches {
			full := resolveFilePath(patch.File, home)
			data, _ := os.ReadFile(full)
			fileContents[patch.File] = string(data)
		}
		lastError = fmt.Sprintf("Build falhou na iteracao %d. Erro do compilador Go:\n%s", iter, buildErr)
	}
	log.Printf("[AUDIT] autopatch esgotou iteracoes sem sucesso - backup=%s", backupDir)
	return fmt.Sprintf("\u274C Esgotadas %d iteracoes sem sucesso. Ultimo erro: %s\nBackup: %s",
		req.MaxIter, truncateStr(lastError, 400), backupDir)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func resolveFilePath(fp, home string) string {
	if filepath.IsAbs(fp) {
		return fp
	}
	if strings.HasPrefix(fp, "~/") {
		return filepath.Join(home, fp[2:])
	}
	return filepath.Join("/root/hokma", "backend", fp)
}

func buildPatchPrompt(task string, files []string, contents map[string]string, lastErr string, iter int) string {
	var sb strings.Builder
	sb.WriteString("Você é um agente autônomo de autopatch para o projeto HOK OS (Go).\n\n")
	sb.WriteString(fmt.Sprintf("TAREFA: %s\n\n", task))

	if lastErr != "" {
		sb.WriteString(fmt.Sprintf("⚠️  ITERAÇÃO %d — ERRO DA TENTATIVA ANTERIOR:\n%s\n\n", iter, lastErr))
		sb.WriteString("Analise o erro acima e corrija sua abordagem.\n\n")
	}

	sb.WriteString("ARQUIVOS ATUAIS:\n")
	for _, fp := range files {
		content := contents[fp]
		if content == "" {
			content = "(arquivo não existe — crie se necessário)"
		}
		// Limita cada arquivo a 8000 chars para não estourar o payload
		if len(content) > 8000 {
			content = content[:8000] + "\n... [truncado para caber no contexto] ..."
		}
		sb.WriteString(fmt.Sprintf("\n--- FILE: %s ---\n%s\n--- END FILE ---\n", fp, content))
	}

	sb.WriteString(`
INSTRUÇÃO CRÍTICA:
Retorne APENAS um JSON válido, sem markdown, sem backticks, sem texto fora do JSON.
Formato exato:
{
  "explanation": "O que foi alterado e por quê",
  "patches": [
    {
      "file": "caminho/relativo/do/arquivo.go",
      "content": "conteúdo completo e válido do arquivo Go"
    }
  ]
}

Regras:
- Inclua apenas arquivos que realmente precisam ser alterados
- O campo "content" deve conter o arquivo Go COMPLETO e válido
- Se nenhuma alteração for necessária, retorne patches: []
- NÃO inclua explicações fora do JSON
- O código Go deve compilar sem erros
`)
	return sb.String()
}

func callHermesForPatch(orKey, prompt string) (*ModelPatchResponse, string, error) {
	// FIX 04/09: removido "google/gemini-2.5-flash" (PAGO) — substituído por
	// ModelB (minimax/minimax-m3:free, pricing 0/0). Mesma política de
	// globals.go para evitar cobrança silenciosa em fallback cascata.
	models := []string{
		"deepseek/deepseek-chat",
		"meta-llama/llama-3.3-70b-instruct",
		ModelB,
	}

	for _, model := range models {
		log.Printf("🤖 autopatch: tentando modelo %s", model)
		raw, err := callOpenRouterPost(orKey, model, prompt, 4000)
		if err != nil {
			log.Printf("⚠️  autopatch: modelo %s falhou: %v", model, err)
			continue
		}
		clean := cleanJSONResponse(raw)
		var parsed ModelPatchResponse
		if jsonErr := json.Unmarshal([]byte(clean), &parsed); jsonErr == nil {
			log.Printf("✅ autopatch: modelo %s respondeu com %d patches", model, len(parsed.Patches))
			return &parsed, raw, nil
		} else {
			log.Printf("⚠️  autopatch: modelo %s JSON inválido: %v", model, jsonErr)
		}
	}
	return &ModelPatchResponse{}, "", fmt.Errorf("todos os modelos falharam")
}

// callOpenRouterPost usa stdin em vez de -d para suportar payloads grandes
func callOpenRouterPost(apiKey, model, prompt string, maxTokens int) (string, error) {
	payload := map[string]interface{}{
		"model":      model,
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %v", err)
	}

	log.Printf("📡 autopatch: curl para OR, payload=%d bytes", len(b))

	// Usa --data-binary @- (stdin) em vez de -d para evitar E2BIG com payloads grandes
	cmd := exec.Command("curl", "-s",
		"-X", "POST",
		"https://openrouter.ai/api/v1/chat/completions",
		"-H", "Authorization: Bearer "+apiKey,
		"-H", "Content-Type: application/json",
		"--data-binary", "@-",
	)
	cmd.Stdin = bytes.NewReader(b)

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("curl error: %v", err)
	}

	log.Printf("📨 autopatch: OR respondeu %d bytes", len(out))

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if jsonErr := json.Unmarshal(out, &resp); jsonErr != nil {
		return "", fmt.Errorf("parse response: %v | raw: %s", jsonErr, truncateStr(string(out), 200))
	}
	if resp.Error.Message != "" {
		return "", fmt.Errorf("API error: %s", resp.Error.Message)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("sem choices na resposta. raw: %s", truncateStr(string(out), 200))
	}
	return resp.Choices[0].Message.Content, nil
}

func cleanJSONResponse(raw string) string {
	s := strings.TrimSpace(raw)
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
	} else if idx := strings.Index(s, "```"); idx >= 0 {
		s = s[idx+3:]
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	if start := strings.Index(s, "{"); start >= 0 {
		s = s[start:]
	}
	return strings.TrimSpace(s)
}

func applyPatches(patches []PatchEntry, home string) error {
	for _, patch := range patches {
		full := resolveFilePath(patch.File, home)
		dir := filepath.Dir(full)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(full, []byte(patch.Content), 0644); err != nil {
			return fmt.Errorf("write %s: %v", full, err)
		}
		log.Printf("📝 autopatch: patch aplicado em %s (%d bytes)", patch.File, len(patch.Content))
	}
	return nil
}

func runGoBuild(home string) string {
	backendDir := "/root/hokma/backend"
	log.Printf("🔨 autopatch: go build em %s", backendDir)
	cmd := exec.Command("go", "build", "-o", "hokma", ".")
	cmd.Dir = backendDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("❌ autopatch: build falhou: %s", truncateStr(string(out), 300))
		return strings.TrimSpace(string(out))
	}
	log.Printf("✅ autopatch: build OK")
	return ""
}

func restoreBackups(patches []PatchEntry, backupDir string, iter int, home string) {
	for _, patch := range patches {
		bakPath := filepath.Join(backupDir, fmt.Sprintf("iter%d_%s",
			iter, strings.ReplaceAll(patch.File, "/", "_")))
		data, err := os.ReadFile(bakPath)
		if err != nil {
			continue
		}
		full := resolveFilePath(patch.File, home)
		os.WriteFile(full, data, 0644)
		log.Printf("↩️  autopatch: restaurado %s", patch.File)
	}
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncado]"
}
