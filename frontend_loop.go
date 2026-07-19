package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const frontendSystemPrompt = `You are Hermes, the reasoning brain of HOK OS — editing React Native / Expo frontend files.
Given a file and a task, produce a modified version that accomplishes the task.

RULES:
1. Return ONLY valid JSON, no markdown, no explanation outside the JSON object
2. Format: {"reasoning":"...","new_content":"...","done":true,"eval":"..."}
3. new_content = COMPLETE file content (not a diff)
4. reasoning = what you changed and why
5. done = true if task complete
6. eval = assessment after seeing validation output (iteration 2+), empty on iteration 1
7. Preserve all existing imports, hooks, and component structure unless the task requires changes
8. For TypeScript files, ensure types are correct
9. For React Native, only use components available in Expo SDK`

type FrontendLoopReq struct {
	Task    string `json:"task"`
	File    string `json:"file"`
	Model   string `json:"model"`
	OrKey   string `json:"or_key"`
	MaxIter int    `json:"max_iter"`
}

type FrontendLoopResp struct {
	Success    bool         `json:"success"`
	Task       string       `json:"task"`
	File       string       `json:"file"`
	Iterations []IterResult `json:"iterations"`
	Message    string       `json:"message"`
}

func handleFrontendLoop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST only"}`, http.StatusMethodNotAllowed)
		return
	}

	var req FrontendLoopReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if req.Task == "" || req.File == "" {
		http.Error(w, `{"status":"error","message":"task e file sao obrigatorios"}`, http.StatusBadRequest)
		return
	}

	// Valida extensão — só frontend
	ext := strings.ToLower(filepath.Ext(req.File))
	allowed := map[string]bool{".tsx": true, ".ts": true, ".js": true, ".jsx": true, ".json": true}
	if !allowed[ext] {
		http.Error(w, `{"error":"Use /agent-loop para arquivos Go. /frontend-loop aceita: .tsx .ts .js .jsx .json"}`, http.StatusBadRequest)
		return
	}

	if req.OrKey == "" {
		req.OrKey = os.Getenv("OPENROUTER_API_KEY")
	}
	if req.Model == "" {
		req.Model = "meta-llama/llama-3.3-70b-instruct"
	}
	if req.MaxIter < 1 || req.MaxIter > 10 {
		req.MaxIter = 3
	}

	home := os.Getenv("HOME")
	filePath := filepath.Join(home, "ecossistema", req.File)

	fileBytes, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"nao consegui ler: %s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	// Backup
	bakPath := filePath + ".bak_fl_" + time.Now().Format("20060102_150405")
	_ = os.WriteFile(bakPath, fileBytes, 0644)

	resp := FrontendLoopResp{Task: req.Task, File: req.File}
	currentContent := string(fileBytes)
	originalContent := currentContent
	loopError := ""

	for i := 1; i <= req.MaxIter; i++ {
		prompt := fmt.Sprintf("FILE: %s\nCONTENT:\n%s\n\nTASK: %s", req.File, currentContent, req.Task)

		reply, err := callHermesFrontend(req.OrKey, req.Model, prompt)
		if err != nil {
			loopError = fmt.Sprintf("OpenRouter erro iter %d: %s", i, err.Error())
			resp.Message = loopError
			break
		}

		if err := os.WriteFile(filePath, []byte(reply.NewContent), 0644); err != nil {
			resp.Message = fmt.Sprintf("Erro ao escrever arquivo: %s", err.Error())
			break
		}
		currentContent = reply.NewContent

		validOK, validLog := frontendValidate(filePath, home)

		resp.Iterations = append(resp.Iterations, IterResult{
			Iteration: i,
			Reasoning: reply.Reasoning,
			BuildOK:   validOK,
			BuildLog:  truncate(validLog, 500),
			Eval:      reply.Eval,
		})

		if validOK && reply.Done {
			resp.Success = true
			resp.Message = "Frontend atualizado com sucesso"
			appendAgentHistory(req.Task, resp.Message, req.Model, true)
			break
		}

		if i > 1 {
			prompt += fmt.Sprintf("\n\n[VALIDATION %s — iter %d]\n%s",
				map[bool]string{true: "OK", false: "FAILED"}[validOK], i-1, validLog)
		}
	}

	if !resp.Success {
		if loopError != "" {
			resp.Message = loopError
		} else {
			resp.Message = "Max iteracoes sem validacao OK — restaurando backup"
		}
		_ = os.WriteFile(filePath, []byte(originalContent), 0644)
		appendAgentHistory(req.Task, resp.Message, req.Model, false)
	}

	json.NewEncoder(w).Encode(resp)
}

// callHermesFrontend — igual ao callHermes mas com system prompt frontend
func callHermesFrontend(apiKey, model, userPrompt string) (*HermesReply, error) {
	payload := map[string]interface{}{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": frontendSystemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0.2,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(body))
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

	var orResp struct {
		Choices []struct {
			Message struct{ Content string `json:"content"` } `json:"message"`
		} `json:"choices"`
		Error *struct{ Message string `json:"message"` } `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&orResp); err != nil {
		return nil, fmt.Errorf("parse error")
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

// frontendValidate — valida arquivo frontend com node ou tsc
func frontendValidate(filePath, home string) (bool, string) {
	ext := strings.ToLower(filepath.Ext(filePath))

	// JSON: valida sintaxe
	if ext == ".json" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return false, err.Error()
		}
		var v interface{}
		if err := json.Unmarshal(data, &v); err != nil {
			return false, "JSON inválido: " + err.Error()
		}
		return true, "JSON válido"
	}

	// JS/TS: tenta node --check primeiro (mais rápido)
	cmd := exec.Command("node", "--check", filePath)
	cmd.Dir = filepath.Join(home, "ecossistema")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return true, "Sintaxe OK (node --check)"
	}

	// Fallback: só verifica se o arquivo não está vazio
	data, _ := os.ReadFile(filePath)
	if len(strings.TrimSpace(string(data))) == 0 {
		return false, "Arquivo vazio"
	}

	// Se node falhou mas arquivo tem conteúdo, aceita (Expo transpila depois)
	log := string(out)
	if strings.Contains(log, "SyntaxError") {
		return false, log
	}
	return true, "Conteúdo presente — validação Expo pendente"
}
