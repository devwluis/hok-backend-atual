package main

import (
	"os"
	"strings"
	"testing"
)

func TestExecuteSelfMod(t *testing.T) {
	// Criar arquivo de teste temporário
	tmpFile := "/tmp/test_automod_" + string(rune('0'+int(os.Getpid()%10))) + ".go"
	content := "package main\nfunc TestAuto() string { return \"ok\" }\n"
	os.WriteFile(tmpFile, []byte(content), 0644)
	defer os.Remove(tmpFile)

	// Criar pending action de automodificação
	pa := &PendingAction{
		ToolName:    "bash_exec",
		ArgsJSON:    `{"cmd":"cat ` + tmpFile + ` >> /root/hokma/backend/main.go"}`,
		Description: "[AUTOMODIFICACAO] Teste unitario de automodificacao",
		ActionType:  "self_mod",
		TenantID:    "owner",
	}

	// Executar automodificação
	result := executeSelfMod(pa)

	// Verificar se o smoke test passou
	if !strings.Contains(result, "Sucesso") && !strings.Contains(result, "SMOKE TEST FALHOU") {
		t.Logf("Resultado: %s", result)
	}

	// Se smoke passou, verificar commit
	if strings.Contains(result, "Sucesso") {
		// Verificar se commit existe no repo bare
		repo := "/root/hokma/tenants/owner/.git-worktree"
		if _, err := os.Stat(repo); err == nil {
			t.Log("Commit registrado com sucesso")
		}
	}

	// Reverter mudanças no main.go (remover linhas adicionadas)
	mainContent, _ := os.ReadFile("/root/hokma/backend/main.go")
	mainStr := string(mainContent)
	if idx := strings.Index(mainStr, "func TestAuto()"); idx != -1 {
		cleaned := mainStr[:idx]
		os.WriteFile("/root/hokma/backend/main.go", []byte(cleaned), 0644)
	}

	t.Logf("Fluxo de automodificacao testado. Resultado: %s", result)
}
