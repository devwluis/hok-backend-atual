package main

import (
	"fmt"
	"strings"
	"time"
)

// ─── Tabela agent_memory ─────────────────────────────────────────────────────
// Criada via initAgentMemory() chamado no initSQLite()

func initAgentMemory() {
	sqliteExec(`CREATE TABLE IF NOT EXISTS agent_memory (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		ts        DATETIME DEFAULT CURRENT_TIMESTAMP,
		task      TEXT,
		file      TEXT,
		model     TEXT,
		success   INTEGER,
		error_msg TEXT,
		iterations INTEGER
	);`)
	sqliteExec(`CREATE TABLE IF NOT EXISTS model_stats (
		model   TEXT PRIMARY KEY,
		wins    INTEGER DEFAULT 0,
		fails   INTEGER DEFAULT 0,
		last_ts DATETIME DEFAULT CURRENT_TIMESTAMP
	);`)
}

// saveAgentMemory — grava resultado de uma execução do agent-loop
func saveAgentMemory(task, file, model string, success bool, errMsg string, iters int) {
	s := 0
	if success {
		s = 1
	}
	safe := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}
	sqliteExec(fmt.Sprintf(
		`INSERT INTO agent_memory (task, file, model, success, error_msg, iterations)
		 VALUES ('%s','%s','%s',%d,'%s',%d);`,
		safe(task), safe(file), safe(model), s, safe(errMsg), iters,
	))

	// Atualiza model_stats
	if success {
		sqliteExec(fmt.Sprintf(
			`INSERT INTO model_stats (model, wins, fails) VALUES ('%s',1,0)
			 ON CONFLICT(model) DO UPDATE SET wins=wins+1, last_ts='%s';`,
			model, time.Now().Format("2006-01-02 15:04:05"),
		))
	} else {
		sqliteExec(fmt.Sprintf(
			`INSERT INTO model_stats (model, wins, fails) VALUES ('%s',0,1)
			 ON CONFLICT(model) DO UPDATE SET fails=fails+1, last_ts='%s';`,
			model, time.Now().Format("2006-01-02 15:04:05"),
		))
	}
}

// queryAgentMemory — retorna contexto relevante para o prompt do Hermes
func queryAgentMemory(file, model string) string {
	var parts []string

	// Falhas recentes no mesmo arquivo
	recentFails := sqliteExec(fmt.Sprintf(
		`SELECT task, error_msg, ts FROM agent_memory
		 WHERE file='%s' AND success=0
		 ORDER BY ts DESC LIMIT 3;`,
		strings.ReplaceAll(file, "'", "''"),
	))
	if strings.TrimSpace(recentFails) != "" {
		parts = append(parts, "RECENT FAILURES ON THIS FILE:\n"+recentFails)
	}

	// Stats do modelo atual
	stats := sqliteExec(fmt.Sprintf(
		`SELECT wins, fails FROM model_stats WHERE model='%s';`,
		model,
	))
	if strings.TrimSpace(stats) != "" {
		parts = append(parts, "MODEL STATS ("+model+"): "+strings.TrimSpace(stats))
	}

	// Modelos com muitas falhas (para avisar Hermes)
	badModels := sqliteExec(
		`SELECT model, fails FROM model_stats WHERE fails >= 3 ORDER BY fails DESC LIMIT 5;`,
	)
	if strings.TrimSpace(badModels) != "" {
		parts = append(parts, "UNRELIABLE MODELS (avoid):\n"+badModels)
	}

	if len(parts) == 0 {
		return ""
	}
	return "\n\nAGENT MEMORY:\n" + strings.Join(parts, "\n---\n")
}

// getTopModel — retorna o modelo com mais wins
func getTopModel() string {
	result := sqliteExec(
		`SELECT model FROM model_stats WHERE wins > 0 ORDER BY wins DESC LIMIT 1;`,
	)
	result = strings.TrimSpace(result)
	if result != "" {
		return result
	}
	return defaultHermesModel
}

// ModelStat representa estatísticas de um modelo no ranking
type ModelStat struct {
	Model   string  `json:"model"`
	Wins    int     `json:"wins"`
	Fails   int     `json:"fails"`
	Total   int     `json:"total"`
	WinRate float64 `json:"win_rate"`
	IsTop   bool    `json:"is_top"`
}

// getModelStats retorna o ranking completo via sqliteExec
// Output do sqlite3: "model|wins|fails" por linha
func getModelStats() []ModelStat {
	result := sqliteExec(`SELECT model, wins, fails FROM model_stats ORDER BY wins DESC, model ASC;`)
	result = strings.TrimSpace(result)
	if result == "" {
		return []ModelStat{}
	}

	var stats []ModelStat
	for _, line := range strings.Split(result, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		var s ModelStat
		s.Model = parts[0]
		fmt.Sscanf(parts[1], "%d", &s.Wins)
		fmt.Sscanf(parts[2], "%d", &s.Fails)
		s.Total = s.Wins + s.Fails
		if s.Total > 0 {
			s.WinRate = float64(s.Wins) / float64(s.Total) * 100.0
		}
		stats = append(stats, s)
	}

	// Marcar top model
	if len(stats) > 0 && stats[0].Wins > 0 {
		stats[0].IsTop = true
	}
	return stats
}
