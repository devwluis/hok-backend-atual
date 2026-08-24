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
	"os/exec"
	"regexp"
	"strings"
	"sync"
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
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
		out, err := exec.Command("tmux", "display", "-p", "-t", target,
			"#{history_size} #{pane_height} #{scroll_position}").CombinedOutput()
		if err != nil {
			log.Printf("[term-scroll] info %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		var hist, height, pos int
		pos = -1
		fmt.Sscanf(string(out), "%d %d %d", &hist, &height, &pos)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]int{"history": hist, "height": height, "pos": pos})
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
func ensureTerminalActiveTable() {
	activeTTYDOnce.Do(func() {
		if res := sqliteExecParams(`CREATE TABLE IF NOT EXISTS terminal_active (
			id INTEGER PRIMARY KEY CHECK(id=1),
			session TEXT NOT NULL
		)`); strings.HasPrefix(res, "Error") {
			log.Printf("[term-active] ERRO criando tabela terminal_active: %s", res)
		}
	})
}

func loadTerminalActive() string {
	ensureTerminalActiveTable()
	res := sqliteExecParams(`SELECT session FROM terminal_active WHERE id=1`)
	if strings.HasPrefix(res, "Error") || res == "" {
		return ""
	}
	return strings.TrimSpace(strings.Trim(res, "\n"))
}

func saveTerminalActive(session string) {
	ensureTerminalActiveTable()
	if res := sqliteExecParams(`INSERT INTO terminal_active (id, session) VALUES (1, ?)
		ON CONFLICT(id) DO UPDATE SET session=excluded.session`, session); strings.HasPrefix(res, "Error") {
		log.Printf("[term-active] ERRO salvando sessão ativa: %s", res)
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
		// Copiar TUDO: topo do histórico → begin-selection → fundo → copy.
		// Tudo em um fluxo server-side (o cliente só lê o resultado).
		steps := [][]string{
			{"copy-mode", "-t", target},
			{"send-keys", "-t", target, "-X", "top-line"},
			{"send-keys", "-t", target, "-X", "begin-selection"},
			{"send-keys", "-t", target, "-X", "bottom-line"},
		}
		for _, st := range steps {
			if out, err := exec.Command("tmux", st[:]...).CombinedOutput(); err != nil {
				log.Printf("[term-select] all %s: %v (%s)", target, err, out)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}
		out, err := exec.Command("tmux", "send-keys", "-t", target, "-X", "copy-selection-and-cancel").CombinedOutput()
		if err != nil {
			log.Printf("[term-select] all copy %s: %v (%s)", target, err, out)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		buf, err := exec.Command("tmux", "show-buffer").CombinedOutput()
		if err != nil {
			log.Printf("[term-select] all show-buffer: %v (%s)", err, buf)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": string(buf)})
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

// Registro segue o precedente do projeto (init() em debug_n8n.go /
// hermes_route.go): módulo autocontido, sem tocar no main.go.
func init() {
	http.HandleFunc("/terminal/token", handleTerminalToken)
	http.HandleFunc("/terminal/token/validate", handleTerminalTokenValidate)
	http.HandleFunc("/terminal/ttyd", handleTerminalTTYDProxy)
	http.HandleFunc("/terminal/ttyd/key", handleTerminalTTYDKey)
	http.HandleFunc("/terminal/ttyd/theme", handleTerminalTTYDTheme)
	http.HandleFunc("/terminal/ttyd/close", handleTerminalTTYDClose)
	http.HandleFunc("/terminal/ttyd/scroll", handleTerminalTTYDScroll)
	http.HandleFunc("/terminal/fonts/", serveTerminalFonts)
	http.HandleFunc("/terminal/ttyd/active", handleTerminalTTYDActive)
	http.HandleFunc("/terminal/ttyd/exec", handleTerminalTTYDExec)
	http.HandleFunc("/terminal/ttyd/selection", handleTerminalTTYDSelection)
	log.Println("✅ rotas ttyd registradas via init(): /terminal/token (POST), /terminal/token/validate (GET), /terminal/ttyd (proxy), /terminal/ttyd/close, /terminal/ttyd/scroll (TESTE D)")
}
