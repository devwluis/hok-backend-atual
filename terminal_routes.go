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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	terminalTokenTTL  = 5 * time.Minute
	terminalPublicURL = "https://terminal.imoveischaves.com"
	ttydUpstream      = "http://127.0.0.1:7681"
	termCookieName    = "hok_term_tok"
)

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
				// Todo tráfego deste hostname pertence ao ttyd: força raiz
				// (o path público /terminal/ttyd não existe no upstream).
				pr.Out.URL.Path = "/"
				pr.Out.URL.RawPath = ""
				pr.Out.Host = target.Host
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

func hasTokenCookie(r *http.Request) bool {
	ck, err := r.Cookie(termCookieName)
	return err == nil && ck.Value != ""
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"status":"unauthorized","message":"token ausente ou expirado — solicite POST /terminal/token"}`))
}

// Registro segue o precedente do projeto (init() em debug_n8n.go /
// hermes_route.go): módulo autocontido, sem tocar no main.go.
func init() {
	http.HandleFunc("/terminal/token", handleTerminalToken)
	http.HandleFunc("/terminal/token/validate", handleTerminalTokenValidate)
	http.HandleFunc("/terminal/ttyd", handleTerminalTTYDProxy)
	log.Println("✅ rotas ttyd registradas via init(): /terminal/token (POST), /terminal/token/validate (GET), /terminal/ttyd (proxy)")
}
