package main
import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)
const claudeCodeTimeout = 120 * time.Second
func isClaudeCodeTask(msg string) bool {
	keywords := []string{
		"edite o arquivo", "edita o arquivo", "editar arquivo",
		"leia o arquivo", "leia esse arquivo", "leia este arquivo",
		"refatore", "refatorar",
		"revise o codigo", "revise o código", "revisao de codigo", "revisão de código",
		"corrija o bug", "corrigir o bug", "conserte o bug",
		"implemente", "implementar",
		"modifique o arquivo", "modificar arquivo",
		"rode o comando", "roda o comando", "execute o comando",
		"analise o repositorio", "analise o repositório",
	}
	lower := strings.ToLower(msg)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}
func buildClaudeCodePrompt(msg string, req ClientRequest) string {
	return msg
}
func describeClaudeCodeAction(prompt string) string {
	p := strings.TrimSpace(prompt)
	if len(p) > 200 {
		p = p[:200] + "..."
	}
	return fmt.Sprintf("Vou executar via Claude Code (com acesso a arquivos/bash): \"%s\"", p)
}
type claudeStreamEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}
func callClaudeCode(prompt string) (string, error) {
	return runClaudeCodeCLI(prompt, false)
}
func callClaudeCodeApproved(prompt string) (string, error) {
	return runClaudeCodeCLI(prompt, true)
}
func runClaudeCodeCLI(prompt string, skipPermissions bool) (string, error) {
	// FASE 2b: sudo direto continua proibido mesmo no fluxo approved — o resto
	// (edicao de arquivos, git, bash sem sudo) executa normalmente.
	if skipPermissions && strings.Contains(strings.ToLower(prompt), "sudo") {
		return "", claudeCodeBlocked()
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeCodeTimeout)
	defer cancel()
	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose"}
	if skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	cmd := exec.CommandContext(ctx, "claude", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	logTag := "claude_code_invoke:minimax-m3"
	if skipPermissions {
		logTag = "claude_code_invoke_approved:minimax-m3"
	}
	if ctx.Err() == context.DeadlineExceeded {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s timeout", logTag),
		)
		return "", fmt.Errorf("claude code: timeout apos %s", claudeCodeTimeout)
	}
	text, parseErr := extractTextFromStream(stdout.String())
	if runErr != nil && text == "" {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s fail", logTag),
		)
		return "", fmt.Errorf("claude code: exit error: %v — stderr: %s", runErr, stderr.String())
	}
	if text == "" {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s empty", logTag),
		)
		errDetail := "nenhum bloco de texto encontrado no stream"
		if parseErr != nil {
			errDetail = parseErr.Error()
		}
		return "", fmt.Errorf("claude code: resposta vazia: %s", errDetail)
	}
	sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'INFO', 'claude_code_client');`,
			fmt.Sprintf("%s ok", logTag),
		)
	return text, nil
}
func extractTextFromStream(raw string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(raw))
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var out strings.Builder
	var lastErr error
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, `"type":"assistant"`) {
			continue
		}
		var event claudeStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			lastErr = err
			continue
		}
		if event.Type != "assistant" {
			continue
		}
		for _, c := range event.Message.Content {
			if c.Type == "text" && c.Text != "" {
				out.WriteString(c.Text)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		lastErr = err
	}
	result := strings.TrimSpace(out.String())
	if result == "" && lastErr != nil {
		return "", lastErr
	}
	return result, nil
}
// === FASE 2b: Bloqueio de execucao sudo direta ===
func claudeCodeBlocked() error {
    return fmt.Errorf("EXECUCAO BLOQUEADA: uso de sudo direto foi desativado. Use o agent loop bash_exec (mutantTools) em vez de callClaudeCode direto. O comando sera roteado pelo gate de aprovacao com diff visual.")
}
