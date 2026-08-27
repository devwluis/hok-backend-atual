package main

// opencode_serve_flow.go — engine "opencode_serve" da cascata do chat
// (Etapa 3 inicial).
//
// Substitui o tryTerminalExec (PTY/tmux + marcador) como canal principal do
// Chat Web quando o `opencode serve` está no ar: a mensagem vai por API HTTP
// para a sessão persistente da conversa (conv_id → session_id, tabela
// session_mode) e a resposta volta como texto. Fallback: se o servidor está
// fora do ar (ou não aplicável), o helper retorna nil e a cascata segue para
// o tryTerminalExec legado — comportamento anterior preservado.
//
// Decisões aprovadas (27/08):
//   - gatilho: keyword de terminal OU forceOpenCode; exceção bridge ttyd
//     (sessão ttyd ativa → deixa o terminal visível cuidar);
//   - summarize: implementado, mas DESLIGADO por default
//     (OPENCODE_SERVE_AUTO_SUMMARIZE=1 para ativar — aguardando validação
//     em produção antes de automatizar);
//   - modelo: modelo ativo do Hokma no sendMessage (mesmo padrão dos outros
//     engines), não o default do servidor;
//   - sem replyPermission/SSE de tool approval nesta etapa (fica para 3b).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// tryOpenCodeServe — engine da cascata (posição do tryTerminalExec).
func tryOpenCodeServe(msg string, req ClientRequest, convId string, tenantID string, userID string) *smartTextResult {
	// Gatilho: mesma superfície do tryTerminalExec + forceOpenCode.
	if !(req.ForceOpenCode || containsTerminalKeyword(msg)) {
		return nil
	}
	// ETAPA A (27/08): a exceção §2.1 (bridge ttyd para terminal visível) foi
	// REMOVIDA — o Chat Web responde via opencode serve por padrão, mesmo com
	// a aba Terminal aberta no app. O terminal visível (TerminalTTYDScreen +
	// backend ttyd/tmux) permanece funcional para acesso manual/emergencial;
	// o tryTerminalExec abaixo na cascata segue como fallback de resiliência
	// quando o serve estiver fora do ar ou não configurado.
	// Fail closed: sem senha configurada o cliente não opera.
	if opencodeServePassword() == "" {
		return nil
	}
	c := newOpenCodeServeClient()
	if !opencodeServeHealthy(c) {
		log.Printf("[AUDIT] opencode_serve FORA DO AR conv=%s — cascata segue (tryTerminalExec legado)", convId)
		return nil
	}

	// Blocklist de segurança (mesmo princípio do tryTerminalExec).
	if cmd := extractTerminalCommand(msg); cmd != "" {
		lowerCmd := strings.ToLower(cmd)
		for _, bad := range terminalExecBlocklist {
			if strings.Contains(lowerCmd, bad) {
				log.Printf("[AUDIT] opencode_serve BLOQUEADO user=%s conv=%s cmd=%q ts=%s",
					userID, convId, cmd, time.Now().Format(time.RFC3339))
				return &smartTextResult{
					reply:  "⛔ Comando bloqueado por política de segurança: `" + cmd + "`.\nUse o terminal web para comandos dessa natureza.",
					mode:   "opencode_serve_blocked",
					engine: "opencode_serve",
				}
			}
		}
	}

	// Sessão persistente por conv_id (get-or-create; reabrir reutiliza).
	sessionID, _, err := getOrCreateOpenCodeServeSession(convId, tenantID, userID, openCodeServeSessionTitle(msg, convId), c)
	if err != nil {
		log.Printf("[AUDIT] opencode_serve SEM SESSAO conv=%s: %v — cascata segue", convId, err)
		return nil
	}

	opts := openCodeServeMessageOpts{
		System: openCodeServeSystemPrompt(req),
	}
	if model := opencodeModelID(getActiveModel()); model != "" && model != "auto" {
		opts.ProviderID, opts.ModelID = openCodeServeSplitModel(model)
	}

	// ETAPA B (27/08): o gate legado de aprovação (pending_action) sai do
	// caminho serve — as permissões de tool agora são decididas pela camada
	// nativa (permission.asked via SSE → once/reject automático, fail-safe).
	// O pending_action continua intacto nos caminhos legados (tryTerminalExec/
	// tryOpenCode) para quando o serve estiver fora do ar.

	// Mensagens que podem gerar tool calls vão pelo caminho async
	// (prompt_async + SSE) — o /message síncrono trava sem timeout quando há
	// permission pendente (achado da investigação da Etapa B). Mensagens
	// simples seguem síncronas.
	if openCodeServeNeedsTools(msg) {
		text, err := tryOpenCodeServeAsync(c, sessionID, msg, opts)
		if err != nil {
			log.Printf("[AUDIT] opencode_serve async FALHOU conv=%s: %v — cascata segue", convId, err)
			return nil
		}
		return &smartTextResult{reply: text, mode: "opencode_serve", engine: "opencode_serve"}
	}

	m, err := c.sendMessage(sessionID, msg, opts)
	if err != nil {
		log.Printf("[AUDIT] opencode_serve message FALHOU conv=%s: %v — cascata segue", convId, err)
		return nil
	}
	text := strings.TrimSpace(openCodeServeMessageText(m))
	if text == "" {
		log.Printf("[AUDIT] opencode_serve resposta VAZIA conv=%s — cascata segue", convId)
		return nil
	}
	maybeOpenCodeServeSummarize(c, sessionID)
	return &smartTextResult{reply: text, mode: "opencode_serve", engine: "opencode_serve"}
}

// openCodeServeSystemPrompt monta o system da mensagem: persona do chat +
// instrução de modo plan (sem garantia de execução — bug conhecido do modo
// plan, mesmo critério documentado no adendo de 24/08).
func openCodeServeSystemPrompt(req ClientRequest) string {
	sys := smartChatSystemPrompt()
	if req.Mode == "plan" {
		sys = "Você está em MODO PLANEJAMENTO: apenas analise e proponha o plano, NÃO execute comandos nem edite arquivos.\n\n" + sys
	}
	return sys
}

// openCodeServeSessionTitle gera o título da sessão a partir da mensagem.
func openCodeServeSessionTitle(msg, convId string) string {
	t := strings.TrimSpace(msg)
	if runes := []rune(t); len(runes) > 60 {
		t = string(runes[:60]) + "…"
	}
	if t == "" {
		return "conv " + convId
	}
	return t
}

// ─── Health check com cache curto (evita latência/spam por mensagem) ────────

var (
	opencodeServeHealthMu sync.Mutex
	opencodeServeHealthUp bool
	opencodeServeHealthAt time.Time
)

const opencodeServeHealthTTL = 15 * time.Second

func opencodeServeHealthy(c *opencodeServeClient) bool {
	opencodeServeHealthMu.Lock()
	defer opencodeServeHealthMu.Unlock()
	if time.Since(opencodeServeHealthAt) < opencodeServeHealthTTL {
		return opencodeServeHealthUp
	}
	up := false
	req, err := http.NewRequest("GET", c.baseURL+"/api/health", nil)
	if err == nil {
		req.SetBasicAuth(c.username, c.password)
		resp, err := c.http.Do(req)
		if err == nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			up = resp.StatusCode == http.StatusOK
		}
	}
	opencodeServeHealthUp = up
	opencodeServeHealthAt = time.Now()
	return up
}

// ─── Summarize automático (implementado, DESLIGADO por default) ─────────────
// Decisão aprovada: NÃO disparar por limiar de tokens nesta etapa. Ative com
// OPENCODE_SERVE_AUTO_SUMMARIZE=1 para validar comportamento antes de
// automatizar. O disparo é em goroutine — nunca bloqueia o reply.

func maybeOpenCodeServeSummarize(c *opencodeServeClient, sessionID string) {
	if os.Getenv("OPENCODE_SERVE_AUTO_SUMMARIZE") != "1" {
		return
	}
	providerID, modelID := openCodeServeSplitModel(opencodeModelID(getActiveModel()))
	if providerID == "" || modelID == "" {
		return
	}
	go func() {
		if err := c.summarizeSession(sessionID, providerID, modelID); err != nil {
			log.Printf("[AUDIT] opencode_serve summarize falhou session=%s: %v", sessionID, err)
		}
	}()
}

// resolveOpenCodeServePendingAction — executa a ação aprovada pelo usuário
// (fluxo de aprovação do Hokma) na sessão persistente da conversa.
func resolveOpenCodeServePendingAction(action *PendingAction, convId, tenantID, userID string) string {
	var args struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(action.ArgsJSON), &args); err != nil || strings.TrimSpace(args.Prompt) == "" {
		log.Printf("[AUDIT] opencode_serve fail-closed actionID=%s: prompt original indisponivel", action.ID)
		return "❌ A ação de opencode expirou ou o prompt original não está mais disponível. Refaça o pedido."
	}
	if opencodeServePassword() == "" {
		return "❌ opencode serve não configurado (OPENCODE_SERVE_PASSWORD ausente)."
	}
	c := newOpenCodeServeClient()
	if !opencodeServeHealthy(c) {
		return "❌ opencode serve fora do ar no momento. Tente novamente mais tarde."
	}
	sessionID, _, err := getOrCreateOpenCodeServeSession(convId, tenantID, userID, openCodeServeSessionTitle(args.Prompt, convId), c)
	if err != nil {
		return fmt.Sprintf("❌ Erro ao preparar a sessão: %v", err)
	}
	opts := openCodeServeMessageOpts{System: smartChatSystemPrompt()}
	if model := opencodeModelID(getActiveModel()); model != "" && model != "auto" {
		opts.ProviderID, opts.ModelID = openCodeServeSplitModel(model)
	}
	log.Printf("[AUDIT] opencode_serve aprovado actionID=%s prompt_len=%d conv=%s tenant=%s",
		action.ID, len(args.Prompt), convId, tenantID)
	m, err := c.sendMessage(sessionID, args.Prompt, opts)
	if err != nil {
		return fmt.Sprintf("❌ Erro ao executar no opencode serve: %v", err)
	}
	return openCodeServeMessageText(m)
}
// ─── ETAPA B: permissions nativas via SSE (decisão automática, sem UI) ──────
// O listener por sessão responde permission.asked do opencode serve:
//   - reject  para comandos da blocklist de segurança (terminalExecBlocklist);
//   - once    para comandos de baixo risco (leitura/echo — prefixos seguros);
//   - reject  por padrão (fail-safe) para tudo que não for claramente seguro
//     (ainda não há card de aprovação manual — Etapa B, sem UI).

var (
	serveWatcherMu sync.Mutex
	serveWatchers  = map[string]*openCodeServeWatcher{} // sessionID → watcher
)

type openCodeServeWatcher struct {
	sessionID string
	client    *opencodeServeClient
	cancel    context.CancelFunc
}

// ensureOpenCodeServeWatcher garante um listener SSE para a sessão (cria se
// não existir). O watcher reconecta sozinho se o stream cair e sai quando a
// sessão é encerrada (ctx cancelado) ou o stream falha de forma terminal.
func ensureOpenCodeServeWatcher(sessionID string, c *opencodeServeClient) {
	serveWatcherMu.Lock()
	defer serveWatcherMu.Unlock()
	if _, ok := serveWatchers[sessionID]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveWatchers[sessionID] = &openCodeServeWatcher{sessionID: sessionID, client: c, cancel: cancel}
	go func() {
		defer func() {
			serveWatcherMu.Lock()
			delete(serveWatchers, sessionID)
			serveWatcherMu.Unlock()
		}()
		for ctx.Err() == nil {
			err := c.eventStream(ctx, func(ev openCodeServeEvent) {
				if ev.Type != "permission.asked" {
					return
				}
				var props struct {
					ID         string                 `json:"id"`
					SessionID  string                 `json:"sessionID"`
					Permission string                 `json:"permission"`
					Patterns   []string               `json:"patterns"`
					Metadata   map[string]interface{} `json:"metadata"`
				}
				if err := json.Unmarshal(ev.Properties, &props); err != nil || props.ID == "" {
					return
				}
				if props.SessionID != sessionID {
					return
				}
				// metadata pode ter campos de tipos variados (ex.: directories/
				// patterns como arrays em external_directory) — extrai o command
				// como string quando existir.
				cmd := ""
				if c, ok := props.Metadata["command"].(string); ok {
					cmd = c
				}
				reply := decideOpenCodeServePermission(props.Permission, props.Patterns, cmd)
				log.Printf("[AUDIT] opencode_serve permission %s (%s %v) → %s", props.ID, props.Permission, props.Patterns, reply)
				if err := c.replyPermission(sessionID, props.ID, reply); err != nil {
					log.Printf("[AUDIT] opencode_serve replyPermission %s falhou: %v", props.ID, err)
				}
			})
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				log.Printf("[opencode_serve] watcher %s: erro no SSE: %v — reconectando", sessionID, err)
			} else {
				log.Printf("[opencode_serve] watcher %s: SSE caiu — reconectando", sessionID)
			}
			time.Sleep(3 * time.Second)
		}
	}()
}

// decideOpenCodeServePermission — regra automática da Etapa B.
func decideOpenCodeServePermission(permission string, patterns []string, cmdFromMetadata string) string {
	cmd := strings.ToLower(strings.TrimSpace(firstString(patterns, cmdFromMetadata)))
	if cmd == "" {
		return "reject" // fail-safe: sem descrição clara, não executa
	}
	for _, bad := range terminalExecBlocklist {
		if strings.Contains(cmd, bad) {
			return "reject"
		}
	}
	if isLowRiskShellCommand(cmd) {
		return "once"
	}
	return "reject" // fail-safe: sem card de aprovação ainda
}

// isLowRiskShellCommand — leitura/echo/inspeção sem efeito destrutivo.
func isLowRiskShellCommand(cmd string) bool {
	if cmd == "" {
		return false
	}
	for _, p := range lowRiskPrefixes {
		if cmd == p || strings.HasPrefix(cmd, p+" ") {
			return true
		}
	}
	return false
}

var lowRiskPrefixes = []string{
	"echo", "ls", "cat", "pwd", "whoami", "date", "printf", "head", "tail",
	"grep", "find", "wc", "hostname", "uptime", "df", "free", "ps", "env",
	"which", "type", "true", "false", "uname", "id", "getent",
}

func firstString(patterns []string, fallback string) string {
	if len(patterns) > 0 && patterns[0] != "" {
		return patterns[0]
	}
	return fallback
}

// openCodeServeNeedsTools — mensagens que podem disparar tool calls vão pelo
// caminho async (prompt_async + SSE + polling); as demais ficam síncronas.
func openCodeServeNeedsTools(msg string) bool {
	return needsRealTools(msg) || containsTerminalKeyword(msg)
}

// tryOpenCodeServeAsync — caminho para mensagens que podem gerar tool calls.
// ACHADO da investigação (binário 1.18.23): prompt_async/noReply=true NÃO
// inicia o processamento de forma confiável (0 eventos; o POST bloqueante
// inicia na hora). Por isso o caminho "async" envia via /message síncrono
// em GOROUTINE com timeout gerenciado — e o watcher SSE responde as
// permissions automaticamente (once/reject), o que elimina o travamento do
// POST bloqueante com permission pendente (o motivo original da migração).
const openCodeServeAsyncTimeout = 240 * time.Second

func tryOpenCodeServeAsync(c *opencodeServeClient, sessionID, msg string, opts openCodeServeMessageOpts) (string, error) {
	ensureOpenCodeServeWatcher(sessionID, c)
	type result struct {
		m   *openCodeServeMessage
		err error
	}
	ch := make(chan result, 1)
	go func() {
		m, err := c.sendMessage(sessionID, msg, opts)
		ch <- result{m, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
		text := strings.TrimSpace(openCodeServeMessageText(r.m))
		if text == "" {
			// Ferramenta negada pela política: alguns modelos terminam sem
			// texto após o reject — devolve aviso claro em vez de cascata.
			if openCodeServeMessageHasTool(r.m) {
				return "⛔ A ação solicitada foi bloqueada pela política de segurança (permissão negada).", nil
			}
			return "", fmt.Errorf("opencode serve: resposta vazia (tool negada pela política)")
		}
		return text, nil
	case <-time.After(openCodeServeAsyncTimeout):
		return "", fmt.Errorf("opencode serve: timeout aguardando resposta (apos %s)", openCodeServeAsyncTimeout)
	}
}

// openCodeServeMessageHasTool — true se a mensagem contém partes de tool
// (ex.: tool negada pela política → o modelo termina sem texto).
func openCodeServeMessageHasTool(m *openCodeServeMessage) bool {
	for _, p := range m.Parts {
		if p.Type == "tool" {
			return true
		}
	}
	return false
}
