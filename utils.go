package main

import (
	"bufio"
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

func setCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-HOK-TOKEN, X-N8N-Token, X-Conversation-Id")
	w.Header().Set("Content-Type", "application/json")
}

func respondJSON(w http.ResponseWriter, v interface{}) {
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "status": "error"})
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

// ─── env_tools.go (consolidado) ──────────────────────────────────────────────
//
// Ferramenta de diagnostico do arquivo .env do backend. Detecta os padroes
// de bug reais encontrados na migracao Hetzner -> Hostinger:
//   - chaves duplicadas (causou autenticacao inconsistente com N8N_API_KEY)
//   - URLs apontando para localhost/127.0.0.1 (causou N8N_BASE_URL orfa)
//   - valores vazios
//
// SEGURANCA: esta tool NUNCA devolve o valor real de nenhuma variavel para
// o modelo. Todo valor e mascarado antes de sair da funcao.

const envDefaultPath = "/root/hokma/backend/.env"

// envMaskValue mostra so os primeiros 3 caracteres do valor, o resto vira "***".
// Nunca expõe o valor real da credencial para o modelo.
func envMaskValue(v string) string {
	if len(v) == 0 {
		return "(vazio)"
	}
	if len(v) <= 3 {
		return "***"
	}
	return v[:3] + "***(" + fmt.Sprintf("%d chars", len(v)) + ")"
}

// envDiagnoseConfig analisa o arquivo .env do backend em busca de problemas
// estruturais conhecidos, sem nunca expor valores reais de credenciais.
// args (JSON): { "path": "..." } (opcional, default /root/hokma/backend/.env)
func envDiagnoseConfig(args string) string {
	var raw map[string]string
	_ = json.Unmarshal([]byte(args), &raw) // args pode vir vazio, tudo bem

	path := envDefaultPath
	if p, ok := raw["path"]; ok && p != "" {
		path = p
	}

	f, err := os.Open(path)
	if err != nil {
		return errJSON("nao foi possivel abrir " + path + ": " + err.Error())
	}
	defer f.Close()

	type occurrence struct {
		Line   int    `json:"line"`
		Masked string `json:"masked_value"`
	}

	keyOccurrences := map[string][]occurrence{}
	lineNum := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		keyOccurrences[key] = append(keyOccurrences[key], occurrence{
			Line:   lineNum,
			Masked: envMaskValue(value),
		})
	}

	type finding struct {
		Key    string `json:"key"`
		Issue  string `json:"issue"`
		Detail string `json:"detail"`
	}
	var findings []finding

	for key, occs := range keyOccurrences {
		// checagem 1: chave duplicada
		if len(occs) > 1 {
			lines := []string{}
			for _, o := range occs {
				lines = append(lines, fmt.Sprintf("linha %d (%s)", o.Line, o.Masked))
			}
			findings = append(findings, finding{
				Key:    key,
				Issue:  "chave_duplicada",
				Detail: fmt.Sprintf("Aparece %d vezes: %s. O comportamento real depende de qual linha o systemd carrega por ultimo - risco de inconsistencia.", len(occs), strings.Join(lines, "; ")),
			})
		}
		// checagem 2: valor vazio
		for _, o := range occs {
			if o.Masked == "(vazio)" {
				findings = append(findings, finding{
					Key:    key,
					Issue:  "valor_vazio",
					Detail: fmt.Sprintf("Linha %d sem valor definido.", o.Line),
				})
			}
		}
		// checagem 3: URL apontando para localhost/127.0.0.1 em variavel *_URL
		if strings.Contains(strings.ToUpper(key), "URL") {
			// precisa reabrir a linha original pra checar o valor sem mascarar
			// (checagem de padrao, nao expoe o valor - so confirma presenca do padrao)
			f2, err := os.Open(path)
			if err == nil {
				sc2 := bufio.NewScanner(f2)
				ln := 0
				for sc2.Scan() {
					ln++
					l := strings.TrimSpace(sc2.Text())
					if strings.HasPrefix(l, key+"=") {
						val := strings.TrimSpace(strings.TrimPrefix(l, key+"="))
						if strings.Contains(val, "127.0.0.1") || strings.Contains(val, "localhost") {
							findings = append(findings, finding{
								Key:    key,
								Issue:  "url_localhost_suspeita",
								Detail: fmt.Sprintf("Linha %d aponta para localhost/127.0.0.1. Se essa variavel deveria apontar para um servico externo, isso pode sobrescrever silenciosamente o endpoint correto (mesmo padrao do bug N8N_BASE_URL orfa).", ln),
							})
						}
					}
				}
				f2.Close()
			}
		}
	}

	status := "saudavel"
	if len(findings) > 0 {
		status = "problemas_encontrados"
	}

	out, err := json.Marshal(map[string]any{
		"status":       "ok",
		"arquivo":      path,
		"total_chaves": len(keyOccurrences),
		"diagnostico":  status,
		"total_issues": len(findings),
		"findings":     findings,
	})
	if err != nil || len(out) == 0 {
		return `{"status":"error","error":"falha ao montar diagnostico do .env"}`
	}
	return string(out)
}

// ─── debug_tools.go (consolidado) ────────────────────────────────────────────

func handleDebugTools(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	tools := agentTools()
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tools); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
