package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func checkRateLimit(ip string, maxReq int) bool {
	rateLimiterMu.Lock()
	defer rateLimiterMu.Unlock()
	now := time.Now()
	times := rateLimiter[ip]
	valid := []time.Time{}
	for _, t := range times {
		if now.Sub(t) < time.Minute {
			valid = append(valid, t)
		}
	}
	if len(valid) >= maxReq {
		rateLimiter[ip] = valid
		return false
	}
	rateLimiter[ip] = append(valid, now)
	return true
}

// cloudflareIPRanges — CIDR oficiais do Cloudflare (cloudflare.com/ips-v4 e /ips-v6),
// copiados em 2026-08-14. Só confiamos em X-Forwarded-For se a conexão direta
// vier destas faixas.
var cloudflareIPRanges = []string{
	// IPv4
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	// IPv6
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
}

// cloudflareProxyAllowed — decide se a conexão direta vem de um proxy
// Cloudflare (edge), caso em que X-Forwarded-For pode ser confiado.
func cloudflareProxyAllowed(remoteIP string) bool {
	ip := net.ParseIP(remoteIP)
	if ip == nil {
		return false
	}
	for _, cidr := range cloudflareIPRanges {
		_, netw, err := net.ParseCIDR(cidr)
		if err == nil && netw.Contains(ip) {
			return true
		}
	}
	return false
}

// getClientIP — IP do cliente para rate limit. Fonte de verdade: RemoteAddr.
// Só aceita X-Forwarded-For quando a conexão direta vem de um proxy Cloudflare
// (impede bypass por rotação do header — varredura 12/08, item pendente).
func getClientIP(r *http.Request) string {
	remoteHost := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteHost = host
	}
	if cloudflareProxyAllowed(remoteHost) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	return remoteHost
}

func requireOwnerToken(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("X-Hok-Token")
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(HOK_API_TOKEN)) != 1 {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"status": "unauthorized"})
		return false
	}
	return true
}

// roleAuthorized — JWT de clientes NÃO tem acesso a endpoints administrativos.
// Apenas role owner/admin (ou X-Hok-Token) passam.
func roleAuthorized(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("X-Hok-Token")
	if token != "" {
		if subtle.ConstantTimeCompare([]byte(token), []byte(HOK_API_TOKEN)) == 1 {
			return true
		}
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"status": "unauthorized"})
		return false
	}
	auth := r.Header.Get("Authorization")
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"status": "unauthorized"})
		return false
	}
	claims, err := parseJWT(parts[1])
	if err != nil {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"status": "unauthorized"})
		return false
	}
	role, _ := claims["role"].(string)
	if role != "owner" && role != "admin" {
		w.WriteHeader(403)
		respondJSON(w, map[string]string{"status": "forbidden", "message": "role nao autorizada para este endpoint"})
		return false
	}
	return true
}

func requireHokAuth(w http.ResponseWriter, r *http.Request) bool {
	if !roleAuthorized(w, r) {
		return false
	}
	return true
}

// ─── Telemetria ──────────────────────────────────────────────────────────────
func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func containsAny(s string, keywords []string) bool {
	for _, k := range keywords {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-HOK-TOKEN, X-N8N-Token, X-Conversation-Id")
	w.Header().Set("Content-Type", "application/json")
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	json.NewEncoder(w).Encode(v)
}

// respondStreamNDJSON envia um texto ja pronto simulando streaming,
// quebrando em palavras e mandando uma linha NDJSON {"delta":"..."} por vez,
// com flush apos cada linha. Usado quando o cliente pede stream:true mas o
// backend so tem a resposta completa (nao streaming real do provedor).
func respondStreamNDJSON(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	flusher, ok := w.(http.Flusher)
	if !ok {
		// Sem suporte a flush: manda tudo de uma vez
		enc := json.NewEncoder(w)
		enc.Encode(map[string]string{"delta": text})
		return
	}
	words := strings.Split(text, " ")
	for i, word := range words {
		chunk := word
		if i < len(words)-1 {
			chunk += " "
		}
		line, err := json.Marshal(map[string]string{"delta": chunk})
		if err != nil {
			continue
		}
		w.Write(line)
		w.Write([]byte("\n"))
		flusher.Flush()
		time.Sleep(18 * time.Millisecond)
	}
}

// ─── Executor de comandos ─────────────────────────────────────────────────────
func executeCommand(cmdStr string) string {
	blocked := []string{
		"rm -rf /", "rm -rf ~", "rm -rf *", "rm -rf .",
		"mkfs", "dd if=", "chmod 000", "curl | bash", "wget | bash",
	}
	for _, b := range blocked {
		if strings.Contains(cmdStr, b) {
			return "⚠️ Comando bloqueado por segurança."
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	out, _ := cmd.CombinedOutput()
	res := strings.TrimSpace(string(out))
	if len(res) > 4000 {
		res = res[:4000] + "\n..."
	}
	return res
}

func executeCommandWithSelfHealing(cmdStr string) string {
	output := executeCommand(cmdStr)
	if strings.Contains(output, "Traceback (most recent call last):") {
		filePath := ""
		for _, w := range strings.Fields(cmdStr) {
			if strings.HasSuffix(w, ".py") {
				filePath = w
				break
			}
		}
		if filePath != "" {
			if !filepath.IsAbs(filePath) {
				filePath = filepath.Join(ROOT_PATH, strings.TrimPrefix(filePath, "~/hokma/"))
			}
			if code, err := os.ReadFile(filePath); err == nil {
				prompt := fmt.Sprintf(
					"Script %s falhou:\n---\n%s\n---\nCódigo:\n---\n%s\n---\nCorriga. Retorne APENAS código limpo.",
					filePath, output, string(code))
				msgs := []Message{
					{Role: "system", Content: "Depurador de precisão. Responda só com código limpo."},
					{Role: "user", Content: prompt},
				}
				if fixed, err := callDeepSeek("deepseek-chat", msgs); err == nil && fixed != "" {
					fixed = strings.TrimPrefix(strings.TrimPrefix(fixed, "```python"), "```")
					fixed = strings.TrimSuffix(strings.TrimSpace(fixed), "```")
					os.WriteFile(filePath, []byte(fixed), 0755)
					if second := executeCommand(cmdStr); !strings.Contains(second, "Traceback") {
						teleMu.Lock()
						errorsFixed++
						teleMu.Unlock()
						return "🛠️ *[AUTO-DEBUG]* Corrigido!\n\n" + second
					}
				}
			}
		}
	}
	return output
}

// ─── SQLite ───────────────────────────────────────────────────────────────────

// containsSecurityKeyword — detecta se prompt é sobre segurança/cibersegurança
func containsSecurityKeyword(msg string) bool {
	msgLower := strings.ToLower(msg)
	keywords := []string{
		"security", "segurança", "seguranca", "vulnerab", "vuln",
		"exploit", "pentest", "cve-", "cvss", "auditoria",
		"audit", "jwt", "token", "sql injection", "xss",
		"csrf", "hardening", "owasp", "threat", "ameaça",
		"malware", "ransomware", "phishing", "ethical hack",
		"red team", "blue team", "soc ", "siem", "firewall",
		"criptograf", "encrypt", "hash seguro", "tls ", "ssl ",
	}
	for _, k := range keywords {
		if strings.Contains(msgLower, k) {
			return true
		}
	}
	return false
}
