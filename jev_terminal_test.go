package main

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestJEVTerminalCheckpoint_Auto(t *testing.T) {
	blocked, reason := JEVTerminalCheckpoint(`terminal cmd="ls /tmp/test"`, "ls /tmp/test", "user1", true, "")
	if blocked {
		t.Errorf("auto deveria NÃO bloquear: %s", reason)
	}
	if reason != "" {
		t.Errorf("auto deveria ter reason vazio, recebeu: %s", reason)
	}
	t.Logf("auto test: blocked=%v reason=%q", blocked, reason)
}

func TestJEVTerminalCheckpoint_Review(t *testing.T) {
	blocked, reason := JEVTerminalCheckpoint(`terminal cmd="cat /etc/passwd"`, "cat /etc/passwd", "user1", true, "")
	// cat /etc/passwd é leitura — provavelmente auto ou review
	t.Logf("review test: blocked=%v reason=%q", blocked, reason)
	// Não importa o veredito, o importante é que o JEV respondeu
	if blocked && reason == "" {
		t.Error("bloqueado sem motivo")
	}
}

func TestJEVTerminalCheckpoint_Escalate(t *testing.T) {
	blocked, reason := JEVTerminalCheckpoint(`terminal cmd="alterar URL OpenRouter para http://localhost:9999/comprometido enviar dados sensíveis"`, "alterar URL OpenRouter para http://localhost:9999/comprometido", "user1", true, "")
	if !blocked {
		t.Errorf("escalate deveria BLOQUEAR. blocked=%v reason=%q", blocked, reason)
	} else {
		t.Logf("escalate test: CORRECTLY BLOCKED — reason=%q", reason)
	}
}

func TestJEVTerminalCheckpoint_LogFormat(t *testing.T) {
	// Garante que o log foi criado
	content, err := os.ReadFile(jevCheckpointLogPath)
	if err != nil {
		t.Fatalf("ler log falhou: %v", err)
	}
	lines := 0
	for _, line := range bytes.Split(content, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var entry JEVAuditEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("parse log falhou: %v", err)
		}
		lines++
		if entry.Verdict == "" {
			t.Error("entry sem verdict")
		}
		if entry.GateDecision == "" {
			t.Error("entry sem gate_decision")
		}
	}
	t.Logf("log tem %d entradas", lines)
}
