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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
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
	// Exceção aprovada (§2.1): sessão ttyd REGISTRADA pelo frontend ativa →
	// a ponte visível do terminal cuida (tryTerminalExec abaixo na cascata) —
	// o comando deve aparecer no terminal do usuário. Usa apenas o registro
	// do frontend (request + tabela terminal_active); NÃO o fallback tmux ls
	// do registeredActiveTTYD (uma sessão tmux existente no servidor não
	// significa terminal visível no app).
	if req.TerminalSession != "" {
		if s := resolveTmuxSession(req.TerminalSession); s != "" {
			if exec.Command("tmux", "has-session", "-t", s).Run() == nil {
				return nil
			}
		}
	}
	if active := loadTerminalActive(); active != "" {
		if exec.Command("tmux", "has-session", "-t", active).Run() == nil {
			return nil
		}
	}
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

	// Prompts destrutivos: fluxo de aprovação do Hokma (mesmo padrão do
	// tryOpenCode) — a execução só acontece após "sim" do usuário.
	if promptNeedsApproval(msg) && !promptContainsOnlyReadOnlyCommands(msg) {
		argsJSON, _ := json.Marshal(map[string]string{"prompt": msg})
		desc := describeOpenCodeAction(msg)
		setPendingAction(convId, tenantID, userID, "opencode_serve", string(argsJSON), desc)
		return &smartTextResult{
			reply:  desc + "\n\nConfirma? (responda sim/nao)",
			mode:   "opencode_serve_pending",
			engine: "opencode_serve",
		}
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