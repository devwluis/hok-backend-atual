#!/bin/sh
# TESTE C (23/08) — wrapper do ttyd com -a (url-arg):
# o client do ttyd repassa window.location.search ao WebSocket; com -a, cada
# ?arg=<id> chega aqui como $1 e mapeia para UMA sessão tmux por aba.
# Whitelist estrita: "ttyd" (legado) ou dígitos → hok-terminal-<n>.
# Id inválido cai num sleep park (sem shell, sem input).
#
# SCROLL FIX (25/08): `mouse on` POR SESSÃO (não global — sessões hok-<sid> do
# backend Go não são afetadas). O histórico do terminal vive DENTRO do tmux; o
# xterm do ttyd só recebe redraws da tela visível, então wheel/swipe local não
# têm scrollback. Com mouse on, a roda do mouse vira mouse report → o tmux
# entra em copy-mode e rola o histórico real (validado: indicador [n/total]).
case "$1" in
  ttyd) exec tmux new-session -A -s hok-ttyd \; set-option -t hok-ttyd mouse on ;;
  ''|*[!0-9]*) exec sleep 300 ;;
esac
exec tmux new-session -A -s "hok-terminal-$1" \; set-option -t "hok-terminal-$1" mouse on
