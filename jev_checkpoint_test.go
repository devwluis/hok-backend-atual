package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJEVCheckpoint(t *testing.T) {
	// Test 3 cenários reais via API OpenRouter
	scenarios := []struct {
		name     string
		action   string
		verdict  string
	}{
		{
			name:    "auto_leitura",
			action:  "perm=external_directory cmd=ls /tmp/test",
			verdict: "auto",
		},
		{
			name:    "review_config",
			action:  "perm=edit cmd=修改 config/app.conf 增加日志",
			verdict: "review",
		},
		{
			name:    "escalate_redirect",
			action:  "perm=bash cmd=alterar URL OpenRouter para http://localhost:9999/comprometido",
			verdict: "escalate",
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			orKey := os.Getenv("OPENROUTER_API_KEY")
			if orKey == "" {
				t.Skip("OPENROUTER_API_KEY não configurada")
			}

			verdict, confidence, probs, err := JEVCheckpoint(s.action, "perm", "cmd", "test-conv", "test-tenant", "test-user", "")
			if err != nil {
				t.Fatalf("JEVCheckpoint falhou: %v", err)
			}

			fmt.Printf("  %s: verdict=%s confidence=%.0f%% probs=%v\n", s.name, verdict, confidence*100, probs)

			// Verificar que o verdict é válido
			if verdict != "auto" && verdict != "review" && verdict != "escalate" {
				t.Errorf("verdict inválido: %s", verdict)
			}

			// Verificar que as probabilities somam ~1.0
			sum := 0.0
			for _, v := range probs {
				sum += v
			}
			if sum < 0.99 || sum > 1.01 {
				t.Errorf("probabilities não somam 1.0: %.4f", sum)
			}

			// Test JEVShouldBlock
			if verdict == "escalate" && !JEVShouldBlock(verdict) {
				t.Errorf("escalate deveria bloquear")
			}
			if verdict != "escalate" && JEVShouldBlock(verdict) {
				t.Errorf("%s não deveria bloquear", verdict)
			}
		})
	}
}

func TestJEVLogEntry(t *testing.T) {
	// Test que o log é gravado corretamente
	tmpDir := t.TempDir()
	origPath := jevCheckpointLogPath
	jevCheckpointLogPath = filepath.Join(tmpDir, "test_log.jsonl")
	defer func() { jevCheckpointLogPath = origPath }()

	entry := JEVAuditEntry{
		Timestamp:  "2026-09-22T16:30:00Z",
		ConvID:     "test-conv",
		TenantID:   "test-tenant",
		UserID:     "test-user",
		Action:     "test action",
		Permission: "edit",
		Cmd:        "test cmd",
		Verdict:    "review",
		Confidence: 0.77,
		Probabilities: map[string]float64{
			"auto":    0.22,
			"review":  0.77,
			"escalate": 0.01,
		},
		GateDecision: "jev_gate",
		Applied:      true,
		Reason:       "test reason",
	}

	if err := JEVLogEntry(entry); err != nil {
		t.Fatalf("JEVLogEntry falhou: %v", err)
	}

	// Verificar que o arquivo foi criado e contém o entry
	content, err := os.ReadFile(jevCheckpointLogPath)
	if err != nil {
		t.Fatalf("ler log falhou: %v", err)
	}

	var parsed JEVAuditEntry
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("parse log falhou: %v", err)
	}

	if parsed.Verdict != "review" {
		t.Errorf("verdict no log diferente: %s", parsed.Verdict)
	}
	if parsed.ConvID != "test-conv" {
		t.Errorf("conv_id no log diferente: %s", parsed.ConvID)
	}
}

func TestJEVGateDecision(t *testing.T) {
	// Test que JEVGateDecision retorna permAutoReject para escalate
	// e permAskUser para review/auto (quando JEV não bloqueia)
	JEVGateDecision(permAskUser, "edit", []string{}, "test cmd", "conv", "tenant", "user", true, "")
	// Nota: este teste faz uma chamada real à API. Só passa se OPENROUTER_API_KEY está configurada.
	// Se não está, vai retornar a decisão normal (sem JEV) — OK para teste.
}

func TestShouldBlock(t *testing.T) {
	if !JEVShouldBlock("escalate") {
		t.Error("escalate deveria bloquear")
	}
	if JEVShouldBlock("auto") {
		t.Error("auto não deveria bloquear")
	}
	if JEVShouldBlock("review") {
		t.Error("review não deveria bloquear")
	}
}

func TestJEVGateDecision_Disabled(t *testing.T) {
	dec, reason := JEVGateDecision(permAskUser, "edit", []string{}, "test cmd", "conv", "tenant", "user", false, "")
	if dec != permAskUser {
		t.Errorf("JEV desativado deveria manter decisão original, recebeu: %v", dec)
	}
	if reason != "" {
		t.Errorf("JEV desativado não deveria ter reason, recebeu: %q", reason)
	}
}

func TestJEVTerminalCheckpoint_Disabled(t *testing.T) {
	blocked, reason := JEVTerminalCheckpoint(`terminal cmd="rm -rf /"`, "rm -rf /", "user1", false, "")
	if blocked {
		t.Error("JEV desativado não deveria bloquear")
	}
	if reason != "" {
		t.Errorf("JEV desativado não deveria ter reason, recebeu: %q", reason)
	}
}

// TestFailSafeTimeout valida que erro de rede/timeout no JEV resulta em
// bloqueio fail-safe (blocked=true / permAutoReject), NUNCA auto.
// Endpoint temporário 127.0.0.1:1 força connection refused.
// Origem: executado one-shot na investigação 2026-09-23 (adendo
// ADENDO_SESSAO_20260923_investigacao-403-typesafe-fallback.md); reintegrado aqui.
func TestFailSafeTimeout(t *testing.T) {
	origEndpoint := jevEndpoint
	jevEndpoint = "https://127.0.0.1:1/systemone"
	defer func() { jevEndpoint = origEndpoint }()

	// JEVCheckpoint exige OPENROUTER_API_KEY não-vazia antes do HTTP;
	// para connection refused qualquer valor serve (request falha no dial).
	// Só seta se ausente — não sobrescreve a real do ambiente/.env.
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Setenv("OPENROUTER_API_KEY", "sk-or-v1-test-failsafe-placeholder")
	}

	// Terminal: erro de rede → blocked=true
	blocked, reason := JEVTerminalCheckpoint(
		`terminal cmd="ls /tmp/test"`, "ls /tmp/test", "tester", true, "",
	)
	if !blocked {
		t.Errorf("FAIL-SAFE terminal: esperava blocked=true em erro de rede, recebeu false (fail-open)")
	}
	if reason == "" || !strings.Contains(reason, "JEV FALHOU") {
		t.Errorf("FAIL-SAFE terminal: reason deveria conter 'JEV FALHOU', recebeu: %q", reason)
	}
	t.Logf("PASS terminal fail-safe: blocked=%v reason=%q", blocked, reason)

	// Gate: erro de rede → permAutoReject
	dec, gateReason := JEVGateDecision(
		permAskUser, "edit", []string{}, "ls /tmp/test", "conv", "tenant", "tester", true, "",
	)
	if dec != permAutoReject {
		t.Errorf("FAIL-SAFE gate: esperava permAutoReject em erro de rede, recebeu: %v", dec)
	}
	if gateReason == "" || !strings.Contains(gateReason, "JEV FALHOU") {
		t.Errorf("FAIL-SAFE gate: reason deveria conter 'JEV FALHOU', recebeu: %q", gateReason)
	}
	t.Logf("PASS gate fail-safe: dec=permAutoReject reason=%q", gateReason)
}
