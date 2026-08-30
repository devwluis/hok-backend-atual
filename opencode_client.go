package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// opencodeBlocked indica que a acao de OpenCode foi bloqueada pela policy de seguranca
// (mesmo criterio do claude_code: sudo direto continua proibido mesmo no fluxo approved).
var opencodeBlocked = errors.New("opencode: comando bloqueado pela politica de seguranca (sudo proibido)")

// opencodeSessionStore guarda o sessionID do OpenCode por conversa/tenant, garantindo
// isolamento entre conversas diferentes (mesmo padrao do pendingActionMap).
var (
	opencodeSessionMu sync.RWMutex
	opencodeSession   = map[string]string{} // chave: "tenantID:userID:convId" -> sessionID
)

// opencodeSessionKey gera a chave de isolamento por conversa/tenant (mesmo padrao pendingActionMap).
func opencodeSessionKey(convId, tenantID, userID string) string {
	return convId + ":" + tenantID + ":" + userID
}

// getOpenCodeSession devolve o sessionID persistido para a conversa ("" se nova).
func getOpenCodeSession(convId, tenantID, userID string) string {
	key := opencodeSessionKey(convId, tenantID, userID)
	opencodeSessionMu.RLock()
	defer opencodeSessionMu.RUnlock()
	return opencodeSession[key]
}

// setOpenCodeSession persiste o sessionID para a conversa (isolamento por tenant:user:conv).
func setOpenCodeSession(convId, tenantID, userID, sessionID string) {
	if sessionID == "" {
		return
	}
	key := opencodeSessionKey(convId, tenantID, userID)
	opencodeSessionMu.Lock()
	opencodeSession[key] = sessionID
	opencodeSessionMu.Unlock()
}

// openCodeEvent representa um evento do stream JSON do `opencode run --format json`.
// Campos relevantes: type (step_start, text, ...), sessionID, text.
type openCodeEvent struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID"`
	Text      string          `json:"text"`
	Part      openCodePart    `json:"part"`
	Error     json.RawMessage `json:"error"`
}

type openCodePart struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Tool string `json:"tool"`
}

// opencodeCLIArgs monta os argumentos do CLI opencode (paralelo a claudeCLIArgs).
// Usa --format json para parsear o stream e --dir para isolamento de diretorio.
func opencodeCLIArgs(prompt string, skipPermissions bool, sessionID string, model string, planMode bool) []string {
	args := []string{"run", prompt, "--format", "json", "--dir", opencodeWorkdir}
	if planMode {
		// GATE PLAN (28/08): agente 'plan' do config do projeto — todas as
		// permissoes negadas (deny) — o opencode NÃO executa nenhuma tool,
		// apenas descreve o plano.
		args = append(args, "--agent", "plan")
	}
	if sessionID != "" {
		args = append(args, "--session", sessionID)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if skipPermissions {
		args = append(args, "--auto")
	}
	return args
}

// opencodeModelID normaliza o id do catalogo HOK para o formato que o CLI
// opencode entende. Catalogo vem de duas fontes:
//   - OpenRouter: ids como "deepseek/deepseek-chat-v3.1" — o CLI exige o
//     prefixo de provider "openrouter/";
//   - OpenCode Zen: ids simples como "claude-opus-5" — o CLI exige "opencode/".
//
// Todo modelo do catalogo HOK pertence a um desses dois providers (inclusive
// os fallbacks globais como "google/gemini-2.5-flash", que sao ids do
// OpenRouter). Por isso, id com "/" SEMPRE vira "openrouter/<id>", e id
// simples vira "opencode/<id>".
func opencodeModelID(m string) string {
	m = strings.TrimSpace(m)
	if m == "" || m == "auto" {
		return ""
	}
	if strings.HasPrefix(m, "openrouter/") || strings.HasPrefix(m, "opencode/") || strings.HasPrefix(m, "opencode-go/") {
		return m
	}
	if strings.Contains(m, "/") {
		return "openrouter/" + m
	}
	return "opencode/" + m
}

// opencodeBinaryPath retorna o caminho do binario, validando existencia.
func opencodeBinaryPath() (string, error) {
	bin := opencodeBinary
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("opencode: binario nao encontrado em %s", bin)
	}
	return bin, nil
}

func opencodeBlockedErr() error { return opencodeBlocked }

// callOpenCode — fluxo direto (sem aprovacao), equivalente a callClaudeCode.
// Usa modelA por padrao e faz fallback automatico para modelB em caso de erro recuperavel.
func callOpenCode(ctx context.Context, prompt string, convId, tenantID, userID string) (string, error) {
	prompt = ensureInlineContent(prompt)
	out, err := runOpenCodeCLI(ctx, prompt, false, convId, tenantID, userID, opencodeModelID(getActiveModel()), false)
	if err == nil {
		return out, nil
	}
	if isRecoverableOpenCodeError(err) {
		log.Printf("⚠️ opencode modelA falhou (%v) — reexecutando com modelB=%s", err, ModelB)
		return runOpenCodeCLI(ctx, prompt, false, convId, tenantID, userID, opencodeModelID(ModelB), false)
	}
	return "", err
}

// buildOpenCodePrompt monta o prompt passado ao CLI opencode (paralelo a buildClaudeCodePrompt).
func buildOpenCodePrompt(msg string, req ClientRequest) string {
	return msg
}

// callOpenCodeApproved — fluxo aprovado (gate de permissao), equivalente a callClaudeCodeApproved.
// Usa --auto (equivale a --dangerously-skip-permissions do claude). Fallback automatico
// para modelB em caso de erro recuperavel (mantem sessao -> contexto preservado).
func callOpenCodeApproved(ctx context.Context, prompt string, convId, tenantID, userID string) (string, error) {
	prompt = ensureInlineContent(prompt)
	out, err := runOpenCodeCLI(ctx, prompt, true, convId, tenantID, userID, opencodeModelID(getActiveModel()), false)
	if err == nil {
		return out, nil
	}
	if isRecoverableOpenCodeError(err) {
		log.Printf("[opencode] modelA falhou (%v) — reexecutando com modelB, sessao preservada", err)
		return runOpenCodeCLI(ctx, prompt, true, convId, tenantID, userID, opencodeModelID(ModelB), false)
	}
	return "", err
}

// callOpenCodeAutonomous — GATE AUTÔNOMO (29/08): mesmo --auto do fluxo
// aprovado, mas sem pendência — a blocklist Hokma e o budget (decisões
// 2/4) são validados ANTES pelo caller. Fallback para modelB preserva a
// sessão.
func callOpenCodeAutonomous(ctx context.Context, prompt string, convId, tenantID, userID string) (string, error) {
	prompt = ensureInlineContent(prompt)
	out, err := runOpenCodeCLI(ctx, prompt, true, convId, tenantID, userID, opencodeModelID(getActiveModel()), false)
	if err == nil {
		return out, nil
	}
	if isRecoverableOpenCodeError(err) {
		log.Printf("[opencode] autônomo modelA falhou (%v) — reexecutando com modelB, sessao preservada", err)
		return runOpenCodeCLI(ctx, prompt, true, convId, tenantID, userID, opencodeModelID(ModelB), false)
	}
	return "", err
}

// isRecoverableOpenCodeError decide quando troca pro fallback de modelo (modelA -> modelB).
// Timeout/erro de exit/vazio/429 sao recuperaveis; erros de seguranca (blocked) nao.
func isRecoverableOpenCodeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "resposta vazia") ||
		strings.Contains(msg, "exit error") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate")
}

// processOpenCodeJSONStream le eventos NDJSON do stream do CLI, acumula o
// texto do assistente e devolve o texto final e o sessionID (se houver).
// Retorna (textoAcumulado, sessionID, err).
func processOpenCodeJSONStream(r io.Reader) (string, string, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var out strings.Builder
	var sessionID string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev openCodeEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if ev.Type == "text" && ev.Text != "" {
			out.WriteString(ev.Text)
		}
		if ev.Type == "text" && ev.Text == "" && ev.Part.Text != "" {
			out.WriteString(ev.Part.Text)
		}
		if ev.SessionID != "" {
			sessionID = ev.SessionID
		}
	}
	if err := scanner.Err(); err != nil {
		return out.String(), "", fmt.Errorf("scanner error: %v", err)
	}
	return out.String(), sessionID, nil
}

// runOpenCodeCLI executa o CLI opencode de forma nao-interativa (modo run), com
// timeout, auditoria e parse do stream JSON. Estrutura paralela a runClaudeCodeCLI.
// parentCtx é o request context — quando o cliente HTTP desconecta, ctx.Done()
// dispara e o exec.CommandContext mata o opencode CLI em vez de deixar o job
// rodando até o fim (ORPHAN KILL 29/08).
func runOpenCodeCLI(parentCtx context.Context, prompt string, skipPermissions bool, convId, tenantID, userID, model string, planMode bool) (string, error) {
	// FASE 2b: sudo direto continua proibido mesmo no fluxo approved.
	if skipPermissions && strings.Contains(strings.ToLower(prompt), "sudo") {
		return "", opencodeBlockedErr()
	}

	bin, err := opencodeBinaryPath()
	if err != nil {
		return "", err
	}

	// Tenta reutilizar o sessionID existente para esta conversa/tenant/user.
	existingSid := getOpenCodeSession(convId, tenantID, userID)
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, opencodeTimeout)
	defer cancel()

	args := opencodeCLIArgs(prompt, skipPermissions, existingSid, model, planMode)
	cmd := exec.CommandContext(ctx, bin, args...)
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return "", fmt.Errorf("opencode: erro ao abrir stdout: %v", pipeErr)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if startErr := cmd.Start(); startErr != nil {
		return "", fmt.Errorf("opencode: erro ao iniciar: %v — stderr: %s", startErr, stderr.String())
	}

	logTag := "opencode_invoke:" + activeModelTag()
	if skipPermissions {
		logTag = "opencode_invoke_approved:" + activeModelTag()
	}

	text, sessionID, err := processOpenCodeJSONStream(stdout)
	if err != nil {
		_ = cmd.Wait()
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'opencode_client');`,
			logTag+" stream_error: "+err.Error(),
		)
		return "", fmt.Errorf("opencode: erro no stream: %v", err)
	}

	// Persiste o sessionID se for novo.
	setOpenCodeSession(convId, tenantID, userID, sessionID)

	runErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'opencode_client');`,
			logTag+" timeout",
		)
		return "", fmt.Errorf("opencode: timeout apos %s", opencodeTimeout)
	}
	if runErr != nil && text == "" {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'opencode_client');`,
			logTag+" fail: "+stderr.String(),
		)
		return "", fmt.Errorf("opencode: exit error: %v — stderr: %s", runErr, stderr.String())
	}
	if text == "" {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'opencode_client');`,
			logTag+" empty",
		)
		return "", fmt.Errorf("opencode: resposta vazia")
	}
	sqliteExecParams(
		`INSERT INTO logs (event, level, source) VALUES (?, 'INFO', 'opencode_client');`,
		logTag+" ok",
	)
	return text, nil
}

// callOpenCodePlan — GATE PLAN (28/08): roda o CLI opencode com o agente
// "plan" (permissões deny no config do projeto) — o opencode NÃO executa
// tools, apenas descreve o plano. Nunca usa --auto.
func callOpenCodePlan(ctx context.Context, prompt string, convId, tenantID, userID string) (string, error) {
	prompt = ensureInlineContent(prompt)
	return runOpenCodeCLI(ctx, prompt, false, convId, tenantID, userID, opencodeModelID(getActiveModel()), true)
}
