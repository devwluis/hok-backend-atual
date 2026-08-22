# Adendo — Gate TUI consciente de tmux (chat→terminal) — 2026-08-22

## Contexto
Com a estabilização via tmux (commit 7d6ee03), o PTY passou a ser ocupado
pelo client tmux — `foregroundPgrp` (ioctl TIOCGPGRP) sempre retornava o
pgrp do client, nunca do processo real dentro do painel. O gate que impedia
digitação automática com TUI em primeiro plano (vim/htop/less…) ficou
inefetivo: comandos da integração chat→terminal eram digitados no painel
ativo mesmo com um editor aberto.

## Correção (commit 76b70cf)
- `terminal_session.go`: novo método `paneForegroundCommand()` consultando
  `tmux display-message -p -t hok-<id> '#{pane_current_command}'` e método
  `terminalGateReason()` centralizando a decisão, com regex `knownShellRe`
  (`bash|sh|zsh|dash|ksh|fish`) e FALLBACK ioctl para modo degradado bash puro.
- `terminal_exec.go`: gate substituído por `s.terminalGateReason()`; recusa
  cita o processo real no reply e no `[AUDIT]` (motivo=%q).
- Comportamento: pane em shell conhecido → permite; qualquer outro processo
  em foreground → recusa com mensagem clara; tmux indisponível → fallback
  ioctl preserva o comportamento pré-tmux.

## Testes
- Unitário `TestTerminalGateKnownShellRegex` (shells × não-shells).
- E2E `TestTerminalGateRecusaTUIDentroDoTmux` (tmux real): sleep em
  foreground → recusa citando "sleep"; Ctrl+C → volta a permitir.
- Suite completa verde (20s). Smoke manual em produção via /chat/smart:
  vim aberto → `terminal_exec_busy` ("executing \"vim\""); vim fechado →
  `terminal_exec` executando o comando (output GATE_OK_123).

## Rollback
    git revert 76b70cf
    cp terminal_session.go.bak_20260822_190153_gatefix terminal_session.go
    cp terminal_exec.go.bak_20260822_190153_gatefix terminal_exec.go
    go build -o hokma . && systemctl restart hokma

## Observação fora de escopo (registrada)
containsTerminalKeyword aceita "roda "/"rodar "/"/terminal" mas NÃO "rode "
(inconsistente com terminalVerbPrefixes do extrator) — mensagens "rode X"
caem no chat padrão. Tratar em sessão própria.
