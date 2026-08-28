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
	if req.Mode == "plan" {
		// GATE PLAN (28/08) camada 1 (serve): usa o agente "plan" do config do
		// projeto (permissões deny) — o servidor NEGA toda tool sem pedir.
		// Camada 2: o watcher também responde reject em modo plan.
		opts.Agent = "plan"
		openCodeServeSetPlanMode(sessionID, true)
	} else {
		openCodeServeSetPlanMode(sessionID, false)
	}
	// GATE AUTÔNOMO (29/08): o serve roda com o agente normal (build); o
	// watcher auto-aprova (once) tudo que não for blocklist (camada 2).
	openCodeServeSetAutonomousMode(sessionID, req.Mode == "autonomous")

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
		text, card, err := tryOpenCodeServeAsync(c, sessionID, msg, opts, convId, tenantID, userID)
		if err != nil {
			log.Printf("[AUDIT] opencode_serve async FALHOU conv=%s: %v — cascata segue", convId, err)
			return nil
		}
		if card {
			return &smartTextResult{reply: text, mode: "opencode_serve_pending", engine: "opencode_serve"}
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
// ─── ETAPA B: permissions nativas via SSE (once/reject automático + CARD) ──
// O listener por sessão responde permission.asked do opencode serve:
//   - reject para comandos da blocklist de segurança (terminalExecBlocklist);
//   - once para comandos de baixo risco (leitura/echo — prefixos seguros);
//   - CARD de aprovação do usuário para o resto (pending_action
//     "opencode_serve_perm") — a resposta do usuário decide via
//     POST /session/{id}/permissions/{id}; sem resposta, o TTL
//     (openCodeServeCardTTL) rejeita automaticamente (a sessão volta a idle).

type openCodeServePermDecision int

const (
	permAutoOnce openCodeServePermDecision = iota
	permAutoReject
	permAskUser
)

// openCodeServePermissionAsked — dados da permission capturada no SSE.
type openCodeServePermissionAsked struct {
	ID         string   `json:"id"`
	SessionID  string   `json:"sessionID"`
	Permission string   `json:"permission"`
	Patterns   []string `json:"patterns"`
	Command    string   `json:"command"`
}

// openCodeServeCardTTL — tempo máximo do card de aprovação aguardando o
// usuário. CONSTANTE ÚNICA (ajustável sem reabrir a implementação). Se
// expirar sem resposta, a permission é rejeitada automaticamente.
const openCodeServeCardTTL = 120 * time.Second

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
				asked := openCodeServePermissionAsked{
					ID:         props.ID,
					SessionID:  props.SessionID,
					Permission: props.Permission,
					Patterns:   props.Patterns,
					Command:    cmd,
				}
				// GATE PLAN camada 2: em modo plan, TODA permission é negada
				// (o modelo só descreve; qualquer tentativa de tool falha).
				if openCodeServeIsPlanMode(sessionID) {
					log.Printf("[AUDIT] opencode_serve permission %s (%s %v) → reject (modo plan)", props.ID, props.Permission, props.Patterns)
					_ = c.replyPermission(sessionID, props.ID, "reject")
					return
				}
				// GATE AUTÔNOMO (29/08) camada 2: em modo autônomo, TODA
				// permission não-bloqueada é auto-aprovada (once) — a
				// blocklist do serve (permAutoReject) continua rejeitando.
				// A blocklist Hokma do prompt barra ANTES (no tryOpenCodeServe);
				// budget/circuit breaker/auditoria ficam no Hokma.
				if openCodeServeIsAutonomousMode(sessionID) {
					if decideOpenCodeServePermission(props.Permission, props.Patterns, cmd) == permAutoReject {
						log.Printf("[AUDIT] opencode_serve permission %s (%s %v) → reject (blocklist, modo autônomo)", props.ID, props.Permission, props.Patterns)
						_ = c.replyPermission(sessionID, props.ID, "reject")
					} else {
						log.Printf("[AUDIT] opencode_serve permission %s (%s %v) → once (modo autônomo)", props.ID, props.Permission, props.Patterns)
						_ = c.replyPermission(sessionID, props.ID, "once")
					}
					return
				}
				switch decideOpenCodeServePermission(props.Permission, props.Patterns, cmd) {
				case permAutoOnce:
					log.Printf("[AUDIT] opencode_serve permission %s (%s %v) → once", props.ID, props.Permission, props.Patterns)
					_ = c.replyPermission(sessionID, props.ID, "once")
				case permAutoReject:
					log.Printf("[AUDIT] opencode_serve permission %s (%s %v) → reject (blocklist)", props.ID, props.Permission, props.Patterns)
					_ = c.replyPermission(sessionID, props.ID, "reject")
				case permAskUser:
					// Aprovação recente do usuário na mesma execução → once
					// automático (sem novo card).
					if openCodeServeRecentlyApproved(sessionID) {
						log.Printf("[AUDIT] opencode_serve permission %s (%s %v) → once (aprovação recente)", props.ID, props.Permission, props.Patterns)
						_ = c.replyPermission(sessionID, props.ID, "once")
						return
					}
					openCodeServeAskUser(c, asked)
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
func decideOpenCodeServePermission(permission string, patterns []string, cmdFromMetadata string) openCodeServePermDecision {
	cmd := strings.ToLower(strings.TrimSpace(firstString(patterns, cmdFromMetadata)))
	if cmd == "" {
		return permAskUser // sem descrição clara → card
	}
	for _, bad := range terminalExecBlocklist {
		if strings.Contains(cmd, bad) {
			return permAutoReject
		}
	}
	if isLowRiskShellCommand(cmd) {
		return permAutoOnce
	}
	return permAskUser // card de aprovação (antes era reject fail-safe)
}

// openCodeServeSessionOwner — resolve conv/tenant/user da sessão serve via
// session_mode (necessário para criar a pendência de aprovação do chat).
func openCodeServeSessionOwner(sessionID string) (convID, tenantID, userID string, ok bool) {
	err := db.QueryRow(
		`SELECT conv_id, tenant_id, user_id FROM session_mode WHERE opencode_session_id = ?`,
		sessionID,
	).Scan(&convID, &tenantID, &userID)
	if err != nil {
		return "", "", "", false
	}
	return convID, tenantID, userID, true
}

// ─── Card de aprovação ───────────────────────────────────────────────────────
// Canal por sessão: o watcher avisa o async (aguardando a resposta) que um
// card foi criado — o async devolve "Confirma? (responda sim/nao)" na hora,
// e a execução da tool fica em background até a aprovação/TTL.

var (
	serveCardMu    sync.Mutex
	serveCardChans = map[string]chan string{} // sessionID → chan do reply do card
	serveApproved  = map[string]time.Time{}   // sessionID → última aprovação (recent-approve)
)

// openCodeServeRecentApproveTTL — janela em que, após o usuário aprovar um
// card, as permissions subsequentes da MESMA execução são respondidas "once"
// automaticamente (ex.: mkdir pede external_directory e depois bash — a
// segunda permission não deve exigir um segundo card).
const openCodeServeRecentApproveTTL = 90 * time.Second

// openCodeServeRecentlyApproved — true se o usuário aprovou um card desta
// sessão recentemente (dentro da janela).
func openCodeServeRecentlyApproved(sessionID string) bool {
	serveCardMu.Lock()
	defer serveCardMu.Unlock()
	t, ok := serveApproved[sessionID]
	return ok && time.Since(t) < openCodeServeRecentApproveTTL
}

// openCodeServeMarkApproved — registra a aprovação do usuário na sessão.
func openCodeServeMarkApproved(sessionID string) {
	serveCardMu.Lock()
	serveApproved[sessionID] = time.Now()
	serveCardMu.Unlock()
}

func openCodeServeCardChan(sessionID string) chan string {
	serveCardMu.Lock()
	defer serveCardMu.Unlock()
	ch := serveCardChans[sessionID]
	if ch == nil {
		ch = make(chan string, 1)
		serveCardChans[sessionID] = ch
	}
	return ch
}

// openCodeServeAskUser — cria a pendência de aprovação no chat e sinaliza o
// async aguardando. Sem dono conhecido (sessão órfã) → reject fail-safe.
func openCodeServeAskUser(c *opencodeServeClient, asked openCodeServePermissionAsked) {
	convID, tenantID, userID, ok := openCodeServeSessionOwner(asked.SessionID)
	if !ok {
		log.Printf("[AUDIT] opencode_serve permission %s sem dono — reject", asked.ID)
		_ = c.replyPermission(asked.SessionID, asked.ID, "reject")
		return
	}
	desc := strings.TrimSpace(asked.Permission + ": " + asked.Command)
	argsJSON, _ := json.Marshal(asked)
	pa := setPendingAction(convID, tenantID, userID, "opencode_serve_perm", string(argsJSON), desc)
	log.Printf("[AUDIT] opencode_serve permission %s (%s) → CARD (TTL %s)", asked.ID, desc, openCodeServeCardTTL)
	select {
	case openCodeServeCardChan(asked.SessionID) <- desc + "\n\nConfirma? (responda sim/nao)":
	default: // ninguém aguardando nesta sessão agora — o TTL resolve
	}
	time.AfterFunc(openCodeServeCardTTL, func() {
		// Rejeita SÓ se ESTA pendência (mesmo actionID) ainda estiver ativa —
		// se o usuário respondeu (consumida) ou outra pendência a sobrescreveu
		// (nova permission), não rejeita esta (a nova tem o próprio TTL).
		if pa != nil {
			if cur := getPendingAction(convID, tenantID, userID); cur != nil && cur.ID == pa.ID {
				log.Printf("[AUDIT] opencode_serve permission %s TTL expirado — reject automático", asked.ID)
				_ = c.replyPermission(asked.SessionID, asked.ID, "reject")
			}
		}
	})
}

// openCodeServeWaitResult — polling do histórico até a resposta do assistente
// completar (usado pelo resolver do card após aprovar: a tool executa em
// background e o texto final é devolvido na resposta da aprovação).
func openCodeServeWaitResult(c *opencodeServeClient, sessionID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(1 * time.Second)
		msgs, err := c.getMessages(sessionID)
		if err != nil || len(msgs) == 0 {
			continue
		}
		m := msgs[len(msgs)-1]
		if m.Info.Role == "assistant" && openCodeServeMessageFinished(&m) {
			text := strings.TrimSpace(openCodeServeMessageText(&m))
			if text == "" {
				return "executado (o modelo não retornou texto)", nil
			}
			return text, nil
		}
	}
	return "", fmt.Errorf("timeout aguardando o resultado da ação")
}

// openCodeServeMessageFinished — true quando a mensagem do assistente chegou
// ao fim (step-finish nos parts ou timestamp de conclusão preenchido).
func openCodeServeMessageFinished(m *openCodeServeMessage) bool {
	if m.Info.Time.Completed > 0 {
		return true
	}
	for _, p := range m.Parts {
		if p.Type == "step-finish" {
			return true
		}
	}
	return false
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

// ─── Sessão "zumbi" pós-TTL (pendência 1, 27/08) ────────────────────────────
// Após o TTL do card rejeitar uma permission, a sessão pode passar a
// responder VAZIO (step-finish sem texto e sem tool) — comportamento do
// modelo/servidor. Detecção automática: N respostas vazias consecutivas na
// mesma sessão → recria a sessão (DELETE em session_mode + sessão nova) e
// reenvia a mensagem 1x. A sessão antiga fica órfã no serve (inofensiva).

var (
	serveZombieMu sync.Mutex
	serveZombie   = map[string]int{} // sessionID → respostas vazias consecutivas
)

// openCodeServeZombieThreshold — nº de respostas vazias consecutivas que
// considera a sessão zumbi e a recria. Constante única (ajustável).
const openCodeServeZombieThreshold = 2

func openCodeServeNoteEmpty(sessionID string) int {
	serveZombieMu.Lock()
	defer serveZombieMu.Unlock()
	serveZombie[sessionID]++
	return serveZombie[sessionID]
}

func openCodeServeNoteOk(sessionID string) {
	serveZombieMu.Lock()
	defer serveZombieMu.Unlock()
	delete(serveZombie, sessionID)
}

// openCodeServeTryRecreate — recria a sessão zumbi e reenvia a mensagem 1x.
// Devolve (texto, ok): ok=true quando o reenvio na sessão nova respondeu.
func openCodeServeTryRecreate(c *opencodeServeClient, sessionID, convID, tenantID, userID, msg string, opts openCodeServeMessageOpts) (string, bool) {
	log.Printf("[AUDIT] opencode_serve sessão ZUMBI detectada (%s) — recriando", sessionID)
	openCodeServeNoteOk(sessionID)
	clearOpenCodeServeSessionID(convID, tenantID, userID)
	newSID, _, err := getOrCreateOpenCodeServeSession(convID, tenantID, userID, openCodeServeSessionTitle(msg, convID), c)
	if err != nil || newSID == sessionID {
		log.Printf("[AUDIT] opencode_serve recriação falhou: %v", err)
		return "", false
	}
	log.Printf("[AUDIT] opencode_serve sessão recriada %s → %s", sessionID, newSID)
	m2, err := c.sendMessage(newSID, msg, opts)
	if err != nil {
		return "", false
	}
	t2 := strings.TrimSpace(openCodeServeMessageText(m2))
	if t2 == "" {
		return "", false
	}
	return t2, true
}

// tryOpenCodeServeAsync — caminho para mensagens que podem gerar tool calls.
// ACHADO da investigação (binário 1.18.23): prompt_async/noReply=true NÃO
// inicia o processamento de forma confiável (0 eventos; o POST bloqueante
// inicia na hora). Por isso o caminho "async" envia via /message síncrono
// em GOROUTINE com timeout gerenciado — e o watcher SSE responde as
// permissions automaticamente (once/reject), o que elimina o travamento do
// POST bloqueante com permission pendente (o motivo original da migração).
const openCodeServeAsyncTimeout = 240 * time.Second

func tryOpenCodeServeAsync(c *opencodeServeClient, sessionID, msg string, opts openCodeServeMessageOpts, convID, tenantID, userID string) (text string, card bool, err error) {
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
	cardCh := openCodeServeCardChan(sessionID)
	select {
	case r := <-ch:
		if r.err != nil {
			return "", false, r.err
		}
		text := strings.TrimSpace(openCodeServeMessageText(r.m))
		if text == "" {
			// Ferramenta negada pela política: alguns modelos terminam sem
			// texto após o reject — devolve aviso claro em vez de cascata
			// (NÃO é sessão zumbi — a tool foi negada de propósito).
			if openCodeServeMessageHasTool(r.m) {
				return "⛔ A ação solicitada foi bloqueada pela política de segurança (permissão negada).", false, nil
			}
			// Resposta vazia SEM tool: possível sessão zumbi pós-TTL.
			if openCodeServeNoteEmpty(sessionID) >= openCodeServeZombieThreshold {
				if t2, ok := openCodeServeTryRecreate(c, sessionID, convID, tenantID, userID, msg, opts); ok {
					return t2, false, nil
				}
				return "", false, fmt.Errorf("opencode serve: sessão recriada, mas o modelo ainda responde vazio — reenvie a mensagem")
			}
			return "", false, fmt.Errorf("opencode serve: resposta vazia (tool negada pela política)")
		}
		openCodeServeNoteOk(sessionID)
		return text, false, nil
	case reply := <-cardCh:
		// Card criado pelo watcher: devolve a pergunta; a tool continua em
		// background até a aprovação/TTL — o resolver do card devolve o
		// resultado na resposta da aprovação.
		serveCardMu.Lock()
		delete(serveCardChans, sessionID)
		serveCardMu.Unlock()
		return reply, true, nil
	case <-time.After(openCodeServeAsyncTimeout):
		// Rede de segurança: aborta o processamento pendente para a sessão
		// não ficar busy com tool pendente.
		if err := c.abortSession(sessionID); err != nil {
			log.Printf("[opencode_serve] abort apos timeout falhou: %v", err)
		}
		return "", false, fmt.Errorf("opencode serve: timeout aguardando resposta (apos %s)", openCodeServeAsyncTimeout)
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

// ─── GATE PLAN — camada 2 (serve) ────────────────────────────────────────────
// Em modo plan, o watcher responde "reject" para QUALQUER permission.asked
// (defesa em profundidade — mesmo que o agente "plan" falhe em negar, nada
// executa). O modo é marcado por sessão pelo tryOpenCodeServe.

var (
	servePlanModeMu sync.Mutex
	servePlanMode   = map[string]bool{} // sessionID → em modo plan
)

func openCodeServeSetPlanMode(sessionID string, plan bool) {
	servePlanModeMu.Lock()
	defer servePlanModeMu.Unlock()
	if plan {
		servePlanMode[sessionID] = true
	} else {
		delete(servePlanMode, sessionID)
	}
}

func openCodeServeIsPlanMode(sessionID string) bool {
	servePlanModeMu.Lock()
	defer servePlanModeMu.Unlock()
	return servePlanMode[sessionID]
}

// GATE AUTÔNOMO (29/08): em modo autônomo o watcher auto-aprova (once)
// TODA permission que não for blocklist do serve — o espelho do plan.
// A blocklist Hokma do prompt (terminalExecBlocklist) barra antes, no
// tryOpenCodeServe; o budget/circuit breaker/auditoria ficam no Hokma.

var (
	serveAutonomousModeMu sync.Mutex
	serveAutonomousMode   = map[string]bool{} // sessionID → em modo autônomo
)

func openCodeServeSetAutonomousMode(sessionID string, autonomous bool) {
	serveAutonomousModeMu.Lock()
	defer serveAutonomousModeMu.Unlock()
	if autonomous {
		serveAutonomousMode[sessionID] = true
	} else {
		delete(serveAutonomousMode, sessionID)
	}
}

func openCodeServeIsAutonomousMode(sessionID string) bool {
	serveAutonomousModeMu.Lock()
	defer serveAutonomousModeMu.Unlock()
	return serveAutonomousMode[sessionID]
}
