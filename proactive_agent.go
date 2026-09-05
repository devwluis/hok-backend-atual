package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	proactiveDiskThreshold  = 75.0
	proactiveRAMThresholdMB = 300.0
	proactiveLLMCooldown    = 30 * time.Minute
)

var (
	lastSuggestion       string
	lastSuggestionAt     time.Time
	suggestionMu         sync.RWMutex
	lastProactiveLLMCall time.Time
)

func collectSystemSignals() string {
	var parts []string
	out, err := exec.Command("df", "-h", "/").Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 1 {
			parts = append(parts, "Disco: "+lines[1])
		}
	}
	out, err = exec.Command("free", "-m").Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		if len(lines) > 1 {
			parts = append(parts, "RAM (MB): "+strings.TrimSpace(lines[1]))
		}
	}
	if len(parts) == 0 {
		return "sinais indisponiveis"
	}
	return strings.Join(parts, "\n")
}

// proactiveDiskUsedPercent le a saida de `df -h /` e devolve o percentual
// usado (coluna Use%). Retorna erro se nao conseguir parsear.
func proactiveDiskUsedPercent() (float64, error) {
	out, err := exec.Command("df", "-h", "/").Output()
	if err != nil {
		return -1, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return -1, fmt.Errorf("saida inesperada de df")
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return -1, fmt.Errorf("colunas insuficientes na saida de df")
	}
	val, err := strconv.ParseFloat(strings.TrimSuffix(fields[4], "%"), 64)
	if err != nil {
		return -1, err
	}
	return val, nil
}

// readRAMAvailableMB le a coluna "available" de `free -m`. Retorna -1 se nao
// conseguir parsear (formato inesperado de free).
func readRAMAvailableMB() float64 {
	out, err := exec.Command("free", "-m").Output()
	if err != nil {
		return -1
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return -1
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 7 {
		return -1
	}
	val, err := strconv.ParseFloat(fields[6], 64)
	if err != nil {
		return -1
	}
	return val
}

func startProactiveLoop() {
	time.Sleep(30 * time.Second)
	runProactiveCycle()
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		runProactiveCycle()
	}
}

// runProactiveCycle roda a cada 5min mas so chama o LLM se disco ou RAM
// cruzarem um limite preocupante, respeitando cooldown de 30min entre
// chamadas pagas.
func runProactiveCycle() {
	diskPct, diskErr := proactiveDiskUsedPercent()
	ramMB := readRAMAvailableMB()

	problem := false
	reason := ""
	if diskErr == nil && diskPct > proactiveDiskThreshold {
		problem = true
		reason = fmt.Sprintf("disco em %.1f%%", diskPct)
	}
	if ramMB >= 0 && ramMB < proactiveRAMThresholdMB {
		problem = true
		if reason != "" {
			reason += "; "
		}
		reason += fmt.Sprintf("RAM disponivel em %.0fMB", ramMB)
	}
	if diskErr != nil && ramMB < 0 {
		problem = true
		reason = "sinais locais indisponiveis"
	}

	if !problem {
		suggestionMu.Lock()
		lastSuggestion = "Sistema OK (verificacao local, sem custo de LLM)"
		lastSuggestionAt = time.Now()
		suggestionMu.Unlock()
		return
	}

	suggestionMu.RLock()
	sinceLastCall := time.Since(lastProactiveLLMCall)
	suggestionMu.RUnlock()
	if sinceLastCall < proactiveLLMCooldown {
		suggestionMu.Lock()
		lastSuggestion = fmt.Sprintf("Possivel problema (%s). Proxima verificacao via LLM em %.0fmin.",
			reason, (proactiveLLMCooldown - sinceLastCall).Round(time.Minute).Minutes())
		lastSuggestionAt = time.Now()
		suggestionMu.Unlock()
		return
	}

	signals := collectSystemSignals()
	msgs := []Message{
		{Role: "system", Content: "Voce e o agente de monitoramento do HOK OS. Analise os sinais e sugira acoes concretas se necessario. Se tudo estiver normal, responda apenas: Sistema OK."},
		{Role: "user", Content: fmt.Sprintf("Sinais do sistema:\n%s\n\nSugestao:", signals)},
	}
	// FIX 05/09: trocado de "nousresearch/hermes-3-llama-3.1-70b" (PAGO) para
	// ModelA (deepseek/deepseek-chat-v3.1, FREE). Tarefa é só análise de
	// texto puro (sinais de sistema) — não precisa de tool-use/function-calling,
	// que é a força do ModelB. ModelA é o default de chat e cobre o caso.
	reply, err := callOR(ModelA, msgs)
	if err != nil {
		reply = "[erro: " + err.Error() + "]"
	}
	suggestionMu.Lock()
	lastSuggestion = strings.TrimSpace(reply)
	lastSuggestionAt = time.Now()
	lastProactiveLLMCall = time.Now()
	suggestionMu.Unlock()
}

func handleAgentSuggestions(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	suggestionMu.RLock()
	suggestion := lastSuggestion
	at := lastSuggestionAt
	suggestionMu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"suggestion": suggestion,
		"updated_at": at.Format(time.RFC3339),
		"has_data":   suggestion != "",
	})
}
