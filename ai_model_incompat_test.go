package main

import (
	"os"
	"strings"
	"testing"
)

// TestLogModelIncompatibility valida o registro de combinacao modelo+motor
// incompativel na tabela logs (auditoria para acompanhamento).
func TestLogModelIncompatibility(t *testing.T) {
	if os.Getenv("HOK_TOKEN") == "" {
		t.Skip("HOK_TOKEN nao definido")
	}
	tmpDB := t.TempDir() + "/test_incompat.db"
	orig := DB_PATH
	DB_PATH = tmpDB
	t.Cleanup(func() { DB_PATH = orig })
	initDB()
	sqliteExec(`CREATE TABLE IF NOT EXISTS logs (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, event TEXT NOT NULL, level TEXT DEFAULT 'INFO', source TEXT DEFAULT 'hokma_v21');`)

	logModelIncompatibility("n8n_agent_loop", "modelo-x/sem-tool", assertErr("falha tool-use"))

	rows := sqliteExecParams(
		`SELECT count(*) FROM logs WHERE event LIKE 'model_incompat|%' AND source='n8n_agent_loop' AND level='WARN';`,
	)
	if !strings.Contains(rows, "1") {
		t.Fatalf("log de incompatibilidade nao registrado: %q", rows)
	}
}

type strErr string

func assertErr(s string) error { return strErr(s) }
func (e strErr) Error() string { return string(e) }
