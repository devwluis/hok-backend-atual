package main

// memory test
// memory test 2
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)
// hermesModels — cascade de fallback (modelos válidos no OR)
var hermesModels = []string{
	"deepseek/deepseek-chat",
	"meta-llama/llama-3.3-70b-instruct",
	"google/gemini-2.5-flash",
	"mistralai/mistral-7b-instruct",
}


const defaultHermesModel = "meta-llama/llama-3.3-70b-instruct"

// Cascade: tenta cada modelo em ordem se o anterior falhar
var hermesModelCascade = []string{
	"meta-llama/llama-3.3-70b-instruct",
	"meta-llama/llama-3.3-70b-instruct",
	"google/gemini-2.5-flash",
}

const agentSystemPrompt = `You are Hermes, the reasoning brain of HOK OS &#8212; a self-modifying AI running on Android via Termux.
Given a file and a task, produce a modified version that accomplishes the task.

RULES:
1. Return ONLY valid JSON, no markdown, no explanation outside the JSON object
2. Format: {"reasoning":"...","new_content":"...","done":true,"eval":"..."}
3. new_content = COMPLETE file content (not a diff)
4. reasoning = what you changed and why
5. done = true if task complete, false if you need the build result first
6. eval = assessment after seeing build output (iteration 2+), empty string on iteration 1
7. Preserve all existing functionality unless the task explicitly requires changing it`

type AgentLoopReq struct {
	Task    string `json:"task"`
	File    string   `json:"file"`
	Files   []string `json:"files"`
	Model   string `json:"model"`
	OrKey   string `json:"or_key"`
	DsKey   string `json:"ds_key"`
	MaxIter int    `json:"max_iter"`
}

type IterResult struct {
	Iteration int    `json:"iteration"`
	Reasoning string `json:"reasoning"`
	BuildOK   bool   `json:"build_ok"`
	BuildLog  string `json:"build_log"`
	Eval      string `json:"eval"`
}

type AgentLoopResp struct {
	Success    bool         `json:"success"`
	Task       string       `json:"task"`
	File       string       `json:"file"`
	Iterations []IterResult `json:"iterations"`
	Message    string       `json:"message"`
}

type HermesReply struct {
	Reasoning  string `json:"reasoning"`
	NewContent string `json:"new_content"`
	Done       bool   `json:"done"`
	Eval       string `json:"eval"`
}

func handleAgentLoop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
		return
	}

	var req AgentLoopReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Task == "" || (req.File == "" && len(req.Files) == 0) {
		http.Error(w, `{"status":"error","message":"task e file/files sao obrigatorios"}`, http.StatusBadRequest)
		return
	}
	// Resolve lista de arquivos
	if len(req.Files) == 0 && req.File != "" {
		req.Files = []string{req.File}
	}
	if req.File == "" && len(req.Files) > 0 {
		req.File = req.Files[0]
	}

	if req.DsKey == "" {
		req.DsKey = os.Getenv("DS_KEY")
	}
	if req.OrKey == "" {
		// Tenta carregar do .keys
		if kb, err := os.ReadFile(os.Getenv("HOME") + "/.keys"); err == nil {
			for _, line := range strings.Split(string(kb), "\n") {
				if strings.HasPrefix(line, "OPENROUTER_API_KEY=") {
					req.OrKey = strings.TrimPrefix(line, "OPENROUTER_API_KEY=")
					break
				}
			}
		}
	}
	if req.Model == "" {
		req.Model = defaultHermesModel
	}
	if req.MaxIter < 1 || req.MaxIter > 5 {
		req.MaxIter = 3
	}

	home := os.Getenv("HOME")
	if strings.Contains(req.File, "..") || filepath.IsAbs(req.File) {
		http.Error(w, `{"status":"error","message":"caminho de arquivo invalido"}`, http.StatusBadRequest)
		return
	}
	filePath := filepath.Join(home, "ecossistema", req.File)

	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"nao consegui ler: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// SYSTEM_STATE como contexto
	systemState := ""
	if sb, err := os.ReadFile(filepath.Join(home, "ecossistema", "SYSTEM_STATE.md")); err == nil {
		// Limita a 2000 chars para nao estourar contexto
		s := string(sb)
		if len(s) > 2000 {
			s = s[len(s)-2000:]
		}
		systemState = s
	}

	// Backup
	bakPath := filePath + ".bak_al_" + time.Now().Format("20060102_150405")
	_ = os.WriteFile(bakPath, fileBytes, 0644)

	resp := AgentLoopResp{Task: req.Task, File: req.File}
	currentContent := string(fileBytes)
	originalContent := currentContent
	buildOK := false
	buildLog := ""

	for i := 1; i <= req.MaxIter; i++ {
		agentMem := queryAgentMemory(req.File, req.Model)
		contentSnip := currentContent
		if len(contentSnip) > 4000 { contentSnip = contentSnip[:4000] + "...[truncado]" }
		prompt := fmt.Sprintf("SYSTEM STATE (recent):\n%s\n\nFILE: %s\nCONTENT:\n%s\n\nTASK: %s"+agentMem,
			systemState, req.File, contentSnip, req.Task)
		if i > 1 {
			buildStatus := "SUCCESS"
			if !buildOK {
				buildStatus = "FAILED"
			}
			logToShow := buildLog
			if len(logToShow) > 1500 {
				logToShow = logToShow[:1500] + "...[truncado]"
			}
			prompt += fmt.Sprintf("\n\n[BUILD %s - iter %d]\n%s", buildStatus, i-1, logToShow)
			if !buildOK {
				prompt += "\n\nSUA TAREFA: corrija o erro acima e reenvie arquivo completo."
			}
		}

		// Seleciona API: DeepSeek nativo ou OpenRouter
		apiKey := req.OrKey
			if apiKey == "" { apiKey = os.Getenv("OPENROUTER_API_KEY") }
		apiURL := "https://openrouter.ai/api/v1/chat/completions"
		if req.DsKey != "" && (strings.HasPrefix(req.Model, "deepseek") || req.Model == "") {
			apiKey = req.DsKey
			apiURL = "https://api.deepseek.com/v1/chat/completions"
			if req.Model == "" || strings.HasPrefix(req.Model, "deepseek") {
				req.Model = "deepseek-chat"
			}
		}
		reply, err := callHermesURL(apiKey, apiURL, req.Model, prompt)
		if err != nil {
			resp.Message = fmt.Sprintf("OpenRouter erro iter %d: %s", i, err.Error())
			break
		}

		if err := os.WriteFile(filePath, []byte(reply.NewContent), 0644); err != nil {
			resp.Message = fmt.Sprintf("Erro ao escrever arquivo: %s", err.Error())
			break
		}
		currentContent = reply.NewContent

		buildOK, buildLog = hokBuild(home)

		resp.Iterations = append(resp.Iterations, IterResult{
			Iteration: i,
			Reasoning: reply.Reasoning,
			BuildOK:   buildOK,
			BuildLog:  truncate(buildLog, 500),
			Eval:      reply.Eval,
		})

		if buildOK && reply.Done {
			resp.Success = true
			resp.Message = "Tarefa concluida com sucesso"
			agentUpdateState(home, req.Task, req.File, i)
			hokRestart(home)
			break
		}

		// Anti-loop: mesmo erro 2x seguidas
		if i >= 2 && !buildOK && len(resp.Iterations) >= 2 {
			prev := resp.Iterations[len(resp.Iterations)-2].BuildLog
			curr := truncate(buildLog, 500)
			if prev != "" && prev == curr {
				resp.Message = fmt.Sprintf("Loop detectado: mesmo erro em %d iteracoes — abortando", i)
				_ = os.WriteFile(filePath, []byte(originalContent), 0644)
				break
			}
		}
	}

	if !resp.Success {
		if buildOK {
			resp.Success = true
			resp.Message = "Build OK, max iteracoes atingido"
			agentUpdateState(home, req.Task, req.File, req.MaxIter)
		} else {
			if resp.Message == "" { resp.Message = "Max iteracoes sem build OK - restaurando backup" } 
			_ = os.WriteFile(filePath, []byte(originalContent), 0644)
		}
	}

	appendAgentHistory(req.Task, resp.Message, req.Model, resp.Success)
	saveAgentMemory(req.Task, req.File, req.Model, resp.Success, resp.Message, len(resp.Iterations))
	json.NewEncoder(w).Encode(resp)
}

func callHermesURL(apiKey, apiURL, model, userPrompt string) (*HermesReply, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": agentSystemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.2,
		"max_tokens": 3000,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("HTTP-Referer", "https://hokos.local")

	client := &http.Client{Timeout: 120 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	resBody, _ := io.ReadAll(res.Body)

	var orResp struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
		Error *struct{ Message string `json:"message"` } `json:"error"`
	}
	if err := json.Unmarshal(resBody, &orResp); err != nil {
		return nil, fmt.Errorf("parse error: %s", truncate(string(resBody), 200))
	}
	if orResp.Error != nil {
		return nil, fmt.Errorf("API: %s", orResp.Error.Message)
	}
	if len(orResp.Choices) == 0 {
		return nil, fmt.Errorf("sem choices na resposta")
	}

	raw := strings.TrimSpace(orResp.Choices[0].Message.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var reply HermesReply
	if err := json.Unmarshal([]byte(raw), &reply); err != nil {
		return nil, fmt.Errorf("hermes reply invalido: %s", truncate(raw, 200))
	}
	return &reply, nil
}


func agentUpdateState(home, task, file string, iters int) {
	stateFile := filepath.Join(home, "ecossistema", "SYSTEM_STATE.md")
	entry := fmt.Sprintf("\n## [%s] AgentLoop\n- Task: %s\n- File: %s\n- Iterations: %d\n- Status: SUCCESS\n",
		time.Now().Format("2006-01-02 15:04"), task, file, iters)
	existing, _ := os.ReadFile(stateFile)
	_ = os.WriteFile(stateFile, append(existing, []byte(entry)...), 0644)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...[truncado]"
}

// loadORKey — carrega chave do .keys ou env
func loadORKey(requestKey string) string {
	if requestKey != "" {
		return requestKey
	}
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(home + "/.keys")
	if err != nil {
		return os.Getenv("OPENROUTER_API_KEY")
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "OPENROUTER_API_KEY=") {
			return strings.TrimPrefix(line, "OPENROUTER_API_KEY=")
		}
	}
	return os.Getenv("OPENROUTER_API_KEY")
}

func hokRestart(home string) {
	restartScript := filepath.Join(home, "ecossistema", "auto_restart.sh")
	cmd := exec.Command("setsid", "bash", restartScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Start()
	// Health check pós-restart com rollback automático
	go func() {
		time.Sleep(3 * time.Second)
		healthURL := "http://127.0.0.1:8082/health"
		deadline := time.Now().Add(30 * time.Second)
		alive := false
		for time.Now().Before(deadline) {
			resp, err := http.Get(healthURL)
			if err == nil && resp.StatusCode == 200 {
				resp.Body.Close()
				alive = true
				break
			}
			time.Sleep(3 * time.Second)
		}
		if !alive {
			log.Printf("⚠ Health check falhou após restart — executando rollback automático")
			backupPath := latestBackupFor(filepath.Join(home, "ecossistema", "backend", "hokma"))
			if backupPath != "" {
				restoreBackup(backupPath, filepath.Join(home, "ecossistema", "backend", "hokma"))
				cmd2 := exec.Command("setsid", "bash", restartScript)
				cmd2.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				cmd2.Start()
				log.Printf("✅ Rollback executado — backend restaurado do backup: %s", backupPath)
			} else {
				log.Printf("✗ Rollback falhou — nenhum backup encontrado")
			}
		} else {
			log.Printf("✅ Health check OK — backend estável após restart")
		}
	}()
}

// callGroq — chama Groq API diretamente (llama-3.3-70b, gratuito)

func hokBuild(home string) (bool, string) {
	candidates := []string{
		filepath.Join(home, "hokma", "backend"),
		filepath.Join(home, "backend"),
		"/root/hokma/backend",
	}
	var srcDir string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			if _, err2 := os.Stat(filepath.Join(c, "go.mod")); err2 == nil {
				srcDir = c
				break
			}
		}
	}
	if srcDir == "" {
		return false, "hokBuild: nao encontrei source do HokMa (testei " + strings.Join(candidates, ", ") + ")"
	}
	cmd := exec.Command("go", "build", "-o", "/tmp/hokma_build_check", ".")
	cmd.Dir = srcDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=1")
	out, err := cmd.CombinedOutput()
	return err == nil, string(out)
}
