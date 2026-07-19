package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Hermes v2 test
// DeepSeek test

type AgentHistoryEntry struct {
	Timestamp string `json:"ts"`
	Task      string `json:"task"`
	Result    string `json:"result"`
	Success   bool   `json:"success"`
	Model     string `json:"model"`
}

func historyPath() string {
	return os.Getenv("HOME") + "/ecossistema/agent_history.jsonl"
}

func appendAgentHistory(task, result, model string, success bool) {
	entry := AgentHistoryEntry{
		Timestamp: time.Now().Format("2006-01-02T15:04:05"),
		Task:      task,
		Result:    result,
		Success:   success,
		Model:     model,
	}
	line, _ := json.Marshal(entry)
	f, err := os.OpenFile(historyPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s\n", line)
}

func getRecentHistory(n int) []AgentHistoryEntry {
	f, err := os.Open(historyPath())
	if err != nil {
		return nil
	}
	defer f.Close()

	var entries []AgentHistoryEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e AgentHistoryEntry
		if json.Unmarshal([]byte(line), &e) == nil {
			entries = append(entries, e)
		}
	}

	if len(entries) <= n {
		return entries
	}
	return entries[len(entries)-n:]
}

func handleAgentHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if !requireHokAuth(w, r) {
		return
	}
	limit := 20
	entries := getRecentHistory(limit)
	if entries == nil {
		entries = []AgentHistoryEntry{}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"total":   len(entries),
		"entries": entries,
	})
}

