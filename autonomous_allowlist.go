package main

import (
	"path"
	"strings"
)

// ─── ALLOWLIST DO MODO AUTÔNOMO (31/08) ─────────────────────────────────────
// Pendência do adendo de session_mode (seção 5): quando session_mode ==
// "autonomous"/"autonomous_total", o que PODE executar sem aprovação é UMA
// ALLOWLIST EXPLÍCITA — não uma blocklist. Comando fora da allowlist cai no
// fluxo de aprovação humana (nunca fail-open). Reutiliza o padrão da Opção A'
// do bash_exec (allowlist + validação rígida, sem shell arbitrário).
//
// NUNCA entram aqui (proibidos mesmo em modo autônomo): rm -rf /, mkfs/dd,
// shutdown/reboot, systemctl start/stop/restart/kill, git push/commit/reset/
// rebase/merge, sudo/su, escrita via curl -X/--data, caminhos sensíveis.

// autonomousDecision — resultado do gate allowlist do modo autônomo.
type autonomousDecision int

const (
	autonomousExec          autonomousDecision = iota // pode executar sem aprovação
	autonomousNeedsApproval                            // fora da allowlist → pendência humana
	autonomousForbidden                                // proibido mesmo em autônomo → bloqueado
)

// autonomousAllowlist — lista EXPLÍCITA de comandos que podem rodar sem
// aprovação no modo autônomo. Prefixos por binário (e subcomando quando
// necessário). Só leitura / diagnóstico / workflow seguro do projeto.
func autonomousAllowlist() []string {
	return []string{
		// diagnóstico somente leitura
		"ls", "cat", "pwd", "whoami", "uptime", "uname",
		"df", "free", "ps", "netstat", "ss", "top", "find", "grep",
		// git somente leitura (log/status/diff/show — nunca push/commit)
		"git status", "git log", "git diff", "git show",
		// serviço: SOMENTE status (nunca start/stop/restart/kill)
		"systemctl status",
		// HTTP interno somente leitura (GET) — validado por isReadOnlyCurl
		"curl",
	}
}

// autonomousNeverAllowed — comandos PROIBIDOS mesmo em modo autônomo
// (consistente com terminalExecBlocklist + destructiveSignals + gates de
// segurança existentes). Retorna o motivo quando bate; "" se não.
func autonomousNeverAllowed(cmd string) string {
	lower := strings.ToLower(cmd)
	never := []string{
		"rm -rf /", "rm -fr /", "mkfs", "dd if=", "> /dev/sd",
		"shutdown", "reboot", "halt", "poweroff", "init 0", "init 6",
		":(){", "chmod -r 777 /",
		"systemctl start", "systemctl stop", "systemctl restart", "systemctl kill",
		"git push", "git commit", "git reset", "git rebase", "git merge",
		"sudo", "su ",
		"curl -x", "curl --request", "curl -d", "curl --data", "curl -f ", "curl --form", "curl -t ", "curl --upload-file",
		"wget ",
	}
	for _, n := range never {
		if strings.Contains(lower, n) {
			return "comando proibido em modo autônomo: " + n
		}
	}
	return ""
}

// autonomousMatchesAllowlist — o primeiro binário (+ subcomando quando a
// entrada da allowlist tem 2 tokens) precisa casar com uma entrada explícita.
func autonomousMatchesAllowlist(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) == 0 {
		return false
	}
	bin := path.Base(parts[0])
	for _, entry := range autonomousAllowlist() {
		etoks := strings.Fields(entry)
		if len(etoks) == 0 {
			continue
		}
		if path.Base(etoks[0]) != bin {
			continue
		}
		if len(etoks) > 1 {
			if len(parts) < 2 || parts[1] != etoks[1] {
				continue
			}
		}
		return true
	}
	return false
}

// autonomousCommandAllowed — decisão por comando individual. Exige: não
// proibido + prefixo na allowlist + validação read-only reforçada
// (write tokens, caminhos sensíveis, metachar de shell, curl GET).
func autonomousCommandAllowed(cmd string) bool {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return false
	}
	if autonomousNeverAllowed(cmd) != "" {
		return false
	}
	if !autonomousMatchesAllowlist(cmd) {
		return false
	}
	if !isReadOnlyCommand(cmd) {
		return false
	}
	return true
}

// promptHasActionSignal — sinal de AÇÃO (efeito colateral) em linguagem
// natural: destrutivo (destructiveSignals) + escrita (writeSignals) + verbos
// de build/deploy/instalação. Usado quando o prompt NÃO tem comando read-only
// explícito: se parece ação, vai pra aprovação (fail-safe).
func promptHasActionSignal(prompt string) bool {
	lower := strings.ToLower(prompt)
	for _, w := range destructiveSignals {
		if strings.Contains(lower, w) {
			return true
		}
	}
	for _, w := range writeSignals {
		if strings.Contains(lower, w) {
			return true
		}
	}
	for _, w := range []string{
		"deploy", "build", "compilar", "compile", "instale", "instalar",
		"npm install", "pip install", "apt ", "yum ",
		"rode o comando", "roda o comando", "execute o comando",
		"rode ", "roda ", "execute ", "executa ", "run ",
		"grava", "gravar", "gera", "gerar", "cria", "criar arquivo",
	} {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// autonomousGate — gate da allowlist para o prompt do modo autônomo.
//   - autonomousForbidden: tem comando proibido (rm -rf, systemctl restart,
//     git push, sudo...) → NUNCA executa no modo autônomo.
//   - autonomousNeedsApproval: tem ação com efeito colateral fora da allowlist
//     → cai no fluxo de aprovação humana (não executa direto).
//   - autonomousExec: mensagem conversacional OU só comandos da allowlist
//     (read-only) → pode executar sem aprovação.
func autonomousGate(prompt string) (autonomousDecision, string) {
	if r := autonomousNeverAllowed(prompt); r != "" {
		return autonomousForbidden, r
	}
	cands := extractCommandCandidates(prompt)
	if len(cands) == 0 {
		// sem comando read-only explícito: se tem sinal de ação (destrutivo/
		// escrita/build), vai pra aprovação; senão é trabalho normal do agente.
		if promptHasActionSignal(prompt) {
			return autonomousNeedsApproval, "ação com efeito colateral fora da allowlist do modo autônomo"
		}
		return autonomousExec, ""
	}
	for _, c := range cands {
		if !autonomousCommandAllowed(c) {
			return autonomousNeedsApproval, "comando fora da allowlist do modo autônomo: " + c
		}
	}
	return autonomousExec, ""
}