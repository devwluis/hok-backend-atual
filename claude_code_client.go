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
	ctx, cancel := context.WithTimeout(context.Background(), claudeCodeTimeout)
	defer cancel()

	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose"}
	var cmd *exec.Cmd
	if skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
		sudoArgs := append([]string{"-u", "claudeagent", "-H", "claude"}, args...)
		cmd = exec.CommandContext(ctx, "sudo", sudoArgs...)
	} else {
		cmd = exec.CommandContext(ctx, "claude", args...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	logTag := "claude_code_invoke:minimax-m2.5"
	if skipPermissions {
		logTag = "claude_code_invoke_approved:minimax-m2.5"
	}

	if ctx.Err() == context.DeadlineExceeded {
		sqliteExec(fmt.Sprintf("INSERT INTO logs (event, level, source) VALUES ('%s timeout', 'WARN', 'claude_code_client');", logTag))
		return "", fmt.Errorf("claude code: timeout apos %s", claudeCodeTimeout)
	}

	text, parseErr := extractTextFromStream(stdout.String())

	if runErr != nil && text == "" {
		sqliteExec(fmt.Sprintf("INSERT INTO logs (event, level, source) VALUES ('%s fail', 'WARN', 'claude_code_client');", logTag))
		return "", fmt.Errorf("claude code: exit error: %v — stderr: %s", runErr, stderr.String())
	}

	if text == "" {
		sqliteExec(fmt.Sprintf("INSERT INTO logs (event, level, source) VALUES ('%s empty', 'WARN', 'claude_code_client');", logTag))
		errDetail := "nenhum bloco de texto encontrado no stream"
		if parseErr != nil {
			errDetail = parseErr.Error()
		}
		return "", fmt.Errorf("claude code: resposta vazia: %s", errDetail)
	}

	sqliteExec(fmt.Sprintf("INSERT INTO logs (event, level, source) VALUES ('%s ok', 'INFO', 'claude_code_client');", logTag))
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
