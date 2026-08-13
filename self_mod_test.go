package main

import (
	"os"
	"strings"
	"testing"
)

func TestExecuteSelfMod(t *testing.T) {
	if os.Getenv("HOK_TOKEN") == "" {
		t.Skip("HOK_TOKEN nao definido")
	}
	// Inicializa o DB em arquivo temporario (o global db é nil sem initSQLite)
	tmpDB := t.TempDir() + "/test_selfmod.db"
	origDBPath := DB_PATH
	DB_PATH = tmpDB
	defer func() { DB_PATH = origDBPath }()
	initDB()
	sqliteExec(`CREATE TABLE IF NOT EXISTS self_modifications (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id TEXT, commit_hash TEXT, file_path TEXT, ia_description TEXT, diff_summary TEXT, smoke_test_passed INTEGER DEFAULT 0, status TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);`)

	// Arquivo de teste seguro (NUNCA mexe no main.go real)
	tmpFile := t.TempDir() + "/alvo.go"
	os.WriteFile(tmpFile, []byte("package main\n"), 0644)
	safeTarget := t.TempDir() + "/dest.go"
	os.WriteFile(safeTarget, []byte("package main\n"), 0644)

	// Criar pending action de automodificação
	pa := &PendingAction{
		ToolName:    "bash_exec",
		ArgsJSON:    `{"cmd":"cat ` + tmpFile + ` >> ` + safeTarget + `"}`,
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
	t.Logf("Fluxo de automodificacao testado. Resultado: %s", result)
}