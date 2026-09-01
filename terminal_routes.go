package main

// ─────────────────────────────────────────────────────────────────────────
// Terminal via ttyd (22/08 — substituição do terminal in-app instável):
//   - ttyd como serviço systemd local (hok-terminal.service), escutando
//     SOMENTE em 127.0.0.1:7681;
//   - Cloudflare Tunnel (terminal.imoveischaves.com) aponta para o BACKEND
//     (:8082), NÃO para o ttyd direto — este módulo valida o token efêmero
//     e faz proxy reverso (incluindo WebSocket) para o ttyd. Expor o ttyd
//     direto deixaria um shell root público; o proxy é a camada de segurança.
//   - Tokens: tabela SQLite `terminal_tokens` (sqliteExecParams), TTL 5 min.
//
// Fluxo: frontend POST /terminal/token (owner-only) → recebe URL
// https://terminal.imoveischaves.com/?token=X → iframe abre → backend valida
// token na entrada ("/") e nos upgrades WebSocket (cookie de sessão cobre os
// demais requests); assets estáticos do ttyd são públicos por natureza.
// ─────────────────────────────────────────────────────────────────────────

import (
	"compress/gzip"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	terminalTokenTTL  = 5 * time.Minute
	terminalPublicURL = "https://terminal.imoveischaves.com"
	// BUG bold (23/08): fonte do cliente xterm com bold REAL (JetBrains Mono
	// 400+700), servida pelo backend e registrada via @font-face injetado no
	// HTML do ttyd — a pilha default (Consolas/Menlo/monospace) cai, no
	// Android, numa fonte sem face bold → SGR 1 renderiza com peso normal.
	fontBaseURL    = "https://api.imoveischaves.com"
	ttydUpstream   = "http://127.0.0.1:7681"
	termCookieName = "hok_term_tok"
	// TESTE 1 — teclado estendido: o ttyd roda attached a esta sessão tmux;
	// teclas da barra overlay são injetadas via tmux send-keys.
	tmuxTtydName = "hok-ttyd"
)

// TESTE C (23/08) — multi-abas: cada aba tem sessão tmux própria
// (hok-ttyd legado ou hok-terminal-<id>). Toda rota que injeta na sessão
// aceita "session" no body; default mantém compatibilidade com hok-ttyd.
var tmuxSessionRe = regexp.MustCompile(`^hok-(ttyd|terminal-[A-Za-z0-9_-]{1,32})$`)

// resolveTmuxSession valida o nome de sessão informado (whitelist estrita)
// ou devolve o default legado quando vazio.
func resolveTmuxSession(s string) string {
	if s == "" {
		return tmuxTtydName
	}
	if len(s) > 48 || !tmuxSessionRe.MatchString(s) {
		return ""
	}
	return s
}

// tmuxKeyRe: formato aceito pelo tmux send-keys (sem "-l"):
// nomes de tecla (Up, PageUp, F12…), combos C-x / M-x / C-Left etc.
var tmuxKeyRe = regexp.MustCompile(`^(C-M-[A-Za-z0-9-]|M-C-[A-Za-z0-9-]|[CM]-[A-Za-z0-9-]{1,10}|[A-Za-z][A-Za-z0-9]{0,11})$`)

var (
	ensureToksOnce sync.Once
	ttydProxyOnce  sync.Once
	ttydProxyRef   *httputil.ReverseProxy
)

func ensureTerminalTokensTable() {
	ensureToksOnce.Do(func() {
		if res := sqliteExecParams(`CREATE TABLE IF NOT EXISTS terminal_tokens (
			token TEXT PRIMARY KEY,
			expires_at INTEGER NOT NULL
		)`); strings.HasPrefix(res, "Error") {
			log.Printf("[term-token] ERRO criando tabela terminal_tokens: %s", res)
		}
	})
}

func newEphemeralTokenValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// issueTerminalToken gera e persiste o token (GC dos expirados no mesmo fluxo).
func issueTerminalToken() (string, error) {
	ensureTerminalTokensTable()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	exp := time.Now().Add(terminalTokenTTL).Unix()
	if res := sqliteExecParams(`DELETE FROM terminal_tokens WHERE expires_at <= strftime('%s','now')`); strings.HasPrefix(res, "Error") {
		log.Printf("[term-token] aviso: GC de tokens expirados falhou: %s", res)
	}
	if res := sqliteExecParams(`INSERT INTO terminal_tokens (token, expires_at) VALUES (?, ?)`, tok, exp); strings.HasPrefix(res, "Error") {
		return "", fmt.Errorf("insert token: %s", res)
	}
	log.Printf("[term-token] token efêmero emitido (exp %d)", exp)
	return tok, nil
}

func validateTerminalToken(token string) bool {
	if token == "" {
		return false
	}
	ensureTerminalTokensTable()
	res := sqliteExecParams(`SELECT 1 FROM terminal_tokens WHERE token = ? AND expires_at > strftime('%s','now')`, token, time.Now().Unix())
	return res != "" && !strings.HasPrefix(res, "Error")
}

// handleTerminalToken — POST /terminal/token (owner-only):
// emite token efêmero e devolve a URL pública pronta para o iframe.
func handleTerminalToken(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireOwnerToken(w, r) {
		return
	}
	tok, err := issueTerminalToken()
	if err != nil {
		log.Printf("[term-token] erro gerando token: %v", err)
		http.Error(w, `{"status":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"terminal_url": terminalPublicURL + "/?token=" + tok,
		"expires_in":   int(terminalTokenTTL.Seconds()),
	})
}

// handleTerminalTokenValidate — GET /terminal/token/validate?token=... :
// checagem auxiliar (nginx auth_request / diagnósticos). 200 válido, 401 não.
func handleTerminalTokenValidate(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	w.Header().Set("Content-Type", "application/json")
	if validateTerminalToken(r.URL.Query().Get("token")) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]bool{"valid": true})
		return
	}
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]bool{"valid": false})
}

// getTTYDProxy cria o proxy reverso uma única vez (suporta upgrade WebSocket).
func getTTYDProxy() *httputil.ReverseProxy {
	ttydProxyOnce.Do(func() {
		target, _ := url.Parse(ttydUpstream)
		ttydProxyRef = &httputil.ReverseProxy{
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				// Preserva SUBPATHS (assets do ttyd: /style.css, /xterm.js…);
				// apenas a rota canônica local perde o prefixo.
				path := strings.TrimPrefix(pr.In.URL.Path, "/terminal/ttyd")
				if path == "" || path == "/terminal/ttyd" {
					path = "/"
				}
				pr.Out.URL.Path = path
				pr.Out.URL.RawPath = ""
				pr.Out.Host = target.Host
			},
			// BUG bold (23/08): injeta @font-face (JetBrains Mono 400+700) no
			// HTML do ttyd — a família HokMono é ativada via -t fontFamily no
			// systemd. Só HTML 200; WS e assets passam intocados.
			// FIX gzip (23/08): via Cloudflare a origem responde COM gzip —
			// descomprime antes de injetar e serve plano (sem Content-Encoding),
			// senão o strings.Replace falha silenciosamente nos bytes comprimidos.
			ModifyResponse: func(resp *http.Response) error {
				if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
					return nil
				}
				var body []byte
				var err error
				if strings.Contains(resp.Header.Get("Content-Encoding"), "gzip") {
					var gr *gzip.Reader
					if gr, err = gzip.NewReader(resp.Body); err != nil {
						return err
					}
					body, err = io.ReadAll(gr)
				} else {
					body, err = io.ReadAll(resp.Body)
				}
				if err != nil {
					return err
				}
				resp.Body.Close()
				// FIX scroll (24/08): a webfont carrega DEPOIS do xterm medir as
				// células → métricas obsoletas quebram a área de scroll do viewport.
				// fonts.ready → resize sintético → o cliente ttyd refaz o fit.
				// FIX mobile (24/08): meta viewport AUSENTE no ttyd → layout 980px
				// no celular → terminal 159x109 ilegível e página = buffer inteiro
				// (body overflow hidden → zero rolagem). Com a meta: layout 390,
				// terminal ~60x43 legível, scrollback no viewport do xterm.
				inject := `<meta name="viewport" content="width=device-width, initial-scale=1">` +
					`<style>@font-face{font-family:HokMono;src:url('` + fontBaseURL + `/terminal/fonts/JetBrainsMono-Regular.ttf') format('truetype');font-weight:400;}@font-face{font-family:HokMono;src:url('` + fontBaseURL + `/terminal/fonts/JetBrainsMono-Bold.ttf') format('truetype');font-weight:700;}</style><script>(function(){var f=document.fonts;if(!f)return;var t=function(){setTimeout(function(){window.dispatchEvent(new Event("resize"))},80)};f.addEventListener("loadingdone",t);f.ready&&f.ready.then(t)})()</script></head>`
				out := strings.Replace(string(body), "</head>", inject, 1)
				resp.Header.Del("Content-Encoding")
				resp.Header.Del("Content-Length")
				resp.Body = io.NopCloser(strings.NewReader(out))
				return nil
			},
		}
	})
	return ttydProxyRef
}

// handleTerminalTTYDProxy — qualquer método/path sob /terminal/ttyd:
// valida o token efêmero (query na entrada; cookie nas requisições seguintes)
// nas requisições PROTEGIDAS (raiz e upgrades WebSocket) e repassa ao ttyd.
// Assets estáticos do ttyd seguem sem token por natureza (não têm segredo).
func handleTerminalTTYDProxy(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	} else {
		http.SetCookie(w, &http.Cookie{
			Name:     termCookieName,
			Value:    token,
			Path:     "/",
			MaxAge:   int(terminalTokenTTL.Seconds()),
			Secure:   true,
			SameSite: http.SameSiteNoneMode, // iframe cross-origin precisa
		})
	}

	isWS := strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
	// Entrada = carga inicial do iframe (raiz via túnel ou rota canônica local).
	entry := r.URL.Path == "/" || r.URL.Path == "" || r.URL.Path == "/index.html" ||
		r.URL.Path == "/terminal/ttyd"
	if token == "" && !hasTokenCookie(r) {
		// Nem query nem cookie: só pode ser asset anônimo; bloqueia entrada/WS.
		if entry || isWS {
			unauthorized(w)
			return
		}
	} else if !validateTerminalToken(token) {
		// Token presente porém inválido/expirado: bloqueia tudo que importa.
		if entry || isWS || true {
			unauthorized(w)
			return
		}
	}
	getTTYDProxy().ServeHTTP(w, r)
}

// handleTerminalTTYDKey — POST /terminal/ttyd/key (protegido pelo token
// efêmero; o dono já o recebeu em /terminal/token):
//
//	body {"key":"Up"}            → tmux send-keys -t hok-ttyd Up
//	body {"text":"|"}            → tmux send-keys -t hok-ttyd -l "|"
//	body {"key":"C-c"}           → Ctrl+C; combinações: "C-Left", "C-Space"…
//
// Injeção via tmux permite teclas especiais sem tocar no cliente do ttyd,
// e dá persistência de sessão de graça (tmux sobrevive a reloads).
func handleTerminalTTYDKey(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"unauthorized"}`))
		return
	}
	var req struct {
		Key     string `json:"key"`
		Text    string `json:"text"`
		Session string `json:"session"` // TESTE C: sessão da aba ativa
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := resolveTmuxSession(req.Session)
	if target == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_session"}`))
		return
	}
	switch {
	case req.Key != "" && req.Text == "":
		if len(req.Key) > 24 || !tmuxKeyRe.MatchString(req.Key) {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":"bad_key"}`))
			return
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", target, req.Key).CombinedOutput(); err != nil {
			log.Printf("[term-key] send-keys %s@%s: %v (%s)", req.Key, target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case req.Text != "" && req.Key == "":
		if len(req.Text) > 64 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-l", req.Text).CombinedOutput(); err != nil {
			log.Printf("[term-key] send-keys -l@%s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func hasTokenCookie(r *http.Request) bool {
	ck, err := r.Cookie(termCookieName)
	return err == nil && ck.Value != ""
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"status":"unauthorized","message":"token ausente ou expirado — solicite POST /terminal/token"}`))
}

// clearOscEchoIfShell — BUG 1 (23/08): o OSC injetado via send-keys -l é
// ecoado pelo shell (modo canônico) como lixo visível na linha de input.
// Em TUI (raw mode) não há eco. Envia C-u APENAS quando o painel está num
// shell — gate consciente (mesmo princípio do tryTerminalExec).
func clearOscEchoIfShell(target string) {
	out, err := exec.Command("tmux", "display", "-p", "-t", target,
		"#{pane_current_command}").CombinedOutput()
	if err != nil {
		return
	}
	switch strings.TrimSpace(string(out)) {
	case "bash", "sh", "zsh", "ash", "dash", "ksh":
		if out2, err2 := exec.Command("tmux", "send-keys", "-t", target, "C-u").CombinedOutput(); err2 != nil {
			log.Printf("[term-theme] C-u limpa-eco %s: %v (%s)", target, err2, out2)
		}
	}
}

// handleTerminalTTYDTheme — POST /terminal/ttyd/theme (token efêmero):
// aplica paleta de cores À SESSÃO ttyd viva via sequências OSC 10/11/4
// (redefinição de paleta em tempo real — suportada pelo xterm do ttyd).
// Body: {"background":"#rrggbb","foreground":"#rrggbb","cursor":"#rrggbb",
//
//	"ansi":["#…",×16]}
func handleTerminalTTYDTheme(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	var req struct {
		Osc        string   `json:"osc"`
		Background string   `json:"background"`
		Foreground string   `json:"foreground"`
		Cursor     string   `json:"cursor"`
		Ansi       []string `json:"ansi"`
		Session    string   `json:"session"` // TESTE C: sessão da aba ativa
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := resolveTmuxSession(req.Session)
	if target == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_session"}`))
		return
	}
	if req.Osc != "" && len(req.Osc) < 8192 {
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-l", req.Osc).CombinedOutput(); err != nil {
			log.Printf("[term-theme] send-keys osc@%s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		clearOscEchoIfShell(target)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	}
	hexToOsc := func(h string) string {
		h = strings.TrimPrefix(strings.ToLower(h), "#")
		if len(h) != 6 {
			return ""
		}
		return "rgb:" + h[0:2] + "/" + h[2:4] + "/" + h[4:6]
	}
	var seq strings.Builder
	if c := hexToOsc(req.Background); c != "" {
		seq.WriteString("\x1b]11;" + c + "\x07")
	}
	if c := hexToOsc(req.Foreground); c != "" {
		seq.WriteString("\x1b]10;" + c + "\x07")
	}
	if c := hexToOsc(req.Cursor); c != "" {
		seq.WriteString("\x1b]12;" + c + "\x07")
	}
	for i, col := range req.Ansi {
		if c := hexToOsc(col); c != "" && i < 16 {
			seq.WriteString(fmt.Sprintf("\x1b]4;%d;%s\x07", i, c))
		}
	}
	if seq.Len() == 0 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if out, err := exec.Command("tmux", "send-keys", "-t", target, "-l", seq.String()).CombinedOutput(); err != nil {
		log.Printf("[term-theme] send-keys@%s: %v (%s)", target, err, out)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	clearOscEchoIfShell(target)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleTerminalTTYDClose — POST /terminal/ttyd/close (token efêmero):
// TESTE C — fecha a sessão tmux da aba (botão "x"): body {"session":"hok-terminal-1"}.
// Whitelist estrita (resolveTmuxSession) impede fechar sessões alheias.
func handleTerminalTTYDClose(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	var req struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := resolveTmuxSession(req.Session)
	if target == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_session"}`))
		return
	}
	out, err := exec.Command("tmux", "kill-session", "-t", target).CombinedOutput()
	if err != nil {
		// Sessão inexistente: idempotente para o client (já "fechada").
		log.Printf("[term-close] kill-session %s: %v (%s)", target, err, out)
	}
	// FIX 27/08: limpar o registro de sessão ativa quando a sessão fechada
	// é a registrada — o registro deve refletir "terminal aberto agora".
	if active := loadTerminalActive(); active != "" && active == target {
		clearTerminalActive()
		log.Printf("[term-active] registro limpo (close %s)", target)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleTerminalTTYDetach — POST /terminal/ttyd/detach (token efêmero):
// FIX 27/08 — "sair sem encerrar" (botão ⤴ do frontend): limpa o registro
// de sessão ativa SEM matar a sessão tmux (o processo continua no servidor).
func handleTerminalTTYDetach(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	var req struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := resolveTmuxSession(req.Session)
	if target == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_session"}`))
		return
	}
	if active := loadTerminalActive(); active != "" && active == target {
		clearTerminalActive()
		log.Printf("[term-active] registro limpo (detach %s)", target)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// handleTerminalStatus — GET /terminal/status?session=<nome> (token efêmero):
// PENDÊNCIA 3 (28/08) — poll leve do estado da sessão pelo frontend.
// Checa se a sessão tmux existe E o pane ainda responde (display-message).
// Quando "down", o frontend dispara startRecovery (remontagem do iframe) sem
// depender de visibilitychange — cobre pane/sessão morta com o app em
// foreground. Leve: duas chamadas tmux rápidas, sem estado.
func handleTerminalStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		jsonError(w, "metodo nao suportado", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	session := resolveTmuxSession(r.URL.Query().Get("session"))
	if session == "" {
		jsonError(w, "session obrigatorio", http.StatusBadRequest)
		return
	}
	up := false
	if err := exec.Command("tmux", "has-session", "-t", session).Run(); err == nil {
		if out, err := exec.Command("tmux", "display-message", "-t", session, "-p", "#{pane_pid}").CombinedOutput(); err == nil && strings.TrimSpace(string(out)) != "" {
			up = true
		}
	}
	respondJSON(w, map[string]interface{}{
		"session":    session,
		"status":     map[bool]string{true: "up", false: "down"}[up],
		"pane_alive": up,
	})
}

// handleTerminalTTYDScroll — POST /terminal/ttyd/scroll (token efêmero):
// TESTE D — barra de rolagem do scrollback dirigida por tmux copy-mode.
//
//	body {"session":"hok-terminal-1","action":"info"}              → {"history":N,"height":P,"pos":-1}
//	body {"session":"...","action":"enter"}                        → tmux copy-mode
//	body {"session":"...","action":"up"|"down","amount":5}         → scroll-up/-down N
//	body {"session":"...","action":"goto","amount":120}            → goto-line N
//	body {"session":"...","action":"top"}                          → copy-mode + goto-line 0
//	body {"session":"...","action":"bottom"|"exit"}                → cancel (volta ao vivo)
//
// pos = -1 fora de copy-mode. Em apps TUI (alternate screen) history=0 →
// o frontend oculta a barra (quem rola lá é o próprio app).
// DOC 01/09 (scroll "travado" em TUI): em alternate screen (ex: opencode,
// claude fullscreen) o tmux reporta history_size=0 por design — NÃO há
// scrollback no tmux para copy-mode navegar; o scroll é interno do app TUI.
// Isso é comportamento esperado, não bug do frontend. O botão "Histórico"
// (log rotativo via capture-pane) é o workaround oficial. (Opcional futura:
// `tmux set -g mouse on` para a roda física chegar ao app no desktop.)
func handleTerminalTTYDScroll(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	var req struct {
		Session string `json:"session"`
		Action  string `json:"action"`
		Amount  int    `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := resolveTmuxSession(req.Session)
	if target == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_session"}`))
		return
	}
	switch req.Action {
	case "info":
		// ATENÇÃO: fora de copy-mode #{scroll_position} expande para VAZIO
		// (não "0") — o campo some da saída e deslocaria um parse posicional.
		// Por isso fg vai POR ÚLTIMO e o parse é: numéricos iniciais = [hist,
		// height, (pos), alt, width, mouse_any]; qualquer resto não numérico = fg.
		out, err := exec.Command("tmux", "display", "-p", "-t", target,
			"#{history_size} #{pane_height} #{scroll_position} #{alternate_on} #{pane_width} #{mouse_any_flag} #{pane_current_command}").CombinedOutput()
		if err != nil {
			log.Printf("[term-scroll] info %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f := strings.Fields(string(out))
		nums := make([]int, 0, 5)
		fgEnd := len(f)
		for i, tok := range f {
			if v, err := strconv.Atoi(tok); err == nil {
				nums = append(nums, v)
			} else {
				fgEnd = i
				break
			}
		}
		if len(nums) < 2 {
			log.Printf("[term-scroll] info %s: saída inesperada: %q", target, string(out))
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		hist, height := nums[0], nums[1]
		pos := -1
		altIdx, wIdx, mouseIdx := 2, 3, 4
		if len(nums) >= 6 { // pos presente (dentro de copy-mode)
			pos = nums[2]
			altIdx, wIdx, mouseIdx = 3, 4, 5
		}
		alt := altIdx < len(nums) && nums[altIdx] == 1
		width := 0
		if wIdx < len(nums) {
			width = nums[wIdx]
		}
		mouseAny := mouseIdx < len(nums) && nums[mouseIdx] == 1
		fg := ""
		if fgEnd < len(f) {
			fg = f[fgEnd]
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"history": hist, "height": height, "pos": pos,
			"fg": fg, "alt": alt, "width": width, "mouse_any": mouseAny,
		})
		return
	case "enter":
		if out, err := exec.Command("tmux", "copy-mode", "-t", target).CombinedOutput(); err != nil {
			log.Printf("[term-scroll] enter %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case "exit", "bottom":
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", "cancel").CombinedOutput(); err != nil {
			log.Printf("[term-scroll] cancel %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case "top":
		if out, err := exec.Command("tmux", "copy-mode", "-t", target).CombinedOutput(); err != nil {
			log.Printf("[term-scroll] top enter %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", "goto-line", "0").CombinedOutput(); err != nil {
			log.Printf("[term-scroll] top goto %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case "up", "down":
		if req.Amount < 1 || req.Amount > 100 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":"bad_amount"}`))
			return
		}
		cmd := "scroll-up"
		if req.Action == "down" {
			cmd = "scroll-down"
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", cmd,
			fmt.Sprintf("%d", req.Amount)).CombinedOutput(); err != nil {
			log.Printf("[term-scroll] %s %d %s: %v (%s)", cmd, req.Amount, target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case "wheel":
		// FIX SCROLL TUI (26/08): repassa roda do mouse SGR direto ao app no
		// painel — o MESMO caminho que a roda física usa no desktop (tmux mouse
		// on → forward ao app). Sem copy-mode: nada congela, nada duplica, o
		// teclado nunca fica preso. Amount >0 = wheel-up (histórico mais antigo,
		// conteúdo desce), <0 = wheel-down. Press+release por passo.
		if req.Amount == 0 || req.Amount > 20 || req.Amount < -20 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":"bad_amount"}`))
			return
		}
		var pw, ph int
		if out, err := exec.Command("tmux", "display", "-p", "-t", target,
			"#{pane_width} #{pane_height}").CombinedOutput(); err == nil {
			fmt.Sscanf(string(out), "%d %d", &pw, &ph)
		}
		if pw < 1 {
			pw = 80
		}
		if ph < 1 {
			ph = 24
		}
		cx, cy := pw/2, ph/2
		if cx < 1 {
			cx = 1
		}
		if cy < 1 {
			cy = 1
		}
		btn := "64" // wheel-up
		if req.Amount < 0 {
			btn = "65" // wheel-down
		}
		n := req.Amount
		if n < 0 {
			n = -n
		}
		seq := strings.Repeat(fmt.Sprintf("\x1b[<%s;%d;%dM\x1b[<%s;%d;%dm", btn, cx, cy, btn, cx, cy), n)
		if out, err := exec.Command("tmux", "send-keys", "-l", "-t", target, seq).CombinedOutput(); err != nil {
			log.Printf("[term-scroll] wheel %d %s: %v (%s)", req.Amount, target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	case "goto":
		if req.Amount < 0 || req.Amount > 1_000_000 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":"bad_amount"}`))
			return
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", "goto-line",
			fmt.Sprintf("%d", req.Amount)).CombinedOutput(); err != nil {
			log.Printf("[term-scroll] goto %d %s: %v (%s)", req.Amount, target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_action"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// ── PONTE CHAT→TTYD (23/08): registro da sessão ttyd VISÍVEL ativa ──────
// O frontend (que sabe a aba ativa) registra a sessão; a cascata do chat
// (tryTerminalExec) injeta nela via key injection + captura por marker.
var (
	activeTTYDMu      sync.Mutex
	activeTTYDSession string // espelho em memória (rápido); fonte de verdade = SQLite
	activeTTYDOnce    sync.Once
)

// FIX persistência (24/08): o registro em memória morria no restart do
// hokma e o /terminal do chat voltava a "Nenhuma sessão ativa". Fonte de
// verdade agora é o SQLite (sobrevive a restarts).
// FIX 27/08: coluna updated_at + TTL — o registro deve refletir "terminal
// ABERTO agora", não "usado alguma vez" (ver ADENDO_BUG_20260827).
const terminalActiveTTL = 7 * 60 // segundos; o app renova o token a cada ~4-5 min

func ensureTerminalActiveTable() {
	activeTTYDOnce.Do(func() {
		if res := sqliteExecParams(`CREATE TABLE IF NOT EXISTS terminal_active (
			id INTEGER PRIMARY KEY CHECK(id=1),
			session TEXT NOT NULL,
			updated_at INTEGER NOT NULL DEFAULT (unixepoch())
		)`); strings.HasPrefix(res, "Error") {
			log.Printf("[term-active] ERRO criando tabela terminal_active: %s", res)
		}
		// migração defensiva: tabela antiga (produção) sem updated_at
		if res := sqliteExecParams(`ALTER TABLE terminal_active ADD COLUMN updated_at INTEGER NOT NULL DEFAULT (unixepoch())`); strings.HasPrefix(res, "Error") && !strings.Contains(res, "duplicate") {
			log.Printf("[term-active] ALTER updated_at: %s", res)
		}
	})
}

func loadTerminalActive() string {
	ensureTerminalActiveTable()
	var session string
	var updated int64
	if err := db.QueryRow(`SELECT session, updated_at FROM terminal_active WHERE id=1`).Scan(&session, &updated); err != nil {
		return ""
	}
	// TTL defensivo: registro antigo = aba fechada/app morto sem notificar.
	// Com o terminal aberto o app re-registra a cada renovação de token
	// (~4-5 min), então 7 min nunca expira indevidamente.
	if time.Now().Unix()-updated > terminalActiveTTL {
		return ""
	}
	return strings.TrimSpace(session)
}

func saveTerminalActive(session string) {
	ensureTerminalActiveTable()
	if res := sqliteExecParams(`INSERT INTO terminal_active (id, session, updated_at) VALUES (1, ?, unixepoch())
		ON CONFLICT(id) DO UPDATE SET session=excluded.session, updated_at=unixepoch()`, session); strings.HasPrefix(res, "Error") {
		log.Printf("[term-active] ERRO salvando sessão ativa: %s", res)
	}
}

// clearTerminalActive remove o registro de sessão ativa (aba fechada ou
// detach) — o registro deve refletir "terminal aberto agora".
func clearTerminalActive() {
	ensureTerminalActiveTable()
	activeTTYDMu.Lock()
	activeTTYDSession = ""
	activeTTYDMu.Unlock()
	if res := sqliteExecParams(`DELETE FROM terminal_active WHERE id=1`); strings.HasPrefix(res, "Error") {
		log.Printf("[term-active] ERRO limpando sessão ativa: %s", res)
	}
}

func handleTerminalTTYDActive(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	var req struct {
		Session string `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := ""
	if req.Session != "" {
		target = resolveTmuxSession(req.Session)
		if target == "" {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":"bad_session"}`))
			return
		}
	}
	activeTTYDMu.Lock()
	activeTTYDSession = target
	activeTTYDMu.Unlock()
	saveTerminalActive(target)
	log.Printf("[term-active] sessão ttyd ativa registrada: %q", target)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// registeredActiveTTYD devolve a sessão ttyd ativa (registrada e viva no
// tmux). Cadeia de fallback: (1) sessão registrada no SQLite; (2) se existir
// EXATAMENTE UMA sessão ttyd viva, usa-a (restart não apaga a escolha);
// (3) "" → cascata cai no caminho legado.
func registeredActiveTTYD() string {
	ensureTerminalActiveTable()
	activeTTYDMu.Lock()
	s := activeTTYDSession
	activeTTYDMu.Unlock()
	if s == "" {
		s = loadTerminalActive()
		if s != "" {
			activeTTYDSession = s
		}
	}
	if s != "" {
		if err := exec.Command("tmux", "has-session", "-t", s).Run(); err == nil {
			return s
		}
		// registrada mas morta: esquece (não apaga o registro — o frontend
		// pode recriar a sessão com o mesmo nome)
		return ""
	}
	// fallback: exatamente UMA sessão ttyd viva → é ela
	out, err := exec.Command("tmux", "ls").CombinedOutput()
	if err != nil {
		return ""
	}
	var found []string
	for _, ln := range strings.Split(string(out), "\n") {
		if i := strings.Index(ln, ":"); i > 0 {
			name := ln[:i]
			if name == "hok-ttyd" || strings.HasPrefix(name, "hok-terminal-") {
				found = append(found, name)
			}
		}
	}
	if len(found) == 1 {
		return found[0]
	}
	return ""
}

// ttydBridgeExec — PONTE: injeta o comando na sessão ttyd VISÍVEL (send-keys,
// mesmo transporte da barra de teclas), aguarda o marker via capture-pane e
// devolve o output limpo (reaproveita cleanCapturedOutput). Recusa com motivo
// quando o painel está num TUI (gate consciente).
func ttydBridgeExec(target string, cmd string) (output string, refused string, timedOut bool) {
	if out, err := exec.Command("tmux", "display", "-p", "-t", target,
		"#{pane_current_command}").CombinedOutput(); err == nil {
		fg := strings.TrimSpace(string(out))
		switch fg {
		case "bash", "sh", "zsh", "ash", "dash", "ksh":
			// shell pronto ✓
		default:
			return "", "o painel visível está executando " + fg, false
		}
	}
	marker := fmt.Sprintf("___H%d___", time.Now().UnixNano()%1_000_000) // curto: não quebra linha em painéis estreitos
	log.Printf("[AUDIT] ttyd_bridge user=chat session=%s cmd=%q ts=%s", target, cmd, time.Now().Format(time.RFC3339))
	if out, err := exec.Command("tmux", "send-keys", "-t", target, "-l",
		cmd+"\n"+"echo "+marker+"\n").CombinedOutput(); err != nil {
		log.Printf("[ttyd-bridge] send-keys %s: %v (%s)", target, err, out)
		return "", "falha ao injetar na sessão visível", false
	}
	deadline := time.Now().Add(terminalExecTimeout)
	var lastCapture string
	for time.Now().Before(deadline) {
		time.Sleep(400 * time.Millisecond)
		out, err := exec.Command("tmux", "capture-pane", "-p", "-S", "-200", "-t", target).CombinedOutput()
		if err != nil {
			continue
		}
		lastCapture = string(out)
		for _, ln := range strings.Split(lastCapture, "\n") {
			if strings.TrimSpace(ln) == marker {
				return strings.TrimRight(cleanCapturedOutput(lastCapture, cmd, marker), "\n "), "", false
			}
		}
	}
	clean := strings.TrimRight(cleanCapturedOutput(lastCapture, cmd, marker), "\n ")
	if clean == "" {
		clean = "(sem output ainda)"
	}
	return clean + "\n\n_[comando ainda em execução, output parcial]_", "", true
}

// handleTerminalTTYDExec — POST /terminal/ttyd/exec (token efêmero):
// ponte chat→ttyd. body {"session":"hok-ttyd","cmd":"echo x"}.
func handleTerminalTTYDExec(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	var req struct {
		Session string `json:"session"`
		Cmd     string `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := resolveTmuxSession(req.Session)
	if target == "" || req.Cmd == "" || len(req.Cmd) > 500 || strings.ContainsAny(req.Cmd, "\n") {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_request"}`))
		return
	}
	output, refused, timedOut := ttydBridgeExec(target, req.Cmd)
	if refused != "" {
		w.WriteHeader(http.StatusConflict)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "refused", "reason": refused})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"output": output, "timedOut": timedOut})
}

// handleTerminalTTYDSelection — POST /terminal/ttyd/selection (token efêmero):
// TESTE E — seleção de texto dirigida por tmux copy-mode (a única via real:
// o conteúdo vive no iframe cross-origin). A seleção fica VISÍVEL no terminal
// (highlight do tmux) e o usuário extende com as setas da barra de teclas.
//
//	body {"session":"...","action":"start"}                      → copy-mode + begin-selection
//	body {"session":"...","action":"copy"}                      → copy-selection-and-cancel + texto do buffer
//	body {"session":"...","action":"cancel"}                    → cancel (sai sem copiar)
//	body {"session":"...","action":"screen"}                    → capture-pane visível (sem copy-mode)
//	body {"session":"...","action":"all"}                       → capture-pane -S - (histórico inteiro)
//
// FIX 24/08 (usuário: "copiar tudo não funciona"): as ações de captura NÃO
// usam mais a dança copy-mode/top-line/begin-selection/bottom-line — que
// dependia do estado do copy-mode e falhava silenciosamente. capture-pane
// lê o buffer DIRETO da sessão, sem tocar na tela e sem depender de modo.
func handleTerminalTTYDSelection(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	var req struct {
		Session string `json:"session"`
		Action  string `json:"action"`
		Text    string `json:"text,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	target := resolveTmuxSession(req.Session)
	if target == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_session"}`))
		return
	}
	switch req.Action {
	case "start":
		if out, err := exec.Command("tmux", "copy-mode", "-t", target).CombinedOutput(); err != nil {
			log.Printf("[term-select] copy-mode %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", "begin-selection").CombinedOutput(); err != nil {
			log.Printf("[term-select] begin-selection %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case "all":
		// FIX 24/08: histórico inteiro via capture-pane -S - (do início do
		// scrollback até a linha visível). Um só comando, sem copy-mode.
		out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p", "-S", "-").CombinedOutput()
		if err != nil {
			log.Printf("[term-select] all capture %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": string(out)})
		return
	case "screen":
		// Copiar SÓ a tela visível (capture-pane sem -S/-E = área visível).
		out, err := exec.Command("tmux", "capture-pane", "-t", target, "-p").CombinedOutput()
		if err != nil {
			log.Printf("[term-select] screen capture %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": string(out)})
		return
	case "paste":
		// Cola no painel o texto recebido (o frontend lê o clipboard e envia).
		if req.Text == "" || len(req.Text) > 100_000 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"status":"bad_text"}`))
			return
		}
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-l", req.Text).CombinedOutput(); err != nil {
			log.Printf("[term-select] paste %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	case "copy":
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", "copy-selection-and-cancel").CombinedOutput(); err != nil {
			log.Printf("[term-select] copy %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		out, err := exec.Command("tmux", "show-buffer").CombinedOutput()
		if err != nil {
			log.Printf("[term-select] show-buffer: %v (%s)", err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": string(out)})
		return
	case "cancel":
		if out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", "cancel").CombinedOutput(); err != nil {
			log.Printf("[term-select] cancel %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"bad_action"}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// serveTerminalFonts — GET /terminal/fonts/<arquivo> (assets estáticos,
// sem token — mesma natureza dos assets do ttyd): JetBrains Mono 400/700
// para o @font-face injetado no cliente xterm (bold real no Android).
func serveTerminalFonts(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	name := strings.TrimPrefix(r.URL.Path, "/terminal/fonts/")
	switch name {
	case "JetBrainsMono-Regular.ttf", "JetBrainsMono-Bold.ttf":
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "font/ttf")
	w.Header().Set("Cache-Control", "public, max-age=604800")
	http.ServeFile(w, r, "/root/hokma/fonts/"+name)
}

// FIX log-rotativo-tui (30/08): endpoints de histórico rotativo por sessão
// tmux. Necessário porque opencode/claude em alternate screen (TUI
// fullscreen) têm history_size=0 no tmux — a scrollbar do scrollback não
// funciona e o capture-pane -S - só vê a tela atual. O helper
// /root/hokma/tmux-capture.sh roda em background e grava snapshots em
// /var/log/hok-term/<sess>.log a cada 2s.
//
// Estes endpoints NÃO exigem X-Hok-Token (só token efêmero de sessão):
//
//	GET  /terminal/ttyd/log?session=X&token=Y[&max=N]&since=ISO
//	     → retorna texto do log (cap em max linhas, default 2000)
//	POST /terminal/ttyd/log/start?session=X&token=Y
//	     → inicia o helper para a sessão (idempotente, kill+respawn)
const termLogBaseDir = "/var/log/hok-term"
const termLogMaxDefault = 2000
const termLogMaxHard = 20000

func termLogSessionPath(sess string) string {
	// Validação rígida do nome (whitelist: alfanum + - + .)
	for _, c := range sess {
		if !(c >= 'a' && c <= 'z') && !(c >= 'A' && c <= 'Z') &&
			!(c >= '0' && c <= '9') && c != '-' && c != '.' {
			return ""
		}
	}
	return termLogBaseDir + "/" + sess + ".log"
}

func handleTerminalTTYDLog(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	sess := r.URL.Query().Get("session")
	if sess == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}
	logPath := termLogSessionPath(sess)
	if logPath == "" {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}

	// max=N (default 2000, hard 20000) — limita linhas retornadas
	max := termLogMaxDefault
	if v := r.URL.Query().Get("max"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= termLogMaxHard {
			max = n
		}
	}

	// since=ISO8601 — filtra só timestamps >= since (se informado)
	since := r.URL.Query().Get("since")

	// Lê arquivo + corta em linhas
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 200 com texto vazio (sem log ainda) — frontend decide
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"status":  "ok",
				"session": sess,
				"text":    "",
				"lines":   0,
				"path":    logPath,
				"exists":  false,
			})
			return
		}
		http.Error(w, "read log: "+err.Error(), http.StatusInternalServerError)
		return
	}

	text := string(data)
	var lines []string
	if since != "" {
		// Cada snapshot começa com "--- <iso> ---"; filtra por header
		lines = filterLogSince(text, since)
	} else {
		// FIX 01/09 (histórico duplicado): o helper grava a TELA INTEIRA a
		// cada mudança — concatenar todos os blocos faz cada mensagem da
		// conversa aparecer em TODOS os snapshots seguintes (duplicado).
		// Mantemos apenas o ÚLTIMO snapshot (estado mais recente da tela,
		// que já contém a conversa acumulada visível) — sem repetição.
		lines = dedupLogLines(strings.Split(text, "\n"))
	}

	// Pega as últimas `max` linhas
	if len(lines) > max {
		lines = lines[len(lines)-max:]
	}
	out := strings.Join(lines, "\n")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"session": sess,
		"text":    out,
		"lines":   len(lines),
		"path":    logPath,
		"exists":  true,
	})
}

// filterLogSince — mantém só os blocos cujo header "--- ISO ---" >= since.
func filterLogSince(text, since string) []string {
	allLines := strings.Split(text, "\n")
	var out []string
	keep := false
	sinceNorm := since
	for i, ln := range allLines {
		if strings.HasPrefix(ln, "--- ") && strings.HasSuffix(ln, " ---") {
			ts := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ln, "--- "), " ---"))
			keep = ts >= sinceNorm
		}
		if keep {
			out = append(out, ln)
		}
		_ = i
	}
	if len(out) == 0 {
		return allLines // fallback se não achou nenhum header
	}
	return out
}

// dedupLogLines — reconstrói o log sem a duplicação de snapshots.
// O helper tmux-capture.sh grava um SNAPSHOT COMPLETO da tela a cada
// mudança (separado por headers "--- ISO ---"). Numa conversa, cada snapshot
// novo contém a tela inteira acumulada → concatenar todos os blocos faz cada
// mensagem aparecer em vários snapshots seguidos (histórico "duplicado").
// Estratégia: manter apenas o ÚLTIMO snapshot (o estado mais recente da tela),
// que já embute toda a conversa visível no momento — zero repetição. O header
// inicial "=== hok-term log ===" é preservado.
func dedupLogLines(allLines []string) []string {
	var header []string
	var cur []string
	inSnap := false
	var lastSnap []string
	for _, ln := range allLines {
		if strings.HasPrefix(ln, "--- ") && strings.HasSuffix(ln, " ---") {
			// fim do snapshot anterior (se houver) — guarda como candidato
			if inSnap {
				lastSnap = append([]string(nil), cur...)
			}
			cur = []string{ln}
			inSnap = true
			continue
		}
		if !inSnap {
			header = append(header, ln)
			continue
		}
		cur = append(cur, ln)
	}
	if inSnap && len(cur) > 0 {
		lastSnap = append([]string(nil), cur...)
	}
	if len(lastSnap) == 0 {
		return allLines // sem headers (log simples) → devolve como está
	}
	out := append(header, lastSnap...)
	return out
}

// FIX bug-limpar-historico (30/08): DELETE /terminal/ttyd/log?session=X&token=Y
// limpa o arquivo de log da sessão (e mata o helper). O helper é reiniciado
// automaticamente no próximo touchstart do frontend (que chama /log/start).
func handleTerminalTTYDLogDelete(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	sess := r.URL.Query().Get("session")
	if sess == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}
	logPath := termLogSessionPath(sess)
	if logPath == "" {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}

	// Mata o helper (ele tem PID file separado) para parar de escrever no
	// arquivo enquanto a remoção acontece — evita race condition.
	pidFile := termLogBaseDir + "/" + sess + ".pid"
	pidKilled := false
	if data, err := os.ReadFile(pidFile); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if err := syscall.Kill(pid, syscall.SIGTERM); err == nil {
				pidKilled = true
				time.Sleep(200 * time.Millisecond) // dá tempo de sair
			}
		}
	}

	// Remove o arquivo de log (e o .log.1 se existir)
	removedBytes := int64(0)
	for _, p := range []string{logPath, logPath + ".1"} {
		if info, err := os.Stat(p); err == nil {
			removedBytes += info.Size()
			if err := os.Remove(p); err != nil {
				log.Printf("[term-log-delete] erro removendo %s: %v", p, err)
			}
		}
	}
	rmPid := os.Remove(pidFile) // best-effort

	log.Printf("[term-log-delete] sessão %s: %d bytes removidos, helper killed=%v, pidRm=%v",
		sess, removedBytes, pidKilled, rmPid == nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":        "ok",
		"session":       sess,
		"deleted_bytes": removedBytes,
		"helper_killed": pidKilled,
	})
}

func handleTerminalTTYDLogStart(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		if ck, err := r.Cookie(termCookieName); err == nil {
			token = ck.Value
		}
	}
	if !validateTerminalToken(token) {
		unauthorized(w)
		return
	}
	sess := r.URL.Query().Get("session")
	if sess == "" {
		http.Error(w, "session required", http.StatusBadRequest)
		return
	}
	if termLogSessionPath(sess) == "" {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}

	// FIX 01/09 (histórico duplicado): mata TODOS os helpers da sessão, não
	// só o do PID file. Helpers órfãos coexistiam (race no start) e gravavam
	// snapshots duplicados no MESMO arquivo de log — histórico aparecia
	// repetido. pgrep -f casa "tmux-capture.sh <sess>" em cada processo sh.
	killHelperOrphans(sess)

	// Inicia helper em background
	helper := "/root/hokma/tmux-capture.sh"
	if _, err := os.Stat(helper); err != nil {
		http.Error(w, "helper not found: "+helper, http.StatusInternalServerError)
		return
	}
	cmd := exec.Command(helper, sess)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		http.Error(w, "start helper: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// Não espera — helper é daemon-like (Process.Release faz detach)
	go func() { _ = cmd.Wait() }()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"started": true,
		"pid":     cmd.Process.Pid,
	})
}

// killHelperOrphans encerra todos os processos tmux-capture.sh da sessão.
// Usa pgrep -f (casa o caminho do script + o nome da sessão) e envia
// SIGTERM; ignora o próprio processo (o backend nunca roda com esse padrão).
func killHelperOrphans(sess string) {
	if sess == "" {
		return
	}
	out, err := exec.Command("pgrep", "-f", "tmux-capture.sh "+sess).Output()
	if err != nil {
		// pgrep sai 1 quando não encontra — não é erro.
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil || pid <= 0 || pid == os.Getpid() {
			continue
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			log.Printf("[term-log] kill orphan helper %d: %v", pid, err)
		} else {
			log.Printf("[term-log] helper órfão encerrado: pid=%d sessão=%s", pid, sess)
		}
	}
	time.Sleep(200 * time.Millisecond) // dá tempo de sair antes do novo spawn
}

// Registro segue o precedente do projeto (init() em debug_n8n.go /
// hermes_route.go): módulo autocontido, sem tocar no main.go.
func init() {
	http.HandleFunc("/terminal/token", handleTerminalToken)
	http.HandleFunc("/terminal/token/validate", handleTerminalTokenValidate)
	http.HandleFunc("/terminal/ttyd", handleTerminalTTYDProxy)
	http.HandleFunc("/terminal/ttyd/key", handleTerminalTTYDKey)
	http.HandleFunc("/terminal/ttyd/theme", handleTerminalTTYDTheme)
	http.HandleFunc("/terminal/ttyd/close", handleTerminalTTYDClose)
	http.HandleFunc("/terminal/ttyd/detach", handleTerminalTTYDetach)
	http.HandleFunc("/terminal/ttyd/scroll", handleTerminalTTYDScroll)
	http.HandleFunc("/terminal/status", handleTerminalStatus)
	http.HandleFunc("/terminal/fonts/", serveTerminalFonts)
	http.HandleFunc("/terminal/ttyd/active", handleTerminalTTYDActive)
	http.HandleFunc("/terminal/ttyd/exec", handleTerminalTTYDExec)
	http.HandleFunc("/terminal/ttyd/selection", handleTerminalTTYDSelection)
	// FIX log-rotativo-tui (30/08): histórico de TUI via helper externo.
	// /terminal/ttyd/log suporta GET (ler) e DELETE (limpar + matar helper).
	http.HandleFunc("/terminal/ttyd/log", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleTerminalTTYDLog(w, r)
		case http.MethodDelete:
			handleTerminalTTYDLogDelete(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	http.HandleFunc("/terminal/ttyd/log/start", handleTerminalTTYDLogStart)
	log.Println("✅ rotas ttyd registradas via init(): /terminal/token (POST), /terminal/token/validate (GET), /terminal/ttyd (proxy), /terminal/ttyd/close, /terminal/ttyd/scroll (TESTE D), /terminal/ttyd/log, /terminal/ttyd/log/start (FIX log-rotativo-tui)")
}
