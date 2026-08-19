package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os/exec"
	"path"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Classificação de comandos SOMENTE LEITURA (allowlist fail-safe).
//
// Regra de ouro: QUALQUER comando não reconhecido explicitamente como
// somente leitura DEVE cair no gate de aprovação. Nunca o contrário.
// A lista é conservadora e só cresce após revisão/report (ver prompt da task).
// ─────────────────────────────────────────────────────────────────────────────

// binariesReadOnly: binários simples de leitura/diagnóstico (qualquer argv).
var binariesReadOnly = map[string]bool{
	"ls": true, "cat": true, "pwd": true, "whoami": true, "uptime": true,
	"df": true, "free": true, "uname": true, "grep": true, "ps": true,
	"netstat": true, "ss": true,
}

// writeBinaries: binários que podem alterar estado/arquivos. Se QUALQUER token
// do comando (além do binário principal, que já é validado) bater aqui, o
// comando NÃO é read-only. Fail-safe contra contaminação em texto livre
// (ex: "ls -la e depois rm x").
var writeBinaries = map[string]bool{
	"rm": true, "mv": true, "cp": true, "mkdir": true, "rmdir": true,
	"touch": true, "chmod": true, "chown": true, "chgrp": true, "dd": true,
	"mkfs": true, "kill": true, "pkill": true, "killall": true, "systemctl": true,
	"docker": true, "sudo": true, "su": true, "tee": true, "git": true,
	"curl": true, "wget": true, "sed": true, "bash": true, "sh": true,
	"nc": true, "ncat": true, "python": true, "python3": true, "node": true,
	"ruby": true, "perl": true, "echo": true, "printf": true, "make": true,
	"npm": true, "yarn": true, "pip": true, "pip3": true, "apt": true, "yum": true,
}

func containsWriteToken(parts []string) bool {
	for _, p := range parts[1:] {
		if writeBinaries[p] {
			return true
		}
	}
	return false
}

// isReadOnlyCommand decide se um comando (string crua, possivelmente multi-linha
// de uma skill) é somente leitura. Fail-safe: default false.
func isReadOnlyCommand(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	// Bloqueia encadeamento/redirecionamento/metachar de shell SEMPRE.
	// Pipes não são permitidos por serem difíceis de validar segmento a
	// segmento — conservador (pode ser expandido após revisão).
	for _, bad := range []string{";", "&&", "||", ">", ">>", "<", "`", "$(", "|"} {
		if strings.Contains(cmd, bad) {
			return false
		}
	}
	lines := strings.Split(cmd, "\n")
	if len(lines) > 1 {
		// Skill multi-linha: TODAS as linhas não vazias devem ser read-only.
		all := true
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			if !isReadOnlyCommandLine(l) {
				all = false
				break
			}
		}
		return all
	}
	return isReadOnlyCommandLine(cmd)
}

func isReadOnlyCommandLine(line string) bool {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return false
	}
	bin := path.Base(parts[0]) // aceita "ls" e "/bin/ls"

	switch bin {
	case "ls", "pwd", "whoami", "uptime", "uname", "df", "free", "ps", "netstat", "ss":
		if containsWriteToken(parts) || containsSecretPath(line) {
			return false
		}
		return true
	case "cat", "grep":
		// leitura de arquivos: bloqueia caminhos sensíveis (defesa em profundidade)
		if containsWriteToken(parts) || containsSecretPath(line) {
			return false
		}
		return true
	case "find":
		if containsWriteToken(parts) || containsSecretPath(line) {
			return false
		}
		for _, bad := range []string{"-delete", "-exec", "-execdir", "-ok", "-fprint", "-fprintf"} {
			if strings.Contains(line, bad) {
				return false
			}
		}
		return true
	case "git":
		// apenas leitura: log/status/diff/show. git commit/push/reset etc → gate.
		if len(parts) < 2 {
			return false
		}
		switch parts[1] {
		case "log", "status", "diff", "show":
			return !containsWriteToken(parts)
		default:
			return false
		}
	case "curl":
		return isReadOnlyCurl(parts) && !containsWriteToken(parts)
	case "systemctl":
		// SOMENTE status. start/stop/restart/etc → gate.
		return len(parts) >= 2 && parts[1] == "status" && !containsWriteToken(parts) && !containsSecretPath(line)
	case "top":
		// apenas batch de uma iteração (top -bn1 / top -b -n 1)
		if containsWriteToken(parts) {
			return false
		}
		batch := false
		count := false
		for _, p := range parts[1:] {
			if strings.HasPrefix(p, "-bn") {
				return true // top -bn1
			}
			if p == "-b" {
				batch = true
			}
			if p == "-n" {
				count = true
			}
		}
		return batch && count
	default:
		return false // fail-safe: binário não reconhecido → gate
	}
}

// hasAnyArg verifica se um argv (a partir do índice 1) contém o token exato.
func hasAnyArg(parts []string, want string) bool {
	for _, p := range parts[1:] {
		if p == want {
			return true
		}
	}
	return false
}

// bashExecCmdArg extrai o campo "cmd" do argsJSON do bash_exec do agente.
func bashExecCmdArg(argsJSON string) string {
	var args struct {
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	return strings.TrimSpace(args.Cmd)
}

// isReadOnlyCurl: somente GET/HEAD para hosts internos conhecidos, sem
// payload/upload/escrita de arquivo.
func isReadOnlyCurl(parts []string) bool {
	if containsSecretPath(strings.Join(parts, " ")) {
		return false
	}
	var target string
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		lp := strings.ToLower(p)
		switch {
		case strings.HasPrefix(lp, "http://"), strings.HasPrefix(lp, "https://"):
			target = lp
		case !strings.Contains(p, "://") && !strings.HasPrefix(p, "-") && target == "":
			// URL sem esquema (ex: "curl localhost/health") → assume http
			target = "http://" + lp
		case lp == "-x" || lp == "--request":
			return false // método explícito não-GET não permitido (default curl é GET)
		case lp == "-d" || lp == "--data" || lp == "--data-binary" ||
			lp == "--data-raw" || lp == "--data-urlencode" ||
			lp == "-f" || lp == "--form" || lp == "-t" || lp == "--upload-file":
			return false // POST/payload/upload
		case lp == "-o" || lp == "--output" || lp == "--create-dirs":
			// escrita de arquivo só permitida se for /dev/null (descartável)
			if lp == "-o" || lp == "--output" {
				if i+1 >= len(parts) || parts[i+1] != "/dev/null" {
					return false
				}
			} else {
				return false
			}
		}
	}
	if target == "" {
		return false
	}
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host != "localhost" && host != "127.0.0.1" && host != "::1" && host != "0.0.0.0" {
		return false // só endpoints internos conhecidos
	}
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// Ajuda para prompts dos motores Claude Code / OpenCode (linguagem natural).
// Extrai blocos de comando do prompt; se TODOS forem read-only e existir ao
// menos um, o prompt pode executar sem gate. Sem comando extraído → gate
// (fail-safe).
// ─────────────────────────────────────────────────────────────────────────────

func extractCommandCandidates(prompt string) []string {
	var out []string
	for _, line := range strings.Split(prompt, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "```") {
			continue // marcadores de bloco; o conteúdo interno já é processado por linha
		}
		t = strings.TrimPrefix(t, "$")
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		toks := strings.Fields(t)
		joined := strings.Join(toks, " ")
		offset := 0
		for _, tok := range toks {
			bin := path.Base(tok)
			if binariesReadOnly[bin] || bin == "git" || bin == "curl" ||
				bin == "systemctl" || bin == "find" || bin == "top" {
				// candidato: do token de comando read-only até o fim da linha.
				// O isReadOnlyCommand valida contaminação (write tokens, metachar).
				out = append(out, joined[offset:])
				break
			}
			offset += len(tok) + 1
		}
	}
	return out
}

// promptContainsOnlyReadOnlyCommands: fail-safe — exige ao menos 1 comando
// extraído e TODOS read-only.
func promptContainsOnlyReadOnlyCommands(prompt string) bool {
	cands := extractCommandCandidates(prompt)
	if len(cands) == 0 {
		return false
	}
	for _, c := range cands {
		if !isReadOnlyCommand(c) {
			return false
		}
	}
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// Execução imediata (sem gate) de comando read-only + auditoria dedicada.
// NÃO usa self_modifications (reservado a mudanças reais) — usa a tabela
// command_execution_log.
// ─────────────────────────────────────────────────────────────────────────────

func logCommandExecution(tenantID, source, cmd string, outputLen int, status string) {
	sqliteExecParams(
		`INSERT INTO command_execution_log (tenant_id, source, cmd, output_len, status)
		 VALUES (?, ?, ?, ?, ?)`,
		tenantID, source, cmd, outputLen, status,
	)
}

func executeReadOnlyCommand(cmd, source, tenantID string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), approvedCommandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "bash", "-c", cmd).CombinedOutput()
	output := string(out)
	if err != nil {
		if output == "" {
			output = err.Error()
		}
		logCommandExecution(tenantID, source, cmd, len(output), "error")
		log.Printf("[command_execution_log] read-only ERRO source=%s tenant=%s err=%v", source, tenantID, err)
		return output, err
	}
	logCommandExecution(tenantID, source, cmd, len(output), "ok")
	log.Printf("[command_execution_log] read-only OK source=%s tenant=%s output_len=%d", source, tenantID, len(output))
	return output, nil
}

// executarReadOnlyPronto é usado pelas skills/task agent: retorna a string
// formatada para aparecer no chat.
func runReadOnlyForChat(cmd, source, tenantID string) string {
	out, err := executeReadOnlyCommand(cmd, source, tenantID)
	if err != nil {
		return fmt.Sprintf("❌ Comando (somente leitura) falhou.\n\n%s", out)
	}
	return fmt.Sprintf("✅ Comando executado (somente leitura, sem aprovação).\n\n%s", out)
}
