package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"
)

// claudeCodeTimeout limita a duracao do claude_code CLI por chamada.
// FIX 16/08: 120s era menor que a latencia real do modo planejar (visto
// respostas legitimas de 69s+ no claude_code) e o nginx cortava em 60s.
// Subido para 300s (5min) como margem generosa para respostas complexas.
const claudeCodeTimeout = 300 * time.Second

// errSystemPromptLeak indica que a resposta do claude_code continha
// vazamento de system prompt/skills do SDK e foi bloqueada (fix 16/08 item B).
var errSystemPromptLeak = errors.New("claude_code: resposta com vazamento de system prompt bloqueada")

// sinais FORTES: 1 ocorrencia basta. São frases do system prompt do SDK /
// narrativa interna do modelo que nunca deveriam aparecer numa resposta útil
// (o HOK responde em PT-BR; qualquer "respond in Portuguese" é regurgitação).
var systemPromptLeakStrong = []string{
	"still-silent",
	"/dev-discord-webhook",
	"system reminder says",
	"respond in portuguese",
	"per the project instructions",
	"must already want to invoke a skill",
	"skill description here",
	"these skills are available",
	"save analysis in your own memory",
	"let me just read",
	"let me read the",
	"let me look at",
	"the user has a question",
	"i should respond",
	"use the read tool",
	"use the bash tool",
	"use the edit tool",
	"delegated agent",
	"working notes",
	"harness attaches",
	"security agent that demands",
	"secure multi-agent facility",
	"opt-in as described",
	"discussions channel",
	"prompt-injection concerns",
	"git state only",
	"chain review methodology",
	"attack-surface-reduction",
}

// sinais FRACOS: precisam de 2+ ocorrencias distintas. São termos típicos do
// system prompt / lista de skills do SDK.
var systemPromptLeakSignals = []string{
	"## Skills",
	"# Skills",
	"list of agents",
	"list of skills",
	"/agents",
	"/workflows",
	"mcp__",
	"perform_scan",
	"keychain:",
	"worktree",
	"killer-content",
	"official login flow",
	"codeofconduct:",
	"digest: produce",
	"specialized reviewers",
	"the user wants me to",
	"the user wants to",
	"let me check",
	"let me run",
	"running from the project directory",
	"in this mode by default",
}

// narrativas internas em inglês do modelo (pensamento em voz alta antes da
// resposta útil) — presente em todos os vazamentos reais observados.
var internalNarrationSignals = []string{
	"the user wants me to",
	"the user wants to",
	"let me read it",
	"let me check",
	"let me look at",
	"let me run",
	"let me just read",
	"i should respond",
	"this is a simple",
	"the user has a question",
}

// palavras típicas de narrativa interna/instruções do SDK (em inglês).
// O HOK responde em PT-BR; uma resposta com densidade alta dessas palavras
// indica regurgitação do system prompt — camada estrutural que cobre
// variações não mapeadas (o vazamento muda a cada chamada).
var agentNarrationWords = []string{
	"against", "and", "available", "based", "brief", "channel", "checklist",
	"comparing", "control", "debug", "default", "demands", "described",
	"diff", "directly", "discussions", "exit", "explicit", "explore",
	"feedback", "file", "harness", "help", "include", "instructions",
	"internet", "invocation", "invoke", "known", "list", "local", "notes",
	"operation", "operate", "passing", "patterns", "permanent", "plan-mode",
	"prior", "problem", "produce", "project", "readiness", "recent",
	"recommend", "reminder", "report", "repository", "requires", "review",
	"security", "shorthand", "simple", "skill", "skills", "staged", "task",
	"tasks", "unstaged", "user wants", "vulnerability", "working diff",
	"working", "written", "ready",
}

// sinais fortes extras observados em vazamentos reais (16/08): frases de
// "skill invocation" do SDK que nunca aparecem numa resposta normal.
var systemPromptLeakStrong2 = []string{
	"as a skill invocation",
	"skills list",
	"is available as a skill",
	"local-tasks.txt",
	"the skills list",
}

// detectSystemPromptLeak verifica se o texto contém sinais de vazamento do
// system prompt do SDK. Estrategia em camadas:
//  1. sinais fortes: 1 basta (frases exclusivas do system prompt/narrativa)
//  2. sinais fracos + narrativa interna: 2+ distintos
//  3. estrutural: 2+ linhas no formato "- nome: descricao" (item de skill)
//  4. densidade de narrativa inglesa: 8+ ocorrências de palavras de
//     instrução/narrativa do SDK (cobre variações não mapeadas)
func detectSystemPromptLeak(text string) bool {
	lower := strings.ToLower(text)
	for _, s := range systemPromptLeakStrong {
		if strings.Contains(lower, s) {
			return true
		}
	}
	for _, s := range systemPromptLeakStrong2 {
		if strings.Contains(lower, s) {
			return true
		}
	}
	hits := 0
	for _, s := range systemPromptLeakSignals {
		if strings.Contains(lower, s) {
			hits++
		}
	}
	for _, s := range internalNarrationSignals {
		if strings.Contains(lower, s) {
			hits++
		}
	}
	// FIX 16/08 (opcao A): limiar subiu de 2 para 3+ para reduzir falso
	// positivo em respostas com 1-2 sinais fracos acidentais (ex: narrativa
	// "the user wants me to" + "let me check" em comando echo simples).
	// Respostas com 3+ sinais distintos ainda sao bloqueadas (vazamento real).
	if hits >= 3 {
		return true
	}
	// estrutural: 2+ linhas "- nome: descricao" (sem espaco no nome, sem **);
	// OU 1 linha com "/" no nome (skills internas do SDK sempre tem "/",
	// ex: /agents, /workflows, /codeflow) — sinal forte de lista vazada
	skillLines := 0
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if len(line) > 4 && strings.HasPrefix(line, "- ") {
			rest := line[2:]
			idx := strings.Index(rest, ":")
			if idx > 0 && idx < 40 && !strings.Contains(rest[:idx], "*") {
				hasSpace := strings.Contains(rest[:idx], " ")
				hasSlash := strings.Contains(rest[:idx], "/")
				if hasSlash {
					return true
				}
				if !hasSpace {
					skillLines++
				}
			}
		}
		if skillLines >= 2 {
			return true
		}
	}
	// densidade de narrativa inglesa de agente
	narrationHits := 0
	for _, w := range agentNarrationWords {
		if strings.Contains(lower, w) {
			narrationHits++
		}
	}
	return narrationHits >= 5
}

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
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return "", fmt.Errorf("claude code: erro ao abrir stdout: %v", pipeErr)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if startErr := cmd.Start(); startErr != nil {
		return "", fmt.Errorf("claude code: erro ao iniciar: %v — stderr: %s", startErr, stderr.String())
	}
	logTag := "claude_code_invoke:minimax-m3"
	if skipPermissions {
		logTag = "claude_code_invoke_approved:minimax-m3"
	}

	// FIX 16/08 (Opcao A): leitura incremental do stream com deteccao de
	// vazamento a cada chunk. Se a narrativa interna do SDK aparece, mata
	// o processo IMEDIATAMENTE e retorna errSystemPromptLeak — antes o
	// cmd.Run() esperava o CLI terminar inteiro (~100s+), estourando o
	// timeout do Cloudflare (524) sem nenhuma resposta util pro usuario.
	text, leaked, _ := processClaudeStream(stdout)
	if leaked {
		log.Printf("⚠️ claude_code: vazamento DETECTADO DURANTE stream, matando processo (%s)", logTag)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s system_prompt_leak_blocked_early", logTag),
		)
		return "", errSystemPromptLeak
	}
	runErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s timeout", logTag),
		)
		return "", fmt.Errorf("claude code: timeout apos %s", claudeCodeTimeout)
	}
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
		return "", fmt.Errorf("claude code: resposta vazia")
	}
	sqliteExecParams(
		`INSERT INTO logs (event, level, source) VALUES (?, 'INFO', 'claude_code_client');`,
		fmt.Sprintf("%s ok", logTag),
	)
	return text, nil
}

// processClaudeStream le linhas NDJSON do stream do CLI, acumula o texto
// do assistente e verifica vazamento de system prompt A CADA chunk.
// Retorna (textoAcumulado, vazou, err). Separado em funcao propria para
// permitir teste unitario dos dois cenarios (vazamento no meio do stream
// e stream limpo) sem invocar o binario claude.
func processClaudeStream(r io.Reader) (string, bool, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var out strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, `"type":"assistant"`) {
			continue
		}
		var event claudeStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
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
		if out.Len() > 0 && detectSystemPromptLeak(out.String()) {
			return out.String(), true, nil
		}
	}
	return out.String(), false, scanner.Err()
}

// === FASE 2b: Bloqueio de execucao sudo direta ===
func claudeCodeBlocked() error {
	return fmt.Errorf("EXECUCAO BLOQUEADA: uso de sudo direto foi desativado. Use o agent loop bash_exec (mutantTools) em vez de callClaudeCode direto. O comando sera roteado pelo gate de aprovacao com diff visual.")
}
