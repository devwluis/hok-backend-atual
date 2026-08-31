package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// OpenCodeEvent — estrutura mínima dos eventos JSON do opencode run --format json
type OpenCodeEvent struct {
	Type      string          `json:"type"` // step_start, tool_use, step_finish, etc
	Timestamp int64           `json:"timestamp"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part"` // estrutura varia por type
}

// ToolUsePart — sub-estrutura de part quando type=="tool"
type ToolUsePart struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"` // "tool"
	SessionID string          `json:"sessionID"`
	MessageID string          `json:"messageID"`
	Tool      string          `json:"tool"` // bash, edit, read, write, webfetch
	CallID    string          `json:"callID"`
	State     ToolUseState    `json:"state"`
	Metadata  json.RawMessage `json:"metadata"`
}

type ToolUseState struct {
	Status string                 `json:"status"` // running, completed, error
	Input  map[string]interface{} `json:"input"`
	Error  string                 `json:"error,omitempty"`
	Time   map[string]int64       `json:"time"`
}

// InterceptResult — resultado da sessão de intercepção
type InterceptResult struct {
	Reason   string // "completed", "budget_exhausted", "circuit_breaker", "opencode_exit"
	ExitCode int
}

// SpawnOpt — opções de spawn (process group)
func SpawnOpt() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// RunOpenCodeWithIntercept — spawna opencode run --format json e intercepta
// cada tool_use pra budget/CB. SIGTERM no opencode quando bloqueia.
func RunOpenCodeWithIntercept(
	repoPath string,
	userMessage string,
	model string,
	cfgDir string,
	state *SessionState,
	onToolUse func(tool, hash, summary string),
) (*InterceptResult, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	args := []string{"run", userMessage, "--format", "json", "--model", model}
	cmd := exec.CommandContext(ctx, "opencode", args...)
	cmd.Dir = repoPath
	cmd.SysProcAttr = SpawnOpt() // Setpgid=true: opencode vira líder do próprio process group
	cmd.Env = append([]string{}, baseEnv()...)
	if cfgDir != "" {
		cmd.Env = setEnv(cmd.Env, "XDG_CONFIG_HOME", cfgDir)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start opencode: %w", err)
	}
	state.PID = cmd.Process.Pid
	log("opencode iniciado: pid=%d (pgid=%d)", state.PID, cmd.Process.Pid)

	// scanner linha-a-linha do stdout (JSON events)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024) // até 1MB por linha

	var lastBlockReason string

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev OpenCodeEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			// não-JSON (logs misturados?), ignora
			continue
		}
		if ev.Type != "tool_use" {
			continue
		}
		var p ToolUsePart
		if err := json.Unmarshal(ev.Part, &p); err != nil {
			continue
		}

		// Só conta ações REAIS (não cancelled nem rejected)
		if p.State.Status == "error" && strings.Contains(p.State.Error, "rejected") {
			continue // permission rejected — não conta
		}

		// Gera hash determinístico do input (canônico: keys sorted)
		inputJSON, _ := json.Marshal(p.State.Input)
		hash := sha256hex(string(inputJSON))
		summary := fmt.Sprintf("%s(%s)", p.Tool, truncate(string(inputJSON), 80))

		if onToolUse != nil {
			onToolUse(p.Tool, hash, summary)
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		log("scanner err: %v", err)
	}

	waitErr := cmd.Wait()
	exitCode := 0
	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
	}
	log("opencode saiu: exit=%d", exitCode)

	reason := "opencode_exit"
	if lastBlockReason != "" {
		reason = lastBlockReason
	}
	return &InterceptResult{Reason: reason, ExitCode: exitCode}, nil
}

// KillOpenCode — mata o process group inteiro do opencode (pgid = pid por causa
// do Setpgid). Usa SIGTERM, espera 1s, depois SIGKILL se necessário.
// Idempotente — se o pid for 0 ou negativo, ignora.
func KillOpenCode(state *SessionState) error {
	if state.PID <= 0 {
		return nil
	}
	pid := state.PID
	pgid := -pid // kill negativo = killpg

	// 1. SIGTERM no group inteiro
	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil {
		// ESRCH = no such process (já morreu)
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("kill pgid %d: %w", pid, err)
	}

	// 2. Espera até 1.5s pelo processo sumir
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err == syscall.ESRCH {
			return nil // morreu
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 3. SIGKILL forçado
	_ = syscall.Kill(pgid, syscall.SIGKILL)
	time.Sleep(200 * time.Millisecond)
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func baseEnv() []string {
	// Pega env do processo atual (sem variáveis perigosas que o opencode
	// possa usar pra escalar privilégios).
	return append([]string{}, os.Environ()...)
}

func setEnv(env []string, key, value string) []string {
	out := env[:0]
	found := false
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			out = append(out, key+"="+value)
			found = true
		} else {
			out = append(out, e)
		}
	}
	if !found {
		out = append(out, key+"="+value)
	}
	return out
}