package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// TestAutonomousClaudeArgs — GATE AUTÔNOMO: em modo autônomo o CLI claude
// usa o modo nativo "auto" e NUNCA --dangerously-skip-permissions/plan.
func TestAutonomousClaudeArgs(t *testing.T) {
	auto := strings.Join(claudeCLIArgs("crie o arquivo x", false, false, true), " ")
	if !strings.Contains(auto, "--permission-mode") || !strings.Contains(auto, "auto") {
		t.Fatalf("autônomo deveria conter --permission-mode auto, veio: %v", auto)
	}
	if strings.Contains(auto, "--dangerously-skip-permissions") {
		t.Fatalf("autônomo NUNCA pode ter --dangerously-skip-permissions: %v", auto)
	}
	if strings.Contains(auto, "--permission-mode plan") {
		t.Fatalf("autônomo NUNCA pode ser plan: %v", auto)
	}
}

// TestAutonomousHermesArgs — GATE AUTÔNOMO (decisão 3): container efêmero
// com o volume real rw — SEM --read-only, SEM --rm, SEM --tmpfs /opt/data,
// com o mount rw do volume e o -z (yolo) presente para executar.
func TestAutonomousHermesArgs(t *testing.T) {
	args := hermesAutonomousArgs("model-x", "prompt", "hok_hermes_data")
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--read-only") {
		t.Fatalf("autônomo NÃO pode ter --read-only: %v", args)
	}
	if strings.Contains(joined, "--rm") {
		t.Fatalf("autônomo NÃO pode ter --rm (decisão 3): %v", args)
	}
	if !strings.Contains(joined, "type=volume,src=hok_hermes_data,dst=/opt/data") {
		t.Fatalf("autônomo deve montar o volume real rw: %v", args)
	}
	if strings.Contains(joined, "dst=/opt/data,readonly") {
		t.Fatalf("o mount rw não pode ser readonly: %v", args)
	}
	if !strings.Contains(joined, "dst=/opt/data-ro,readonly") {
		t.Fatalf("o mount ro de leitura (config) deve continuar: %v", args)
	}
	if !strings.Contains(joined, "-z ") {
		t.Fatalf("autônomo deve ter o -z (yolo) para executar: %v", args)
	}
}

// TestAutonomousCBRepeat — circuit breaker: 3 ações idênticas → bloqueia.
func TestAutonomousCBRepeat(t *testing.T) {
	cb := &autonomousCB{startTs: time.Now()}
	now := time.Now()
	h := autonomousActionHash("mesma acao")
	if blocked, _ := cb.decide(h, now); blocked {
		t.Fatal("1a ação não deveria bloquear")
	}
	if blocked, _ := cb.decide(h, now); blocked {
		t.Fatal("2a ação igual não deveria bloquear")
	}
	blocked, reason := cb.decide(h, now)
	if !blocked {
		t.Fatal("3a ação idêntica DEVERIA bloquear (circuit breaker)")
	}
	if !strings.Contains(reason, "repetida") {
		t.Fatalf("motivo deveria citar repetição, veio: %q", reason)
	}
	// ação diferente reseta a contagem
	if blocked, _ := cb.decide(autonomousActionHash("outra acao"), now); blocked {
		t.Fatal("ação diferente não deveria bloquear")
	}
}

// TestAutonomousCBErrors — circuit breaker: 3 erros consecutivos → bloqueia.
func TestAutonomousCBErrors(t *testing.T) {
	cb := &autonomousCB{startTs: time.Now()}
	now := time.Now()
	for i := 0; i < 3; i++ {
		cb.recordResult(errors.New("erro"))
	}
	blocked, reason := cb.decide(autonomousActionHash("acao"), now)
	if !blocked {
		t.Fatal("3 erros consecutivos DEVERIAM bloquear")
	}
	if !strings.Contains(reason, "erros consecutivos") {
		t.Fatalf("motivo deveria citar erros consecutivos, veio: %q", reason)
	}
	// sucesso zera a contagem de erros
	cb.recordResult(nil)
	if blocked, _ := cb.decide(autonomousActionHash("outra"), now); blocked {
		t.Fatal("após sucesso, não deveria bloquear")
	}
}

// TestAutonomousCBTime — circuit breaker: tempo total > 10 min → bloqueia.
func TestAutonomousCBTime(t *testing.T) {
	cb := &autonomousCB{startTs: time.Now().Add(-autonomousMaxDuration - time.Minute)}
	blocked, reason := cb.decide(autonomousActionHash("acao"), time.Now())
	if !blocked {
		t.Fatal("tempo excedido DEVERIA bloquear")
	}
	if !strings.Contains(reason, "10 min") {
		t.Fatalf("motivo deveria citar o tempo, veio: %q", reason)
	}
}

// TestAutonomousNormalizeBudget — decisão 2: default 5 em autonomous; 0 nos
// demais modos; body pode especificar.
func TestAutonomousNormalizeBudget(t *testing.T) {
	if b := normalizeAutonomousBudget("autonomous", nil); b != autonomousDefaultBudget {
		t.Fatalf("autonomous sem body deveria ser %d, veio %d", autonomousDefaultBudget, b)
	}
	v := 3
	if b := normalizeAutonomousBudget("autonomous", &v); b != 3 {
		t.Fatalf("autonomous com body=3 deveria ser 3, veio %d", b)
	}
	z := 0
	if b := normalizeAutonomousBudget("autonomous", &z); b != autonomousDefaultBudget {
		t.Fatalf("body=0 deve cair no default %d, veio %d", autonomousDefaultBudget, b)
	}
	if b := normalizeAutonomousBudget("build", nil); b != 0 {
		t.Fatalf("build deveria ter budget 0, veio %d", b)
	}
	if b := normalizeAutonomousBudget("plan", nil); b != 0 {
		t.Fatalf("plan deveria ter budget 0, veio %d", b)
	}
}

// TestAutonomousBlocklistPrompt — a blocklist Hokma barra prompts
// destrutivos ANTES de qualquer execução (decisão 4: aviso direto).
func TestAutonomousBlocklistPrompt(t *testing.T) {
	for _, destructive := range []string{
		"rm -rf /",
		"delete from auth_routes",
		"sudo systemctl stop hokma",
		"git push origin main",
	} {
		if !promptNeedsApproval(destructive) {
			t.Fatalf("prompt destrutivo deveria precisar aprovação (blocklist): %q", destructive)
		}
	}
	for _, safe := range []string{
		"liste os arquivos do diretório",
		"explique o que este código faz",
	} {
		if promptNeedsApproval(safe) {
			t.Fatalf("prompt seguro não deveria precisar aprovação: %q", safe)
		}
	}
}

// TestAutonomousServeMode — espelho do plan: o modo autônomo é marcado por
// sessão e o watcher o consulta.
func TestAutonomousServeMode(t *testing.T) {
	openCodeServeSetAutonomousMode("ses_auto_test", true)
	if !openCodeServeIsAutonomousMode("ses_auto_test") {
		t.Fatal("modo autônomo deveria estar ativo")
	}
	openCodeServeSetAutonomousMode("ses_auto_test", false)
	if openCodeServeIsAutonomousMode("ses_auto_test") {
		t.Fatal("modo autônomo deveria ter sido desativado")
	}
	// modos mutuamente exclusivos (plan liga, autônomo desliga e vice-versa)
	openCodeServeSetAutonomousMode("ses_auto_test", true)
	if openCodeServeIsPlanMode("ses_auto_test") {
		t.Fatal("plan e autônomo não podem coexistir")
	}
	openCodeServeSetAutonomousMode("ses_auto_test", false)
}