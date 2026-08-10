package main

import (
	"database/sql"
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

func getSQLiteCount(table string) int {
	out := sqliteExec(fmt.Sprintf("SELECT count(*) FROM %s;", table))
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
		`CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);`,
		`CREATE TABLE IF NOT EXISTS repositories (id TEXT PRIMARY KEY, kind TEXT NOT NULL, name TEXT NOT NULL, remote_url TEXT, branch TEXT, language TEXT, local_path TEXT, stars INTEGER DEFAULT 0, created_at INTEGER, updated_at INTEGER);`,
		`CREATE TABLE IF NOT EXISTS pending_actions (key TEXT PRIMARY KEY, id TEXT, tool_name TEXT, args_json TEXT, description TEXT, created_at TEXT, action_type TEXT, tenant_id TEXT, diff_preview TEXT);`,
	}
	for _, t := range tables {
		sqliteExec(t)
	}
	sqliteExec(`INSERT INTO logs (event, level) VALUES ('HOK Backend v22 iniciado', 'SUCCESS');`)
	initAgentMemory()
	log.Println("SQLite inicializado")
}

// --- API Callers ---
