package main

import (
	"context"
	"encoding/json"
	"fmt"
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

func getClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.RemoteAddr
	}
	return strings.Split(ip, ":")[0]
}

func requireHokAuth(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("X-Hok-Token")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token != HOK_API_TOKEN {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"status": "unauthorized"})
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
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-HOK-TOKEN, X-N8N-Token")
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
				filePath = filepath.Join(ROOT_PATH, strings.TrimPrefix(filePath, "~/ecossistema/"))
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
						errorsFixed++
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
