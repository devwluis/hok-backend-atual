package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanGateClaudeArgs — GATE PLAN: em modo plan o CLI claude deve usar o
// modo nativo (--permission-mode plan) e NUNCA --dangerously-skip-permissions.
func TestPlanGateClaudeArgs(t *testing.T) {
	planArgs := claudeCLIArgs("crie o arquivo x", false, true, false)
	joined := strings.Join(planArgs, " ")
	if !strings.Contains(joined, "--permission-mode") || !strings.Contains(joined, "plan") {
		t.Fatalf("plan deveria conter --permission-mode plan, veio: %v", planArgs)
	}
	if strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Fatalf("plan NUNCA pode ter --dangerously-skip-permissions: %v", planArgs)
	}
	// sem plan: não inclui --permission-mode
	normal := strings.Join(claudeCLIArgs("crie o arquivo x", false, false, false), " ")
	if strings.Contains(normal, "--permission-mode") {
		t.Fatalf("modo normal não deve ter --permission-mode: %v", normal)
	}
	// approved: skip-permissions presente, sem --permission-mode
	approved := strings.Join(claudeCLIArgs("crie o arquivo x", true, false, false), " ")
	if !strings.Contains(approved, "--dangerously-skip-permissions") {
		t.Fatalf("approved deveria ter --dangerously-skip-permissions: %v", approved)
	}
}

// TestPlanGateOpenCodeArgs — GATE PLAN: em modo plan o CLI opencode deve usar
// o agente "plan" (permissões deny) e NUNCA --auto.
func TestPlanGateOpenCodeArgs(t *testing.T) {
	planArgs := opencodeCLIArgs("crie o arquivo x", false, "", "", true)
	joined := strings.Join(planArgs, " ")
	if !strings.Contains(joined, "--agent") || !strings.Contains(joined, "plan") {
		t.Fatalf("plan deveria conter --agent plan, veio: %v", planArgs)
	}
	if strings.Contains(joined, "--auto") {
		t.Fatalf("plan NUNCA pode ter --auto: %v", planArgs)
	}
	// approved (não-plan): --auto presente, sem --agent plan
	approved := strings.Join(opencodeCLIArgs("crie o arquivo x", true, "", "", false), " ")
	if !strings.Contains(approved, "--auto") {
		t.Fatalf("approved deveria ter --auto: %v", approved)
	}
}

// TestPlanGateHermesArgs — GATE PLAN: em modo plan o hermes NÃO recebe --yolo.
func TestPlanGateHermesArgs(t *testing.T) {
	plan := callHermesArgs("model-x", "prompt", false)
	joined := strings.Join(plan, " ")
	if strings.Contains(joined, "--yolo") {
		t.Fatalf("plan NUNCA pode ter --yolo: %v", plan)
	}
	normal := strings.Join(callHermesArgs("model-x", "prompt", true), " ")
	if !strings.Contains(normal, "--yolo") {
		t.Fatalf("modo normal deveria ter --yolo: %v", normal)
	}
}

// TestPlanGateServeDecision — GATE PLAN camada 2: em modo plan, TODA
// permission.asked deve ser negada (reject) — o modelo nunca executa.
func TestPlanGateServeDecision(t *testing.T) {
	openCodeServeSetPlanMode("ses_plan_test", true)
	defer openCodeServeSetPlanMode("ses_plan_test", false)
	if !openCodeServeIsPlanMode("ses_plan_test") {
		t.Fatal("plan mode deveria estar ativo")
	}
	openCodeServeSetPlanMode("ses_plan_test", false)
	if openCodeServeIsPlanMode("ses_plan_test") {
		t.Fatal("plan mode deveria ter sido desativado")
	}
}

// TestHermesVerifyOutput — verificador pós-execução: alegação de criação sem
// arquivo real (alucinação) deve anexar aviso.
func TestHermesVerifyOutput(t *testing.T) {
	fake := filepath.Join(t.TempDir(), "nao-existe.txt")
	out := "Arquivo criado com sucesso: `" + fake + "`"
	verified := hermesVerifyOutput(out)
	if !strings.Contains(verified, "NÃO existem no disco") {
		t.Fatalf("alucinação deveria ser detectada, veio: %q", verified)
	}
	// arquivo real existente: sem aviso
	real := filepath.Join(t.TempDir(), "existe.txt")
	if err := os.WriteFile(real, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	out2 := "Arquivo criado com sucesso: `" + real + "`"
	if v := hermesVerifyOutput(out2); strings.Contains(v, "NÃO existem no disco") {
		t.Fatalf("arquivo real não deveria gerar aviso: %q", v)
	}
	// output sem alegação de criação: intocado
	if v := hermesVerifyOutput("Resposta simples."); v != "Resposta simples." {
		t.Fatalf("output sem alegação deveria ficar intocado: %q", v)
	}
}