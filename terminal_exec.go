package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────
// Integração Chat ↔ Terminal (FIX 22/08): mensagens do chat admin executam
// comandos na sessão PTY ATIVA do próprio token admin (a mesma do terminal
// web). Segurança em camadas:
//   - só chega aqui quem passou por /chat/smart + requireHokAuth (webhooks de
//     leads/WhatsApp usam outros fluxos e nunca chamam runSmartText);
//   - a sessão usada é SEMPRE a do HOK_TOKEN do processo (terminalUserKey) —
//     o chat nunca digita em PTY de outro usuário;
//   - blocklist de comandos destrutivos recusa e audita;
//   - todo comando é auditado em log (timestamp, user, comando, sessão).
//
// Captura: tap temporário no broadcast do PTY (fan-out aditivo em
// terminal_session.go) até aparecer marcador único por invocação; timeout de
// 15s devolve output parcial com nota.
// ─────────────────────────────────────────────────────────────────────────

const (
	terminalExecTimeout = 15 * time.Second
	terminalTapBuffer   = 256
)

// terminalANSIRe remove códigos ANSI/OSC/DCS do output capturado.
var terminalANSIRe = regexp.MustCompile(
	`\x1b\[[0-9;:?]*[\x20-\x2f]*[A-Za-z]` + // CSI ... final byte
		`|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)` + // OSC ... BEL/ST
		`|\x1bP[\x20-\x7e]*?\x1b\\` + // DCS
		`|\x1b[()][A-Z0-9]` + // charset selection
		`|\x1b[=>]`) // keypad modes

// terminalExecBlocklist: substrings que recusam a execução via chat.
var terminalExecBlocklist = []string{
	"rm -rf /", "rm -fr /", "mkfs", "dd if=", "> /dev/sd",
	"shutdown", "reboot", "halt", "poweroff", "init 0", "init 6",
	":(){", "chmod -r 777 /",
}

// terminalVerbPrefixes: NL aceita SOMENTE verbo explícito no início da
// mensagem (superfície apertada de propósito — as heurísticas largas de
// containsTerminalKeyword NÃO executam nada sozinhas).
var terminalVerbPrefixes = []string{"rode ", "roda ", "execute ", "executa ", "executar ", "run "}

// extractTerminalCommand devolve o comando a executar ou "" se a mensagem não
// contém comando utilizável (o caller retorna nil e a cascata segue normal).
func extractTerminalCommand(msg string) string {
	t := strings.TrimSpace(msg)
	lower := strings.ToLower(t)
	for _, p := range []string{"/terminal ", "/term "} {
		if strings.HasPrefix(lower, p) {
			return strings.TrimSpace(t[len(p):])
		}
	}
	for _, p := range terminalVerbPrefixes {
		if strings.HasPrefix(lower, p) {
			cmd := strings.TrimSpace(extractBashCommand(t))
			cl := strings.ToLower(cmd)
			// limpa coletivos de cortesia que virariam lixo de shell
			for _, suf := range []string{" pra mim", "por favor", " agora", " no terminal"} {
				for strings.HasSuffix(cl, suf) {
					cmd = strings.TrimSpace(cmd[:len(cmd)-len(suf)])
					cl = strings.ToLower(cmd)
				}
			}
			return cmd
		}
	}
	return ""
}

// tryTerminalExec: engine da cascata para execução no PTY via chat admin.
func tryTerminalExec(msg string, chatUserID string) *smartTextResult {
	if !containsTerminalKeyword(msg) {
		return nil
	}
	cmd := extractTerminalCommand(msg)
	if cmd == "" || strings.ContainsAny(cmd, "\n\r") {
		return nil // sem comando utilizável -> próximos engines (claude_code/chat)
	}
	lowerCmd := strings.ToLower(cmd)
	for _, bad := range terminalExecBlocklist {
		if strings.Contains(lowerCmd, bad) {
			log.Printf("[AUDIT] terminal_exec BLOQUEADO user=%s cmd=%q ts=%s",
				chatUserID, cmd, time.Now().Format(time.RFC3339))
			return &smartTextResult{
				reply: "⛔ Comando bloqueado por política de segurança: `" + cmd + "`.\nUse o terminal web para comandos dessa natureza.",
				mode:  "terminal_exec_blocked", engine: "terminal",
			}
		}
	}

	// PONTE CHAT→TTYD (23/08): se houver sessão ttyd VISÍVEL registrada pelo
	// frontend (aba ativa), injeta o comando nela via key injection — comando
	// e output aparecem no terminal do usuário, e o chat recebe o output
	// capturado. Sem sessão registrada/ativa → cai no caminho legado abaixo
	// (PTY backend headless), preservando quem usa só o chat.
	if target := registeredActiveTTYD(); target != "" {
		output, refused, timedOut := ttydBridgeExec(target, cmd)
		if refused != "" {
			log.Printf("[AUDIT] terminal_exec RECUSADO-TUI(user) user=%s target=%s cmd=%q motivo=%q ts=%s",
				chatUserID, target, cmd, refused, time.Now().Format(time.RFC3339))
			return &smartTextResult{
				reply: "⚠️ Recusado: " + refused + ".\nFeche-o (ou volte ao prompt do shell) e reenvie o comando.",
				mode:  "terminal_exec_busy", engine: "terminal",
			}
		}
		if timedOut {
			log.Printf("[AUDIT] terminal_exec bridge TIMEOUT user=%s target=%s cmd=%q ts=%s",
				chatUserID, target, cmd, time.Now().Format(time.RFC3339))
		}
		return &smartTextResult{reply: output, mode: "terminal_exec", engine: "terminal"}
	}

	// Sessão-alvo: SEMPRE a do próprio token admin do processo.
	adminKey := terminalUserKey(os.Getenv("HOK_TOKEN"))
	s := findActiveSession(adminKey)
	if s == nil {
		log.Printf("[AUDIT] terminal_exec SEM SESSAO user=%s cmd=%q ts=%s",
			chatUserID, cmd, time.Now().Format(time.RFC3339))
		return &smartTextResult{
			reply: "⚠️ Nenhuma sessão de terminal ativa no momento. Abra o Terminal no app e tente novamente.",
			mode:  "terminal_exec", engine: "terminal",
		}
	}

	// GATE CRÍTICO (FIX 22/08 bug TUI; revisado pós-tmux): recusar digitação
	// automática quando o painel ativo não está num shell pronto. Com o PTY
	// dentro do tmux, foregroundPgrp só enxerga o client tmux (gate ficava
	// inefetivo) — a checagem real é #{pane_current_command}, centralizada em
	// terminalGateReason (terminal_session.go), com fallback ioctl p/ bash puro.
	if reason := s.terminalGateReason(); reason != "" {
		log.Printf("[AUDIT] terminal_exec RECUSADO-TUI user=%s session=%s cmd=%q motivo=%q ts=%s",
			chatUserID, s.ID, cmd, reason, time.Now().Format(time.RFC3339))
		return &smartTextResult{
			reply: "⚠️ Recusado: " + reason + ".\nFeche-o (ou volte ao prompt do shell) e reenvie o comando.",
			mode:  "terminal_exec_busy", engine: "terminal",
		}
	}

	marker := fmt.Sprintf("___HOK_CMD_DONE_%d___", time.Now().UnixNano())
	log.Printf("[AUDIT] terminal_exec user=%s session=%s cmd=%q ts=%s",
		chatUserID, s.ID, cmd, time.Now().Format(time.RFC3339))

	tap := s.addTap()
	defer s.removeTap(tap)
	// Marcador em LINHA PRÓPRIA, sem "&&": roda mesmo se o comando sair com
	// exit != 0.
	s.writeInput(cmd + "\necho " + marker + "\n")

	var raw strings.Builder
	timeout := time.After(terminalExecTimeout)
	done, timedOut := false, false
	for !done && !timedOut {
		select {
		case chunk, ok := <-tap:
			if !ok {
				done = true // sessão fechou durante a captura
				break
			}
			if raw.Len() < 1<<20 { // teto de memória; marker segue detectável nos chunks novos
				raw.Write(chunk)
			} else if strings.Contains(string(chunk), "___HOK_CMD_DONE_") {
				done = true
			}
			// FIX definitivo: o marcador aparece EXATAMENTE 2 vezes no stream —
			// 1ª no eco do nosso input ("echo <marker>"), 2ª como resultado da
			// execução (formato varia: \r\n, \x1b[?2004l\r etc.). Completo
			// quando a 2ª ocorrência chega. Nano-time torna colisão impossível.
			if strings.Count(raw.String(), marker) >= 2 {
				done = true
			}
		case <-timeout:
			timedOut = true
		}
	}

	reply := strings.TrimRight(cleanCapturedOutput(raw.String(), cmd, marker), "\n ")
	if timedOut {
		note := "_[comando ainda em execução, output parcial]_"
		if reply == "" {
			reply = "(sem output ainda)\n\n" + note
		} else {
			reply += "\n\n" + note
		}
	}
	return &smartTextResult{reply: reply, mode: "terminal_exec", engine: "terminal"}
}

// cleanCapturedOutput: strip ANSI -> normaliza \r -> descarta ecos do nosso
// input (linhas com o marcador que começam com "echo ", eco/redesenho do
// comando digitado) e prompts do PS1 -> encerra na linha do marcador
// EXECUTADO (standalone), que nunca entra no reply.
var promptRe = regexp.MustCompile(`^\S+@\S+:\S+[#$]$`)

func cleanCapturedOutput(raw string, cmd string, marker string) string {
	text := terminalANSIRe.ReplaceAllString(raw, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	cmdCore := strings.TrimSpace(cmd)
	out := make([]string, 0, 64)
	echoed := false
	sawOutput := false
	for _, ln := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(ln)
		if marker != "" && strings.Contains(ln, marker) {
			// marcador EXECUTADO (linha pura) = fim da captura relevante;
			// variantes de eco ("echo <marker>", prompt+echo) só são puladas.
			if trim == marker {
				break
			}
			continue
		}
		// prompt do PS1 (root@host:~/caminho#) é ruído pro chat
		if promptRe.MatchString(trim) {
			continue
		}
		if !echoed {
			if cmdCore != "" && strings.Contains(ln, cmdCore) {
				echoed = true // pulou o eco/redesenho do comando digitado
			}
			continue
		}
		// redesenho do comando (prompt + cmd) antes do primeiro output real:
		// pula também, senão vira lixo na resposta do chat
		if !sawOutput && cmdCore != "" && strings.Contains(ln, cmdCore) {
			continue
		}
		if strings.TrimSpace(ln) != "" {
			sawOutput = true
		}
		out = append(out, strings.TrimRight(ln, " "))
	}
	return strings.Trim(strings.Join(out, "\n"), "\n ")
}
