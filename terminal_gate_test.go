package main

import (
	"strings"
	"testing"
	"time"
)

// Unitário: apenas shells conhecidos liberam a digitação automática.
func TestTerminalGateKnownShellRegex(t *testing.T) {
	for _, ok := range []string{"bash", "sh", "zsh", "dash", "ksh", "fish"} {
		if !knownShellRe.MatchString(ok) {
			t.Fatalf("%q deveria ser reconhecido como shell", ok)
		}
	}
	for _, bad := range []string{"vim", "htop", "less", "sleep", "opencode", "bashrc", "sudobash", ""} {
		if knownShellRe.MatchString(bad) {
			t.Fatalf("%q NÃO deveria passar como shell", bad)
		}
	}
}

// E2E com tmux real: processo em foreground no pane (sleep) → gate RECUSA;
// Ctrl+C devolve o bash ao primeiro plano → gate volta a PERMITIR.
// Regressão do commit 7d6ee03: sob tmux, foregroundPgrp só vê o client tmux
// e o gate antigo ficava inefetivo.
func TestTerminalGateRecusaTUIDentroDoTmux(t *testing.T) {
	ts, cleanup := dupServer(t)
	defer cleanup()
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	cA := dupDial(t, url, "")
	cA.waitReady(t)
	s := findActiveSession(terminalUserKey(dupUser))
	if s == nil {
		t.Fatal("sessão não encontrada")
	}
	waitOutput(t, cA.total, "\x1b[?2004h", 1)
	time.Sleep(300 * time.Millisecond)

	// 1) Shell pronto → gate permite.
	if r := s.terminalGateReason(); r != "" {
		t.Fatalf("gate deveria PERMITIR com bash no painel; motivo=%q", r)
	}

	// 2) Processo em foreground no pane (sleep) → gate recusa citando o processo.
	s.writeInput("sleep 25\n")
	var reason string
	limit := time.Now().Add(3 * time.Second)
	for time.Now().Before(limit) {
		if reason = s.terminalGateReason(); reason != "" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	if reason == "" {
		t.Fatal("gate DEVERIA recusar com processo em foreground dentro do tmux")
	}
	if !strings.Contains(reason, "sleep") {
		t.Fatalf("motivo deveria citar o processo real (%q): %q", "sleep", reason)
	}

	// 3) Ctrl+C encerra o foreground → gate permite novamente.
	s.writeInput("\x03")
	limit = time.Now().Add(5 * time.Second)
	for time.Now().Before(limit) {
		if s.terminalGateReason() == "" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("gate não voltou a permitir após Ctrl+C; motivo=%q", s.terminalGateReason())
}
