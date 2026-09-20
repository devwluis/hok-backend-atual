package main

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

var db *sql.DB

func initDB() {
	os.MkdirAll(filepath.Dir(DB_PATH), 0755)
	var err error
	db, err = sql.Open("sqlite", DB_PATH)
	if err != nil {
		log.Fatalf("FALHA ao abrir SQLite: %v", err)
	}
	db.SetMaxOpenConns(1)
}

func sqliteExec(query string) string {
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "PRAGMA") {
		return runQuery(query)
	}
	return runExec(query)
}

func sqliteExecParams(query string, args ...interface{}) string {
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "PRAGMA") {
		return runQueryParams(query, args...)
	}
	return runExecParams(query, args...)
}

// sqliteExecQuoted retorna o resultado no formato "quote" (estilo .mode quote):
// cada campo entre aspas duplas, aspas internas escapadas como "", newlines como
// \n literal e campos separados por virgula — 1 linha fisica por registro.
func sqliteExecQuoted(query string, args ...interface{}) string {
	trimmed := strings.TrimSpace(strings.ToUpper(query))
	if !(strings.HasPrefix(trimmed, "SELECT") || strings.HasPrefix(trimmed, "PRAGMA")) {
		return runExecParams(query, args...)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var lines []string
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		parts := make([]string, len(cols))
		for i, v := range vals {
			if v == nil {
				parts[i] = `""`
			} else if b, ok := v.([]byte); ok {
				parts[i] = quoteField(string(b))
			} else {
				parts[i] = quoteField(fmt.Sprintf("%v", v))
			}
		}
		lines = append(lines, strings.Join(parts, ","))
	}
	return strings.Join(lines, "\n")
}

func quoteField(s string) string {
	s = strings.ReplaceAll(s, `"`, `""`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

// parseQuotedRows converte a saida de sqliteExecQuoted de volta em linhas de
// campos, reexpandindo os escapes (\n -> newline real, \r -> CR). Linhas que
// nao parseiam ou nao tem exatamente o numero esperado de campos sao
// descartadas.
func parseQuotedRows(out string, expected int) [][]string {
	var rows [][]string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		fields, err := csv.NewReader(strings.NewReader(line)).Read()
		if err != nil || len(fields) != expected {
			continue
		}
		for i, f := range fields {
			f = strings.ReplaceAll(f, `\n`, "\n")
			f = strings.ReplaceAll(f, `\r`, "\r")
			fields[i] = f
		}
		rows = append(rows, fields)
	}
	return rows
}

func runExec(query string) string {
	return runExecParams(query)
}

func runExecParams(query string, args ...interface{}) string {
	_, err := db.Exec(query, args...)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return "OK"
}

func runQuery(query string) string {
	return runQueryParams(query)
}

func runQueryParams(query string, args ...interface{}) string {
	rows, err := db.Query(query, args...)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}

	var lines []string
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		parts := make([]string, len(cols))
		for i, v := range vals {
			if v == nil {
				parts[i] = ""
			} else if b, ok := v.([]byte); ok {
				parts[i] = string(b)
			} else {
				parts[i] = fmt.Sprintf("%v", v)
			}
		}
		lines = append(lines, strings.Join(parts, "|"))
	}
	return strings.Join(lines, "\n")
}

var allowedCountTables = map[string]bool{
	"memory":       true,
	"memories":     true,
	"logs":         true,
	"codex":        true,
	"users":        true,
	"repositories": true,
}

func getSQLiteCount(table string) int {
	if !allowedCountTables[table] {
		sqliteExecParams("INSERT INTO logs (event, level, source) VALUES ('getSQLiteCount:invalid_table:'||?, 'WARN', 'db');", table)
		return 0
	}
	out := sqliteExecParams("SELECT count(*) FROM " + table + ";")
	count := 0
	fmt.Sscanf(out, "%d", &count)
	return count
}

func initSQLite() {
	initDB()
	tables := []string{
		`CREATE TABLE IF NOT EXISTS memory (role TEXT, content TEXT, ts INTEGER);`,
		`CREATE TABLE IF NOT EXISTS memories (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, key TEXT UNIQUE, value TEXT);`,
		`CREATE TABLE IF NOT EXISTS logs (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, event TEXT NOT NULL, level TEXT DEFAULT 'INFO', source TEXT DEFAULT 'hokma_v21');`,
		`CREATE TABLE IF NOT EXISTS codex (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, tag TEXT, title TEXT, content TEXT);`,
		`CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, senha_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'client', tenant_id TEXT, criado_em INTEGER NOT NULL DEFAULT (unixepoch()));`,
		`CREATE TABLE IF NOT EXISTS sessions (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, email TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'client', token_hash TEXT NOT NULL, exp INTEGER NOT NULL, revoked INTEGER DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()), last_used INTEGER DEFAULT (unixepoch()));`,
		`CREATE TABLE IF NOT EXISTS user_totp (email TEXT PRIMARY KEY, secret TEXT NOT NULL, recovery_hashes TEXT NOT NULL, enabled INTEGER DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()));`,
		`CREATE TABLE IF NOT EXISTS login_attempts (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT, action TEXT, success INTEGER DEFAULT 0, ts INTEGER DEFAULT (unixepoch()));`,
		`CREATE TABLE IF NOT EXISTS conversations (id TEXT PRIMARY KEY, title TEXT NOT NULL DEFAULT 'Nova conversa', project TEXT NOT NULL DEFAULT 'default', model TEXT NOT NULL DEFAULT 'default', created_at INTEGER NOT NULL DEFAULT (strftime('%s','now')), updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')), tenant_id TEXT, user_id TEXT);`,
		`CREATE INDEX IF NOT EXISTS idx_conversations_updated ON conversations(updated_at DESC);`,
		`CREATE TABLE IF NOT EXISTS conv_messages (id TEXT PRIMARY KEY, conv_id TEXT NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, role TEXT NOT NULL, content TEXT NOT NULL, ts INTEGER NOT NULL DEFAULT (strftime('%s','now')), attachments TEXT DEFAULT NULL);`,
		`CREATE INDEX IF NOT EXISTS idx_conv_messages_conv_id ON conv_messages(conv_id);`,
		`CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL DEFAULT (strftime('%s','now')));`,
		`CREATE TABLE IF NOT EXISTS repositories (id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, remote_url TEXT, branch TEXT, language TEXT, local_path TEXT, stars INTEGER DEFAULT 0, created_at INTEGER, updated_at INTEGER);`,
		`CREATE TABLE IF NOT EXISTS pending_actions (key TEXT PRIMARY KEY, id TEXT, tool_name TEXT, args_json TEXT, description TEXT, created_at TEXT, action_type TEXT, tenant_id TEXT, diff_preview TEXT);`,
		`CREATE TABLE IF NOT EXISTS pending_automations (id TEXT PRIMARY KEY, description TEXT, workflow_json TEXT, created_at TEXT);`,
		`CREATE TABLE IF NOT EXISTS self_modifications (id INTEGER PRIMARY KEY AUTOINCREMENT, tenant_id TEXT, commit_hash TEXT, file_path TEXT, ia_description TEXT, diff_summary TEXT, smoke_test_passed INTEGER DEFAULT 0, status TEXT, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);`,
		`CREATE TABLE IF NOT EXISTS command_execution_log (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME DEFAULT CURRENT_TIMESTAMP, tenant_id TEXT, source TEXT, cmd TEXT, output_len INTEGER DEFAULT 0, status TEXT);`,
		`CREATE TABLE IF NOT EXISTS session_mode (
			tenant_id          TEXT NOT NULL,
			user_id            TEXT NOT NULL,
			conv_id            TEXT NOT NULL,
			mode               TEXT NOT NULL DEFAULT 'plan' CHECK (mode IN ('plan','build','autonomous','autonomous_total')),
			autonomous_budget  INTEGER NOT NULL DEFAULT 0,
			set_by             TEXT NOT NULL DEFAULT '',
			opencode_session_id TEXT NOT NULL DEFAULT '',
			checkpoint_id      TEXT NOT NULL DEFAULT '',
			auto_rollback      INTEGER NOT NULL DEFAULT 0,
			updated_at         INTEGER NOT NULL DEFAULT (unixepoch()),
			PRIMARY KEY (tenant_id, user_id, conv_id)
		);`,
		`CREATE TABLE IF NOT EXISTS autonomous_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER DEFAULT (unixepoch()),
			conv_id TEXT, tenant_id TEXT, user_id TEXT,
			agent TEXT, action TEXT, action_hash TEXT,
			budget_left INTEGER, status TEXT
		);`,
		`CREATE TABLE IF NOT EXISTS catalog_snapshot (
			id TEXT PRIMARY KEY,
			provider TEXT, category TEXT, source TEXT, seen_at INTEGER DEFAULT (unixepoch())
		);`,
		`CREATE TABLE IF NOT EXISTS catalog_audit (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ts INTEGER DEFAULT (unixepoch()),
			action TEXT, model_id TEXT, provider TEXT, source TEXT, detail TEXT
		);`,
		// DeepSeek não expõe "total carregado" histórico (só o saldo atual).
		// Guardamos snapshots locais para derivar total_loaded (fixo, cresce
		// em recargas) e spent = total_loaded - balance. Ver deepseek_credits.go.
		`CREATE TABLE IF NOT EXISTS deepseek_balance_snapshots (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			ts           INTEGER NOT NULL DEFAULT (unixepoch()),
			currency     TEXT,
			balance      REAL NOT NULL,
			topped_up    REAL,
			granted      REAL,
			total_loaded REAL NOT NULL,
			spent        REAL NOT NULL
		);`,
		// Orchestrator state: salva o estado do loop do orquestrador entre
		// aprovações de pending_action no modo build. Permite retomada
		// síncrona após /actions/approve (sequencial multi-mutação).
		`CREATE TABLE IF NOT EXISTS orchestrator_state (
			conv_id      TEXT PRIMARY KEY,
			tenant_id    TEXT NOT NULL,
			task         TEXT NOT NULL,
			messages     TEXT NOT NULL,
			step         INTEGER NOT NULL,
			model        TEXT NOT NULL,
			mode         TEXT NOT NULL,
			agent_id     TEXT DEFAULT '',
			max_steps    INTEGER DEFAULT 15,
			created_at   TEXT DEFAULT CURRENT_TIMESTAMP,
			expires_at   TEXT NOT NULL,
			plan         TEXT DEFAULT '[]'
		);`,
	}
	for _, t := range tables {
		sqliteExec(t)
	}
	// MIGRATION (10/09): coluna plan em orchestrator_state. DBs criados antes
	// desta data têm a tabela sem a coluna (era aplicada via ALTER TABLE
	// manual). Detecta pela ausência e aplica ALTER TABLE defensivo — sem isso
	// o resume em DB fresco quebra com "no such column: plan".
	if row := sqliteExec(`SELECT sql FROM sqlite_master WHERE type='table' AND name='orchestrator_state';`); row != "" && !strings.Contains(row, "plan") {
		log.Println("MIGRATION: orchestrator_state → adicionando coluna plan")
		sqliteExec(`ALTER TABLE orchestrator_state ADD COLUMN plan TEXT DEFAULT '[]';`)
	}
	// BLOCO 1 (03/09): orquestrador + subagentes + tracing (hok_agents, runs).
	initAgentOrchestratorSchema()
	// MIGRATION (29/08): o CHECK do session_mode ganhou 'autonomous_total'
	// e a tabela ganhou checkpoint_id + auto_rollback. SQLite não altera
	// CHECK/colunas — recria preservando as linhas (padrão das migrations
	// anteriores, ex.: CHECK do plan em 28/08).
	sqliteExec(`INSERT INTO logs (event, level, source) VALUES ('migration:session_mode autonomus_total check', 'INFO', 'db');`)
	row := sqliteExec(`SELECT sql FROM sqlite_master WHERE type='table' AND name='session_mode';`)
	if !strings.Contains(row, "autonomous_total") {
		log.Println("MIGRATION: session_mode → CHECK com autonomous_total + checkpoint_id + auto_rollback")
		sqliteExec(`ALTER TABLE session_mode RENAME TO session_mode_old;`)
		sqliteExec(`CREATE TABLE session_mode (
			tenant_id          TEXT NOT NULL,
			user_id            TEXT NOT NULL,
			conv_id            TEXT NOT NULL,
			mode               TEXT NOT NULL DEFAULT 'plan' CHECK (mode IN ('plan','build','autonomous','autonomous_total')),
			autonomous_budget  INTEGER NOT NULL DEFAULT 0,
			set_by             TEXT NOT NULL DEFAULT '',
			opencode_session_id TEXT NOT NULL DEFAULT '',
			checkpoint_id      TEXT NOT NULL DEFAULT '',
			auto_rollback      INTEGER NOT NULL DEFAULT 0,
			updated_at         INTEGER NOT NULL DEFAULT (unixepoch()),
			PRIMARY KEY (tenant_id, user_id, conv_id)
		);`)
		sqliteExec(`INSERT INTO session_mode (tenant_id, user_id, conv_id, mode, autonomous_budget, set_by, opencode_session_id, updated_at)
			SELECT tenant_id, user_id, conv_id, mode, autonomous_budget, set_by, opencode_session_id, updated_at FROM session_mode_old;`)
		sqliteExec(`DROP TABLE session_mode_old;`)
		log.Println("MIGRATION session_mode OK (linhas preservadas)")
	}
	// MIGRATION (10/09): migrar modo "autonomous" (legado) → "build".
	// O botão "Autônomo" foi removido do frontend; "build" é o equivalente seguro.
	sqliteExec(`UPDATE session_mode SET mode = 'build' WHERE mode = 'autonomous';`)
	sqliteExec(`INSERT INTO logs (event, level) VALUES ('migration:autonomous→build', 'INFO');`)
	sqliteExec(`INSERT INTO logs (event, level) VALUES ('HOK Backend v22 iniciado', 'SUCCESS');`)
	initAgentMemory()
	log.Println("SQLite inicializado")
}

// --- API Callers ---
