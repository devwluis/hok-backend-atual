package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

// SetupSignalHandler — registra handler que mata o process group do opencode
// quando o oca recebe SIGINT/SIGTERM. Grava state antes de sair para que o
// resume saiba exatamente onde parou (item 6: persistência).
func SetupSignalHandler(state *SessionState) chan os.Signal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		sig := <-sigCh
		log("sinal recebido: %s — encerrando opencode", sig)
		// FIX item-6: grava state ANTES de matar (heartbeat final).
		state.LastHeartbeat = time.Now().UTC().Format(time.RFC3339)
		state.UpdatedAt = state.LastHeartbeat
		if state.BlockedReason == "" {
			state.BlockedReason = "signal: " + sig.String()
		}
		_ = writeState(state)
		_ = KillOpenCode(state)
		os.Exit(130)
	}()
	return sigCh
}