package main

import "testing"

// FIX 20/08 (lixo no prompt após TUI filho, ex: opencode): respostas de
// terminal emulador (CPR/DSR/DA) órfãs não podem virar input do bash.
// Testes do stripper usado quando o bash é o foreground do pty.

func TestStripTerminalResponses_CPROrfao(t *testing.T) {
	// Resposta CPR órfã (ESC[<linha>;<col>R) — o caso reportado.
	in := "\x1b[35;36R"
	if got := stripTerminalResponses(in); got != "" {
		t.Fatalf("CPR órfã deveria ser descartada, sobrou: %q", got)
	}
}

func TestStripTerminalResponses_CPRComEnter(t *testing.T) {
	// CPR + Enter (compat com o fluxo real: resposta chega antes do prompt).
	if got := stripTerminalResponses("\x1b[1;1R\r"); got != "\r" {
		t.Fatalf("deveria sobrar só o Enter, sobrou: %q", got)
	}
}

func TestStripTerminalResponses_DSReda(t *testing.T) {
	cases := []string{
		"\x1b[0n",      // DSR "terminal OK"
		"\x1b[?6n",     // DSR privado (consulta CPR)
		"\x1b[?6c",     // DA1 (xterm responde assim ao CSI c)
		"\x1b[>0;276;0c", // DA2
		"\x1b[?1;0$y",  // DECRQM
		"\x1b[Z",       // DECID
	}
	for _, in := range cases {
		if got := stripTerminalResponses(in); got != "" {
			t.Fatalf("resposta de terminal %q deveria ser descartada, sobrou: %q", in, got)
		}
	}
}

func TestStripTerminalResponses_InputNormalPreservado(t *testing.T) {
	// Input normal do usuário NÃO pode ser alterado.
	in := "ls -la\r"
	if got := stripTerminalResponses(in); got != in {
		t.Fatalf("input normal alterado: %q → %q", in, got)
	}
}

func TestStripTerminalResponses_Misturado(t *testing.T) {
	// CPR órfã concatenada com input real: só a resposta some.
	in := "\x1b[1;1Recho ok\r"
	if got := stripTerminalResponses(in); got != "echo ok\r" {
		t.Fatalf("esperado 'echo ok\\r', sobrou: %q", got)
	}
}

func TestStripTerminalResponses_NaoRemoveComandoComR(t *testing.T) {
	// Um "R" solto ou texto com letra R não pode ser removido.
	if got := stripTerminalResponses("git push origin main\r"); got != "git push origin main\r" {
		t.Fatalf("comando com R removido: %q", got)
	}
}
