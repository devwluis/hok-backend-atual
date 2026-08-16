package main

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func resetPendingState() {
	pendingActionMu.Lock()
	pendingActionMap = map[string]*PendingAction{}
	pendingActionMu.Unlock()
	pendingExecMu.Lock()
	pendingExecCommands = map[string]string{}
	pendingExecMu.Unlock()
}

// Bug critico: aprovar digitando "Ok" executava o prompt natural como bash.
// O caminho claude_code deve SEMPRE ir pro CLI aprovado, nunca pro bash.
func TestResolveClaudeCodePendingActionFailClosed(t *testing.T) {
	resetPendingState()

	pa := &PendingAction{
		ID:          "cc_failclosed_test",
		ToolName:    "claude_code",
		ArgsJSON:    `{"prompt":""}`, // prompt perdido (ex.: args corrompidos)
		Description: "Execucao de comando bash: rm -rf /tmp/x",
		CreatedAt:   time.Now(),
	}
	result := resolveClaudeCodePendingAction(pa)
	if !strings.Contains(result, "nao esta mais disponivel") {
		t.Fatalf("esperava fail-closed, veio: %s", result)
	}
	if strings.Contains(result, "rm") || strings.Contains(result, "command not found") {
		t.Fatalf("fail-closed executou algo: %s", result)
	}

	// ArgsJSON invalido (nao-JSON) tambem deve ser fail-closed
	pa.ArgsJSON = "Ok perfeito agora uma duvida"
	result = resolveClaudeCodePendingAction(pa)
	if !strings.Contains(result, "nao esta mais disponivel") {
		t.Fatalf("esperava fail-closed para args invalidos, veio: %s", result)
	}
}

// Fallback cmd=action.Description removido: sem staging, fs_exec NUNCA
// roda a descricao como bash — falha fechado.
func TestResolveFsExecPendingActionFailClosed(t *testing.T) {
	resetPendingState()

	pa := &PendingAction{
		ID:          "fs_missing_test",
		ToolName:    "fs_exec",
		ArgsJSON:    "",
		Description: "Execucao de comando bash: echo perigoso-fallback",
		CreatedAt:   time.Now(),
	}
	result := resolveFsExecPendingAction(pa)
	if !strings.Contains(result, "Refaça o pedido") {
		t.Fatalf("esperava fail-closed, veio: %s", result)
	}
	if strings.Contains(result, "perigoso-fallback") {
		t.Fatalf("fallback perigoso executou a descricao: %s", result)
	}
}

// Com staging perdido na memoria (restart) mas ArgsJSON persistido,
// o comando original ainda executa — sem fallback de descricao.
func TestResolveFsExecPendingActionUsaArgsJSON(t *testing.T) {
	resetPendingState()

	pa := &PendingAction{
		ID:          "fs_persisted_test",
		ToolName:    "fs_exec",
		ArgsJSON:    `{"cmd":"echo hokma-staging-ok-12345"}`,
		Description: "Execucao de comando bash: echo hokma-staging-ok-12345",
		CreatedAt:   time.Now(),
	}
	result := resolveFsExecPendingAction(pa)
	if !strings.Contains(result, "hokma-staging-ok-12345") {
		t.Fatalf("staging via ArgsJSON nao executou: %s", result)
	}
}

// Consumo atomico: duas aprovacoes quase simultaneas consomem a MESMA
// acao apenas UMA vez (sem dupla execucao TOCTOU).
func TestConsumePendingActionAtomic(t *testing.T) {
	resetPendingState()

	setPendingAction("conv_atomic", "owner", "", "bash_exec", `{"cmd":"echo x"}`, "Teste atomico")
	// nota: setPendingAction normaliza userID "" -> "anonymous"

	var wg sync.WaitGroup
	got := make(chan *PendingAction, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got <- consumePendingAction("conv_atomic", "owner", "anonymous")
		}()
	}
	wg.Wait()
	close(got)

	var consumed, nilCount int
	for pa := range got {
		if pa != nil {
			consumed++
		} else {
			nilCount++
		}
	}
	if consumed != 1 || nilCount != 1 {
		t.Fatalf("consumo atomico falhou: 1 acao consumida, recebidos consumed=%d nil=%d", consumed, nilCount)
	}
	if getPendingAction("conv_atomic", "owner", "anonymous") != nil {
		t.Fatal("acao ainda pendente apos consumo atomico")
	}
}

// isApprovalText estrito: "ok" exato aprova; "ok, mas..." NAO.
func TestIsApprovalTextStrict(t *testing.T) {
	approvals := []string{"Ok", "ok", "OK!", "sim", "sim.", "pode", "confirma", "aprova", "manda", "vai", "ok?", "sim!"}
	for _, m := range approvals {
		if !isApprovalText(m) {
			t.Errorf("esperava aprovacao para %q", m)
		}
	}
	nonApprovals := []string{
		"ok, mas antes me explica X",
		"ok perfeito agora uma dúvida você consegue testar o botão debug?",
		"sim, executar o que?",
		"sim por favor me explique",
		"pode me explicar melhor?",
		"vai com calma",
		"não",
		"",
		"okey",
		"confirmar",
	}
	for _, m := range nonApprovals {
		if isApprovalText(m) {
			t.Errorf("NAO deveria aprovar %q", m)
		}
	}
}

// Duas acoes criadas no mesmo segundo NAO podem colidir de ID
// (antes: timestamp com granularidade de 1s sobrescrevia o staging).
func TestSetPendingActionUniqueID(t *testing.T) {
	resetPendingState()

	a := setPendingAction("conv_uniq", "owner", "anonymous", "bash_exec", `{"cmd":"echo a"}`, "A")
	b := setPendingAction("conv_uniq", "owner", "anonymous", "bash_exec", `{"cmd":"echo b"}`, "B")
	if a.ID == b.ID {
		t.Fatalf("IDs colidiram: %s", a.ID)
	}
	if a.TenantID != "owner" || b.TenantID != "owner" {
		t.Fatal("tenant perdido")
	}
	// cleanup do DB real (savePendingAction persiste em memory.db)
	clearPendingAction("conv_uniq", "owner", "anonymous")
}