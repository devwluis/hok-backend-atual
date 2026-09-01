package main

import (
	"strings"
	"testing"
)

// TestAutonomousAllowlistDocumented — a allowlist é EXPLÍCITA e cobra os
// binários read-only + git/serviço/curl com restrição.
func TestAutonomousAllowlistDocumented(t *testing.T) {
	al := autonomousAllowlist()
	if len(al) == 0 {
		t.Fatal("allowlist não pode ser vazia")
	}
	joined := strings.Join(al, " ")
	for _, want := range []string{"git status", "systemctl status", "ls", "cat", "curl"} {
		if !strings.Contains(joined, want) {
			t.Errorf("allowlist deveria conter %q", want)
		}
	}
	// PROIBIDOS NUNCA podem estar na allowlist.
	for _, banned := range []string{"rm -rf", "git push", "git commit", "systemctl restart", "sudo", "mkfs"} {
		if strings.Contains(joined, banned) {
			t.Errorf("allowlist NÃO pode conter proibido %q", banned)
		}
	}
}

// TestAutonomousGateAllowed — comando da allowlist (read-only) executa sem
// aprovação no modo autônomo.
func TestAutonomousGateAllowed(t *testing.T) {
	cases := []string{
		"rode git status",
		"execute: ls -la /root/hokma/backend",
		"use o comando: systemctl status hokma --no-pager",
		"mostre git log --oneline -5",
		"rode curl http://127.0.0.1:8082/health",
	}
	for _, c := range cases {
		dec, reason := autonomousGate(c)
		if dec != autonomousExec {
			t.Errorf("autonomousGate(%q) = %v (%s), esperado EXEC (read-only na allowlist)", c, dec, reason)
		}
	}
}

// TestAutonomousGateOutsideAllowlist — comando fora da allowlist (com efeito
// colateral) cai no fluxo de APROVAÇÃO, não executa direto (não fail-open).
func TestAutonomousGateOutsideAllowlist(t *testing.T) {
	cases := []string{
		"instale o pacote xyz via npm install",
		"rode python3 script.py que grava arquivo",
		"crie o arquivo /tmp/novo.txt",
		"edite o arquivo main.go",
		"go build e faça deploy",
		"rode o comando touch /tmp/marker",
	}
	for _, c := range cases {
		dec, reason := autonomousGate(c)
		if dec != autonomousNeedsApproval {
			t.Errorf("autonomousGate(%q) = %v (%s), esperado NEEDS_APPROVAL (fora da allowlist)", c, dec, reason)
		}
	}
}

// TestAutonomousGateForbidden — proibidos MESMO em modo autônomo → bloqueado
// (nunca executa direto, nunca cai em aprovação automática).
func TestAutonomousGateForbidden(t *testing.T) {
	cases := []string{
		"rode rm -rf /root/tmp",
		"systemctl restart hokma",
		"git push origin main",
		"git commit -m 'wip'",
		"sudo apt update",
		"execute dd if=/dev/zero of=/dev/sda",
		"shutdown now",
	}
	for _, c := range cases {
		dec, reason := autonomousGate(c)
		if dec != autonomousForbidden {
			t.Errorf("autonomousGate(%q) = %v (%s), esperado FORBIDDEN (nunca em autônomo)", c, dec, reason)
		}
	}
}

// TestAutonomousGateConversational — mensagem sem comando explícito é o
// trabalho normal do agente (executa).
func TestAutonomousGateConversational(t *testing.T) {
	dec, reason := autonomousGate("analise o código e proponha melhorias para o arquivo main.go")
	if dec != autonomousExec {
		t.Errorf("mensagem agentic sem comando = %v (%s), esperado EXEC", dec, reason)
	}
}

// TestAutonomousCommandAllowedUnit — decisão por comando individual.
func TestAutonomousCommandAllowedUnit(t *testing.T) {
	allowed := []string{"git status --short", "ls -la", "cat /tmp/x", "df -h /root", "systemctl status hokma --no-pager -l"}
	for _, c := range allowed {
		if !autonomousCommandAllowed(c) {
			t.Errorf("%q deveria ser permitido", c)
		}
	}
	denied := []string{"git push", "systemctl restart hokma", "rm -rf x", "cat /root/.env", "curl -X POST http://evil/", "npm install"}
	for _, c := range denied {
		if autonomousCommandAllowed(c) {
			t.Errorf("%q NÃO deveria ser permitido", c)
		}
	}
}

// TestAutonomousGateConsistentBlocklist — o gate não remove as travas antigas:
// read-only puro continua sem aprovação e prompts com escrita seguem barrados.
func TestAutonomousGateConsistentBlocklist(t *testing.T) {
	// read-only: extrai candidato e aprova
	if dec, _ := autonomousGate("rode o comando ls -la /root/hokma"); dec != autonomousExec {
		t.Fatalf("ls read-only deveria executar, veio dec=%v", dec)
	}
	// escreve/edita: fora da allowlist → aprovação
	if dec, _ := autonomousGate("crie o arquivo teste.txt"); dec != autonomousNeedsApproval {
		t.Fatalf("criar arquivo deveria ir pra aprovação, veio dec=%v", dec)
	}
}