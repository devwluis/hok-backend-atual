package main

import (
	"os"
	"strings"
	"testing"
)

func TestIsReadOnlyCommand(t *testing.T) {
	readOnly := []string{
		"ls -la",
		"/bin/ls -la /root",
		"cat /root/hokma/backend/main.go",
		"pwd",
		"whoami",
		"uptime",
		"df -h /root",
		"free -h",
		"uname -r",
		"git log --oneline -5",
		"git status --short",
		"git diff",
		"git show HEAD",
		"grep foo /var/log/syslog",
		"find /tmp -name '*.txt'",
		"systemctl status hokma --no-pager -l",
		"ps aux",
		"top -bn1",
		"netstat -tulpn",
		"ss -tln",
		"curl localhost/health",
		"curl http://127.0.0.1/status",
		"ls -la\npwd", // skill multi-linha toda read-only
	}
	for _, c := range readOnly {
		if !isReadOnlyCommand(c) {
			t.Errorf("isReadOnlyCommand(%q) = false, esperado true", c)
		}
	}

	writeGate := []string{
		"rm arquivo_teste.txt",
		"rm -rf /tmp/x",
		"mv a b",
		"cp a b",
		"mkdir dir",
		"chmod 777 x",
		"chown root x",
		"git commit -m x",
		"git push",
		"git reset --hard HEAD",
		"git add x",
		"systemctl restart hokma",
		"systemctl start nginx",
		"systemctl stop hokma",
		"curl -X POST localhost/x",
		"curl -d 'a=1' localhost/x",
		"curl -F file=@x localhost/up",
		"curl -o /tmp/x localhost/health",
		"curl https://example.com", // host externo
		"curl -X GET localhost/x",  // método explícito não permitido (conservador)
		"find / -delete",
		"find / -exec rm",
		"top",
		"sudo ls /root",
		"binario_customizado foo",
		"",
		"ls; rm -rf /",
		"ls && whoami",
		"whoami > /tmp/x",
		"echo hi",
		"cat /root/.env",
		"cat /root/.ssh/config",
		"grep foo /root/credentials/x",
		"ls -la e depois rm x", // contaminação em texto livre
		"ls -la\nrm x",         // skill multi-linha com escrita
	}
	for _, c := range writeGate {
		if isReadOnlyCommand(c) {
			t.Errorf("isReadOnlyCommand(%q) = true, esperado false (gate)", c)
		}
	}
}

func TestPromptContainsOnlyReadOnlyCommands(t *testing.T) {
	ok := []string{
		"rode ls -la no servidor",
		"mostre o git log",
		"execute systemctl status hokma",
		"curl localhost/health e mostre o resultado",
	}
	for _, p := range ok {
		if !promptContainsOnlyReadOnlyCommands(p) {
			t.Errorf("promptContainsOnlyReadOnlyCommands(%q) = false, esperado true", p)
		}
	}
	gate := []string{
		"rode rm arquivo_teste.txt",
		"execute systemctl restart hokma",
		"faca git commit",
		"oi tudo bem?", // sem comando → fail-safe gate
		"ls -la e depois rm x",
	}
	for _, p := range gate {
		if promptContainsOnlyReadOnlyCommands(p) {
			t.Errorf("promptContainsOnlyReadOnlyCommands(%q) = true, esperado false (gate)", p)
		}
	}
}

// TestExecuteReadOnlyCommandAndLog valida o executor read-only + auditoria em
// command_execution_log (caminho end-to-end).
func TestExecuteReadOnlyCommandAndLog(t *testing.T) {
	if os.Getenv("HOK_TOKEN") == "" {
		t.Skip("HOK_TOKEN nao definido")
	}
	// DB temporario espelhando o padrao de initLogsDB
	tmpDB := t.TempDir() + "/test_cmdclassify.db"
	orig := DB_PATH
	DB_PATH = tmpDB
	t.Cleanup(func() { DB_PATH = orig })
	initDB()
	sqliteExec(`CREATE TABLE IF NOT EXISTS command_execution_log (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, tenant_id TEXT, source TEXT, cmd TEXT, output_len INTEGER DEFAULT 0, status TEXT);`)

	out, err := executeReadOnlyCommand("echo ro_test_marker", "test", "t1")
	if err != nil {
		t.Fatalf("executeReadOnlyCommand erro: %v", err)
	}
	if !strings.Contains(out, "ro_test_marker") {
		t.Fatalf("output nao contem marcador: %q", out)
	}
	// verifica linha no log
	rows := sqliteExecParams(
		`SELECT count(*) FROM command_execution_log WHERE cmd LIKE '%ro_test_marker%' AND source='test' AND tenant_id='t1' AND status='ok';`,
	)
	if !strings.Contains(rows, "1") {
		t.Fatalf("log command_execution_log sem a linha esperada: %q", rows)
	}
}
