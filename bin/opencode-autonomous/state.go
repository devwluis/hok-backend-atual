package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StateBaseDir — diretório de estado do wrapper
const StateBaseDir = "/root/.local/share/opencode-autonomous"

// StateStaleThreshold — idade máxima de state/current sem heartbeat antes
// de considerar a sessão "órfã" (processo morreu mas state ficou).
const StateStaleThreshold = 60 * time.Second

// ActionRecord — registro de UMA tool_use executada. Persistido no state
// para auditoria e para que o resume saiba o que já rodou.
type ActionRecord struct {
	N        int       `json:"n"`                  // sequencial (1-based)
	Tool     string    `json:"tool"`              // bash, edit, read, write, webfetch
	Hash     string    `json:"hash"`              // sha256 do input normalizado
	Summary  string    `json:"summary"`           // tool + input truncado (humano-legível)
	Ts       time.Time `json:"ts"`                 // quando o wrapper viu o tool_use
	DurationMs int64    `json:"duration_ms,omitempty"` // quanto durou (se conhecido)
}

// SessionState — estado completo de uma sessão. Gravado em state/current
// (atomic via rename) a cada tool_use. Permite resume após crash.
type SessionState struct {
	ID                string         `json:"id"`
	Mode              string         `json:"mode"` // "run" (JSON stream) ou "tui" (SQLite polling)
	RepoPath          string         `json:"repo_path"`
	ConfigPath        string         `json:"config_path"`
	ConfigDir         string         `json:"config_dir"`
	Budget            int            `json:"budget"`
	ActionsUsed       int            `json:"actions_used"`
	StartedAt         string         `json:"started_at"`
	PID               int            `json:"pid"`  // PID do opencode (live)
	ProcessStartPID   int            `json:"process_start_pid"` // mesmo que PID (snapshot inicial)
	AutoRollback      bool           `json:"auto_rollback"`
	CBWindowMins      int            `json:"cb_window_mins"`
	CBMaxRepeat       int            `json:"cb_max_repeat"`
	BlockedReason     string         `json:"blocked_reason,omitempty"`
	Notes             string         `json:"notes,omitempty"`
	OpencodeSessionID string         `json:"opencode_session_id,omitempty"` // só p/ modo tui
	LastSeenPartTS    int64          `json:"last_seen_part_ts,omitempty"`    // só p/ modo tui

	// NOVOS (item 6 — persistência completa):
	Actions          []ActionRecord `json:"actions,omitempty"`     // cada tool_use (cresce durante sessão)
	CBEvents         []CBEvent      `json:"cb_events,omitempty"`   // sliding window do CB (persistido)
	UpdatedAt        string         `json:"updated_at"`             // último write de qualquer campo
	LastHeartbeat    string         `json:"last_heartbeat"`         // último tick de vida (signal handler)
	ResumeCount      int            `json:"resume_count"`           // quantas vezes foi resumed
}

// CurrentStatePath — arquivo de estado da sessão ativa
const CurrentStatePath = "/root/.local/share/opencode-autonomous/state/current"

// writeState — grava estado no arquivo current (atomic via rename)
func writeState(s *SessionState) error {
	s.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	tmp := CurrentStatePath + ".tmp"
	if err := writeJSON(tmp, s); err != nil {
		return err
	}
	return os.Rename(tmp, CurrentStatePath)
}

// touchHeartbeat — atualiza só LastHeartbeat (lightweight, evita carregar tudo)
func touchHeartbeat(s *SessionState) error {
	s.LastHeartbeat = time.Now().UTC().Format(time.RFC3339)
	s.UpdatedAt = s.LastHeartbeat
	return writeState(s)
}

// readCurrentState — lê estado atual (retorna nil se não há sessão ativa)
func readCurrentState() (*SessionState, error) {
	var s SessionState
	if err := readJSONInto(CurrentStatePath, &s); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// clearState — remove arquivo de estado (sessão encerrou)
func clearState() error {
	err := os.Remove(CurrentStatePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// historyDir — diretório de histórico de sessões
func historyDir() string {
	return filepath.Join(StateBaseDir, "state", "history")
}

// snapshotStateToHistory — copia o estado final pra history/<id>.json
func snapshotStateToHistory(s *SessionState) error {
	if err := os.MkdirAll(historyDir(), 0o755); err != nil {
		return err
	}
	path := filepath.Join(historyDir(), s.ID+".json")
	return writeJSON(path, s)
}

// listHistory — lista sessões passadas
func listHistory() ([]SessionState, error) {
	dir := historyDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SessionState
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		var s SessionState
		if err := readJSONInto(filepath.Join(dir, e.Name()), &s); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// findSnapshotDir — encontra o dir do snapshot pelo ID
func findSnapshotDir(id string) (string, error) {
	dir := filepath.Join(StateBaseDir, "snapshots", id)
	if _, err := os.Stat(filepath.Join(dir, "META.json")); err != nil {
		return "", fmt.Errorf("snapshot %s não encontrado em %s", id, dir)
	}
	return dir, nil
}