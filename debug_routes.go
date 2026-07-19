package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"strconv"
)

type DebugRequest struct {
	Lines    int    `json:"lines"`
	LogFile  string `json:"log_file"`
	Question string `json:"question"`
}

type DebugResponse struct {
	Timestamp   string       `json:"timestamp"`
	LogFile     string       `json:"log_file"`
	LinesRead   int          `json:"lines_read"`
	LogSnapshot string       `json:"log_snapshot"`
	Diagnosis   string       `json:"diagnosis"`
	Issues      []DebugIssue `json:"issues"`
	Suggestions []string     `json:"suggestions"`
	Severity    string       `json:"severity"`
}

type DebugIssue struct {
	Line     string `json:"line"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

func registerDebugRoutes(mux *http.ServeMux) {
		mux.HandleFunc("/debug/resources", func(w http.ResponseWriter, r *http.Request) { if r.Method != "OPTIONS" && !requireHokAuth(w, r) { return }; handleDebugResources(w, r) })
	mux.HandleFunc("/debug/assistant", handleDebugAssistant)
	mux.HandleFunc("/debug/logs", handleDebugLogs)
	mux.HandleFunc("/debug/status", handleDebugStatus)
}

func handleDebugAssistant(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		respondJSON(w, map[string]string{"error": "POST only"})
		return
	}
	var req DebugRequest
	req.Lines = 100
	req.LogFile = "backend.log"
	json.NewDecoder(r.Body).Decode(&req)
	if req.Lines <= 0 || req.Lines > 500 {
		req.Lines = 100
	}
	homeDir, _ := os.UserHomeDir()
	logPath := filepath.Join(homeDir, "ecossistema", "logs", req.LogFile)
	lines, err := dbgTailFile(logPath, req.Lines)
	if err != nil {
		respondJSON(w, map[string]string{"error": fmt.Sprintf("Erro ao ler log %s: %v", req.LogFile, err)})
		return
	}
	logContent := strings.Join(lines, "\n")
	issues := dbgDetectIssues(lines)
	question := req.Question
	if question == "" {
		question = "Analise estes logs e identifique problemas, causas e solucoes."
	}
	prompt := fmt.Sprintf("Voce e o Debug Assistant do HOK OS.\n\nLOGS (ultimas %d linhas de %s):\n---\n%s\n---\n\nQUESTAO: %s\n\nResponda em portugues com:\n1. DIAGNOSTICO\n2. PROBLEMAS\n3. SUGESTOES\n4. SEVERIDADE: ok | warning | critical", req.Lines, req.LogFile, logContent, question)
	diagnosis, aiErr := dbgCallAI(prompt)
	if aiErr != nil {
		diagnosis = fmt.Sprintf("AI indisponivel: %v\n\nIssues locais: %d", aiErr, len(issues))
	}
	severity := "ok"
	for _, issue := range issues {
		if issue.Category == "error" {
			severity = "critical"
			break
		} else if issue.Category == "warning" {
			severity = "warning"
		}
	}
	resp := DebugResponse{
		Timestamp:   time.Now().Format("2006-01-02 15:04:05"),
		LogFile:     req.LogFile,
		LinesRead:   len(lines),
		LogSnapshot: dbgTruncate(logContent, 2000),
		Diagnosis:   diagnosis,
		Issues:      issues,
		Suggestions: dbgExtractSuggestions(diagnosis),
		Severity:    severity,
	}
	respondJSON(w, resp)
}

func handleDebugLogs(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	logFile := r.URL.Query().Get("file")
	if logFile == "" {
		logFile = "backend.log"
	}
	linesStr := r.URL.Query().Get("lines")
	lines := 50
	fmt.Sscanf(linesStr, "%d", &lines)
	if lines <= 0 || lines > 500 {
		lines = 50
	}
	homeDir, _ := os.UserHomeDir()
	logPath := filepath.Join(homeDir, "ecossistema", "logs", logFile)
	content, err := dbgTailFile(logPath, lines)
	if err != nil {
		respondJSON(w, map[string]string{"error": err.Error()})
		return
	}
	issues := dbgDetectIssues(content)
	respondJSON(w, map[string]interface{}{
		"file":   logFile,
		"lines":  len(content),
		"log":    strings.Join(content, "\n"),
		"issues": issues,
	})
}

func handleDebugStatus(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	homeDir, _ := os.UserHomeDir()
	logsDir := filepath.Join(homeDir, "ecossistema", "logs")
	files := []string{"backend.log", "tunnel.log", "git.log"}
	result := map[string]interface{}{}
	for _, f := range files {
		path := filepath.Join(logsDir, f)
		lines, err := dbgTailFile(path, 20)
		if err != nil {
			result[f] = map[string]string{"error": err.Error()}
			continue
		}
		issues := dbgDetectIssues(lines)
		result[f] = map[string]interface{}{
			"lines":  len(lines),
			"issues": len(issues),
			"last":   dbgLastNonEmpty(lines),
		}
	}
	respondJSON(w, map[string]interface{}{
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"logs":      result,
	})
}

func dbgTailFile(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(lines) <= n {
		return lines, nil
	}
	return lines[len(lines)-n:], nil
}

func dbgDetectIssues(lines []string) []DebugIssue {
	var issues []DebugIssue
	errorKW := []string{"ERROR", "FATAL", "panic", "error", "failed", "FAIL"}
	warnKW := []string{"WARN", "warning", "timeout", "retry", "slow"}
	for _, line := range lines {
		lower := strings.ToLower(line)
		category := ""
		for _, kw := range errorKW {
			if strings.Contains(lower, strings.ToLower(kw)) {
				category = "error"
				break
			}
		}
		if category == "" {
			for _, kw := range warnKW {
				if strings.Contains(lower, strings.ToLower(kw)) {
					category = "warning"
					break
				}
			}
		}
		if category != "" {
			issues = append(issues, DebugIssue{
				Line:     dbgTruncate(line, 200),
				Category: category,
				Message:  dbgExtractMessage(line),
			})
		}
	}
	return issues
}

func dbgCallAI(prompt string) (string, error) {
	apiKey := getEnvOrDefault("OR_KEY", "")
	if apiKey == "" {
		return "", fmt.Errorf("OR_KEY nao configurada")
	}
	payload := map[string]interface{}{
		"model": "nousresearch/hermes-3-llama-3.1-70b",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tokens":  1500,
		"temperature": 0.3,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse error: %v", err)
	}
	choices, ok := result["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return "", fmt.Errorf("resposta vazia: %s", string(respBody))
	}
	choice := choices[0].(map[string]interface{})
	message := choice["message"].(map[string]interface{})
	return message["content"].(string), nil
}

func dbgExtractSuggestions(text string) []string {
	var suggestions []string
	lines := strings.Split(text, "\n")
	inSuggestions := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(strings.ToLower(line), "sugest") {
			inSuggestions = true
			continue
		}
		if inSuggestions && (strings.HasPrefix(line, "-") || strings.HasPrefix(line, "*")) {
			s := strings.TrimLeft(line, "-* ")
			if s != "" {
				suggestions = append(suggestions, s)
			}
		}
		if inSuggestions && strings.HasPrefix(line, "#") {
			break
		}
	}
	return suggestions
}

func dbgExtractMessage(line string) string {
	parts := strings.SplitN(line, " ", 4)
	if len(parts) >= 4 {
		return parts[3]
	}
	return line
}

func dbgTruncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func dbgLastNonEmpty(lines []string) string {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}


// handleDebugResources — GET /debug/resources
// Retorna CPU, memória, disco e bateria do dispositivo
func handleDebugResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !requireHokAuth(w, r) {
		return
	}
	result := map[string]interface{}{
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	}

	// ── CPU ─────────────────────────────────────────────────
	cpuOut := executeCommand("top -bn1 | grep -E 'CPU|cpu' | head -3")
	result["cpu_raw"] = strings.TrimSpace(cpuOut)
	for _, line := range strings.Split(cpuOut, "\n") {
		if strings.Contains(strings.ToLower(line), "cpu") {
			result["cpu_line"] = strings.TrimSpace(line)
			break
		}
	}

	// ── Memória ─────────────────────────────────────────────
	memOut := executeCommand("free -m")
	result["mem_raw"] = strings.TrimSpace(memOut)
	for _, line := range strings.Split(strings.TrimSpace(memOut), "\n") {
		if strings.HasPrefix(line, "Mem:") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				total, _ := strconv.Atoi(fields[1])
				used, _ := strconv.Atoi(fields[2])
				free := 0
				if len(fields) >= 4 {
					free, _ = strconv.Atoi(fields[3])
				}
				result["mem_total_mb"] = total
				result["mem_used_mb"] = used
				result["mem_free_mb"] = free
				if total > 0 {
					result["mem_percent"] = int(float64(used) / float64(total) * 100)
				}
			}
			break
		}
	}

	// ── Disco ───────────────────────────────────────────────
	diskOut := executeCommand("df -h /")
	result["disk_raw"] = strings.TrimSpace(diskOut)
	diskLines := strings.Split(strings.TrimSpace(diskOut), "\n")
	if len(diskLines) >= 2 {
		fields := strings.Fields(diskLines[1])
		if len(fields) >= 5 {
			result["disk_total"] = fields[1]
			result["disk_used"] = fields[2]
			result["disk_avail"] = fields[3]
			if v, e := strconv.Atoi(strings.TrimSuffix(fields[4], "%")); e == nil { result["disk_percent"] = v } else { result["disk_percent"] = fields[4] }
		}
	}

	// ── Bateria ─────────────────────────────────────────────
	batOut := executeCommand("termux-battery-status")
	batStr := strings.TrimSpace(batOut)
	result["battery_raw"] = batStr
	if idx := strings.Index(batStr, `"percentage"`); idx != -1 {
		sub := batStr[idx+len(`"percentage"`):]
		sub = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sub), ":"))
		end := strings.IndexAny(sub, ",}\n")
		if end > 0 {
			pct, err2 := strconv.Atoi(strings.TrimSpace(sub[:end]))
			if err2 == nil {
				result["battery_percent"] = pct
			}
		}
	}

	if idx := strings.Index(batStr, `"status"`); idx != -1 {
		sub := batStr[idx+len(`"status"`):]
		sub = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(sub), ":"))
		sub = strings.Trim(sub, `"`)
		end := strings.IndexAny(sub, `",}`)
		if end > 0 {
			result["battery_status"] = strings.TrimSpace(sub[:end])
		}
	}

	// ── Uptime ──────────────────────────────────────────────
	upOut := executeCommand("uptime -p")
	result["uptime"] = strings.TrimSpace(upOut)

	respondJSON(w, result)
}
