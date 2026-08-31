package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite (sem CGO)
)

// newOpencodeTUICmd — cria o comando opencode TUI padrão (sem --format json)
func newOpencodeTUICmd(repoPath, cfgDir string) *exec.Cmd {
	// `opencode [project]` — TUI puro, sem flags. O XDG_CONFIG_HOME
	// aponta pro config restritivo gerado pelo wrapper.
	cmd := exec.Command("opencode", repoPath)
	cmd.Dir = repoPath
	cmd.Env = append([]string{}, os.Environ()...)
	if cfgDir != "" {
		cmd.Env = setEnv(cmd.Env, "XDG_CONFIG_HOME", cfgDir)
	}
	return cmd
}

// waitForOpencodeSession — espera a session do opencode aparecer no DB.
// Poll a cada 200ms até timeout. Retorna ID da session ou erro.
func waitForOpencodeSession(repoPath string, timeout time.Duration) (string, error) {
	dbPath := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "opencode.db")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=journal_mode(WAL)")
		if err == nil {
			sid, err := findOpencodeSession(db, repoPath)
			db.Close()
			if err == nil && sid != "" {
				return sid, nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("timeout aguardando session aparecer no DB (path=%s, repo=%s)", dbPath, repoPath)
}

// runTUIWithKnownSession — polling loop dado que já temos o cmd spawned
// e o ID da session. Lê parts novos do DB e chama onToolUse pra cada um.
func runTUIWithKnownSession(
	cmd *exec.Cmd,
	state *SessionState,
	pollInterval time.Duration,
	onToolUse func(tool, hash, summary string),
) (*InterceptResult, error) {
	dbPath := filepath.Join(os.Getenv("HOME"), ".local", "share", "opencode", "opencode.db")
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("abre db %s: %w", dbPath, err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	log("polling session opencode=%s (interval=%dms)", state.OpencodeSessionID, pollInterval.Milliseconds())

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	lastSeenMS := time.Now().UnixMilli() - 1000

	for {
		select {
		case <-ticker.C:
			parts, err := pollNewParts(db, state.OpencodeSessionID, lastSeenMS)
			if err != nil {
				log("poll err: %v", err)
				continue
			}
			for _, p := range parts {
				if p.State.Status == "running" {
					continue
				}
				if p.State.Status == "error" && strings.Contains(p.State.Error, "rejected") {
					continue
				}
				if p.Type != "tool" {
					continue
				}
				inputJSON, _ := json.Marshal(p.State.Input)
				hash := sha256hex(string(inputJSON))
				summary := fmt.Sprintf("%s(%s)", p.Tool, truncate(string(inputJSON), 80))
				if onToolUse != nil {
					onToolUse(p.Tool, hash, summary)
				}
				if p.TimeMS > lastSeenMS {
					lastSeenMS = p.TimeMS
					state.LastSeenPartTS = lastSeenMS
				}
			}
			// opencode ainda vivo?
			if err := syscall.Kill(state.PID, 0); err != nil {
				waitErr := cmd.Wait()
				exitCode := 0
				if waitErr != nil {
					if ee, ok := waitErr.(*exec.ExitError); ok {
						exitCode = ee.ExitCode()
					}
				}
				return &InterceptResult{Reason: "opencode_exit", ExitCode: exitCode}, nil
			}
		}
	}
}

// PartData — estrutura mínima do JSON em part.data quando type=="tool"
type PartData struct {
	Type    string     `json:"type"`
	Tool    string     `json:"tool"`
	CallID  string     `json:"callID"`
	State   PartState  `json:"state"`
	Time    *time.Time `json:"-"`
	TimeMS  int64      `json:"-"` // timestamp em ms unix epoch
}

type PartState struct {
	Status string                 `json:"status"` // running | completed | error
	Input  map[string]interface{} `json:"input"`
	Output string                 `json:"output,omitempty"`
	Error  string                 `json:"error,omitempty"`
	Time   map[string]int64       `json:"time"`
}

// RunTUIWithAudit — LEGADO: ver runTUIWithKnownSession + waitForOpencodeSession.
// Mantido por compat de API; novo código deve usar o par.
func RunTUIWithAudit(
	repoPath string,
	cfgDir string,
	state *SessionState,
	pollInterval time.Duration,
	onToolUse func(tool, hash, summary string),
) (*InterceptResult, error) {
	cmd := newOpencodeTUICmd(repoPath, cfgDir)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start opencode tui: %w", err)
	}
	state.PID = cmd.Process.Pid
	sid, err := waitForOpencodeSession(repoPath, 10*time.Second)
	if err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	state.OpencodeSessionID = sid
	return runTUIWithKnownSession(cmd, state, pollInterval, onToolUse)
}

// findOpencodeSession — acha a session mais recente do opencode que
// corresponde ao repoPath. Janela de tolerância: sessions criadas nos
// últimos 5 minutos.
func findOpencodeSession(db *sql.DB, repoPath string) (string, error) {
	abs, err := filepath.Abs(repoPath)
	if err != nil {
		return "", err
	}
	// query: sessions onde directory = abs E time_created > now-5min
	cutoff := time.Now().Add(-5 * time.Minute).UnixMilli()
	rows, err := db.Query(`
		SELECT id, time_created FROM session
		WHERE directory = ? AND time_created >= ?
		ORDER BY time_created DESC
		LIMIT 5
	`, abs, cutoff)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var ts int64
		if err := rows.Scan(&id, &ts); err != nil {
			return "", err
		}
		// Pega a mais recente
		return id, nil
	}
	return "", nil
}

// pollNewParts — lê parts novos (time_created > lastSeenMS) da session
func pollNewParts(db *sql.DB, sessionID string, lastSeenMS int64) ([]PartData, error) {
	rows, err := db.Query(`
		SELECT data, time_created FROM part
		WHERE session_id = ? AND time_created > ?
		ORDER BY time_created ASC
		LIMIT 100
	`, sessionID, lastSeenMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PartData
	for rows.Next() {
		var data string
		var ts int64
		if err := rows.Scan(&data, &ts); err != nil {
			continue
		}
		var p PartData
		if err := json.Unmarshal([]byte(data), &p); err != nil {
			continue
		}
		p.TimeMS = ts
		out = append(out, p)
	}
	return out, nil
}