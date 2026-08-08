package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

func sqliteExec(query string) string {
	return executeCommand(fmt.Sprintf(`sqlite3 %s "%s"`, DB_PATH, query))
}

func getSQLiteCount(table string) int {
	out := sqliteExec(fmt.Sprintf("SELECT count(*) FROM %s;", table))
	count := 0
	fmt.Sscanf(out, "%d", &count)
	return count
}

func initSQLite() {
	os.MkdirAll(filepath.Dir(DB_PATH), 0755)
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
	log.Println("✅ SQLite inicializado")
}

// ─── API Callers ─────────────────────────────────────────────────────────────
