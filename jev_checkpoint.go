package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	jevCheckpointLogPath = "/root/hokma/backend/contexto-hokma/jev_checkpoint_log.jsonl"
	jevModel             = "typesafe/jev-1.13"
	jevEndpoint          = "https://openrouter.ai/api/v1/systemone"
	jevTimeout           = 15 * time.Second
)

var (
	jevLogMu   sync.Mutex
	jevHTTPMu  sync.Mutex
	jevHTTPClient = &http.Client{Timeout: jevTimeout}
)

type JEVRequest struct {
	Model    string             `json:"model"`
	Questions map[string]JEVQuestion `json:"questions"`
	State    JEVState           `json:"state"`
}

type JEVQuestion struct {
	Type          string            `json:"type"`
	Instructions  string            `json:"instructions"`
	Criteria      map[string]string `json:"criteria"`
}

type JEVState struct {
	Descricao     string `json:"descricao_da_mudanca"`
	WorkflowAtivo bool   `json:"workflow_ativo"`
	TemBackup     bool   `json:"tem_backup_recente"`
}

type JEVResponse struct {
	Answers map[string]JEVAnswer `json:"answers"`
}

type JEVAnswer struct {
	Choice       string             `json:"choice"`
	Confidence   float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

type JEVAuditEntry struct {
	Timestamp     string             `json:"timestamp"`
	ConvID        string             `json:"conv_id"`
	TenantID      string             `json:"tenant_id"`
	UserID        string             `json:"user_id"`
	Action        string             `json:"action"`
	Permission    string             `json:"permission"`
	Cmd           string             `json:"cmd"`
	Verdict       string             `json:"verdict"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
	GateDecision  string             `json:"gate_decision"`
	Applied       bool               `json:"applied"`
	Reason        string             `json:"reason"`
	Elapsed       string             `json:"elapsed"`
}

func isInvalidModelErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "status 400") ||
		strings.Contains(msg, "status 403") ||
		strings.Contains(msg, "status 404") ||
		strings.Contains(msg, "invalid_model") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "model_not_found")
}

func JEVCheckpoint(action, permission, cmd, convID, tenantID, userID, model string) (string, float64, map[string]float64, error) {
	start := time.Now()

	if model == "" {
		model = jevModel
	}

	orKey := os.Getenv("OPENROUTER_API_KEY")
	if orKey == "" {
		envFile := os.Getenv("HOK_ENV_FILE")
		if envFile == "" {
			envFile = "/root/hokma/backend/.env"
		}
		if content, err := os.ReadFile(envFile); err == nil {
			for _, line := range bytes.Split(content, []byte("\n")) {
				if bytes.HasPrefix(bytes.TrimSpace(line), []byte("OPENROUTER_API_KEY=")) {
					parts := bytes.SplitN(bytes.TrimSpace(line), []byte("="), 2)
					if len(parts) == 2 {
						orKey = string(parts[1])
					}
					break
				}
			}
		}
	}

	orKey = strings.TrimPrefix(orKey, "Bearer ")
	orKey = strings.TrimSpace(orKey)
	if orKey == "" {
		return "", 0, nil, fmt.Errorf("OPENROUTER_API_KEY não encontrada")
	}

	reqBody := JEVRequest{
		Model: model,
		Questions: map[string]JEVQuestion{
			"risco_acao": {
				Type:          "choice",
				Instructions:  "Classifique o risco desta ação no contexto de um agente autônomo em execução.",
				Criteria: map[string]string{
					"auto":     "Ação segura, reversível, sem risco de perda de dados",
					"review":   "Ação tem risco moderado, deve ser registrada para revisão humana posterior",
					"escalate": "Ação tem risco alto — paralisar e aguardar aprovação explícita",
				},
			},
		},
		State: JEVState{
			Descricao:     fmt.Sprintf("Ação: %s | Permissão: %s | Cmd: %s | Conv: %s", action, permission, cmd, convID),
			WorkflowAtivo: true,
			TemBackup:     true,
		},
	}

	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequest("POST", jevEndpoint, bytes.NewReader(body))
	if err != nil {
		return "", 0, nil, fmt.Errorf("criar request falhou: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+orKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://hokma.local")
	req.Header.Set("X-Title", "HOK JEV Checkpoint")

	resp, err := jevHTTPClient.Do(req)
	if err != nil {
		return "", 0, nil, fmt.Errorf("request falhou: %v", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, nil, fmt.Errorf("ler response falhou: %v", err)
	}

	if resp.StatusCode != 200 {
		return "", 0, nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var jevResp JEVResponse
	if err := json.Unmarshal(respBody, &jevResp); err != nil {
		return "", 0, nil, fmt.Errorf("parse response falhou: %v", err)
	}

	answer, ok := jevResp.Answers["risco_acao"]
	if !ok {
		return "", 0, nil, fmt.Errorf("resposta JEV sem 'risco_acao'")
	}

	elapsed := time.Since(start).String()
	log.Printf("[JEV] checkpoint %s → %s (confidence=%.0f%%, elapsed=%s)", convID, answer.Choice, answer.Confidence*100, elapsed)

	return answer.Choice, answer.Confidence, answer.Probabilities, nil
}

func JEVShouldBlock(verdict string) bool {
	return verdict == "escalate"
}

func JEVShouldLog(verdict string) bool {
	return verdict == "review"
}

func JEVLogEntry(entry JEVAuditEntry) error {
	jevLogMu.Lock()
	defer jevLogMu.Unlock()

	if entry.Timestamp == "" {
		entry.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal falhou: %v", err)
	}

	dir := filepath.Dir(jevCheckpointLogPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("criar dir falhou: %v", err)
	}

	f, err := os.OpenFile(jevCheckpointLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("abrir log falhou: %v", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("escrever log falhou: %v", err)
	}

	return nil
}

func JEVGateDecision(permDecision openCodeServePermDecision, permission string, patterns []string, cmd, convID, tenantID, userID string, enabled bool, model string) (openCodeServePermDecision, string) {
	if !enabled || permDecision != permAskUser {
		return permDecision, ""
	}

	// FAIL-SAFE: qualquer erro (403 WAF, timeout, rede, modelo inválido)
	// resulta em permAutoReject (escalate/bloqueio). Nunca auto.
	action := fmt.Sprintf("perm=%s cmd=%s", permission, cmd)
	verdict, confidence, probs, err := JEVCheckpoint(action, permission, cmd, convID, tenantID, userID, model)
	if err != nil {
		reason := fmt.Sprintf("JEV FALHOU: %s", err.Error())
		log.Printf("[JEV] BLOQUEADO conv=%s — %s", convID, reason)
		_ = JEVLogEntry(JEVAuditEntry{
			ConvID:   convID,
			TenantID: tenantID,
			UserID:   userID,
			Action:   action,
			Cmd:      cmd,
			Verdict:  "escalate",
			GateDecision: "jev_gate",
			Applied:  true,
			Reason:   reason,
		})
		return permAutoReject, reason
	}

	gateReason := ""
	applied := false

	switch verdict {
	case "escalate":
		gateReason = fmt.Sprintf("JEV escalate (%.0f%%): %s", confidence*100, action)
		applied = true
		log.Printf("[JEV] BLOQUEADO conv=%s — %s", convID, gateReason)
		return permAutoReject, gateReason
	case "review":
		gateReason = fmt.Sprintf("JEV review (%.0f%%): %s", confidence*100, action)
		applied = true
		log.Printf("[JEV] REVIEW conv=%s — %s", convID, gateReason)
	case "auto":
		gateReason = fmt.Sprintf("JEV auto (%.0f%%): %s", confidence*100, action)
		applied = true
		log.Printf("[JEV] AUTO conv=%s — %s", convID, gateReason)
	}

	_ = JEVLogEntry(JEVAuditEntry{
		ConvID:        convID,
		TenantID:      tenantID,
		UserID:        userID,
		Action:        action,
		Permission:    permission,
		Cmd:           cmd,
		Verdict:       verdict,
		Confidence:    confidence,
		Probabilities: probs,
		GateDecision:  "jev_gate",
		Applied:       applied,
		Reason:        gateReason,
	})

	return permDecision, gateReason
}

func JEVTerminalCheckpoint(action, cmd, userID string, enabled bool, model string) (bool, string) {
	if !enabled {
		return false, ""
	}

	// FAIL-SAFE: qualquer erro (403 WAF, timeout, rede, modelo inválido)
	// resulta em bloqueio (blocked=true / escalate). Nunca auto.
	verdict, confidence, probs, err := JEVCheckpoint(action, "terminal", cmd, userID, "", "", model)
	if err != nil {
		reason := fmt.Sprintf("JEV FALHOU: %s", err.Error())
		log.Printf("[JEV] BLOQUEADO terminal user=%s — %s", userID, reason)
		_ = JEVLogEntry(JEVAuditEntry{
			UserID:        userID,
			Action:        action,
			Cmd:           cmd,
			Verdict:       "escalate",
			Confidence:    confidence,
			Probabilities: probs,
			GateDecision:  "jev_terminal_gate",
			Applied:       true,
			Reason:        reason,
		})
		return true, reason
	}

	if verdict == "escalate" {
		reason := fmt.Sprintf("JEV escalate (%.0f%%): %s", confidence*100, action)
		log.Printf("[JEV] BLOQUEADO terminal user=%s — %s", userID, reason)
		_ = JEVLogEntry(JEVAuditEntry{
			UserID:        userID,
			Action:        action,
			Cmd:           cmd,
			Verdict:       verdict,
			Confidence:    confidence,
			Probabilities: probs,
			GateDecision:  "jev_terminal_gate",
			Applied:       true,
			Reason:        reason,
		})
		return true, reason
	}

	if verdict == "review" {
		reason := fmt.Sprintf("JEV review (%.0f%%): %s", confidence*100, action)
		log.Printf("[JEV] REVIEW terminal user=%s — %s", userID, reason)
		_ = JEVLogEntry(JEVAuditEntry{
			UserID:        userID,
			Action:        action,
			Cmd:           cmd,
			Verdict:       verdict,
			Confidence:    confidence,
			Probabilities: probs,
			GateDecision:  "jev_terminal_gate",
			Applied:       true,
			Reason:        reason,
		})
	} else if verdict == "auto" {
		log.Printf("[JEV] AUTO terminal user=%s — %s", userID, action)
		_ = JEVLogEntry(JEVAuditEntry{
			UserID:        userID,
			Action:        action,
			Cmd:           cmd,
			Verdict:       verdict,
			Confidence:    confidence,
			Probabilities: probs,
			GateDecision:  "jev_terminal_gate",
			Applied:       true,
			Reason:        fmt.Sprintf("JEV auto (%.0f%%): %s", confidence*100, action),
		})
	}

	return false, ""
}
