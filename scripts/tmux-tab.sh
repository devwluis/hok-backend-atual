#!/bin/sh
# INSTRUMENTAÇÃO — log apenas, não altera fix (tarefa inv)
logger -t tmux-inv "run sess=$sess args=$*" 2>/dev/null || echo "$(date +%H:%M:%S) sess=$sess args=$*" >> /tmp/opencode/instrument.log
# FIX clone-bug (16/09): argumento "new=1" ou "_t=<ts>" força kill + criação limpa.
# Quando o frontend abre nova aba, passa &_t=<timestamp> — o script mata a
# sessão existente e cria uma nova, evitando o clone da sessão principal.
FORCE_NEW=0
for arg in "$@"; do
  case "$arg" in
    new=1) FORCE_NEW=1 ;;
    _t=*) FORCE_NEW=1 ;;
  esac
done
# PERSISTÊNCIA + RESPAWN (27/08): corrige o bug de "trava tudo" no mobile.
#
# Causa raiz confirmada por evidência (log instrumentado + ttyd):
#   O processo fill (tmux attach) morre com exit code 0 não porque o ttyd o mata,
#   mas porque o bash dentro do pane tmux recebe EOF quando o PTY master é fechado
#   (WebSocket do ttyd caiu por instabilidade de rede mobile) e o bash sai. Sem
#   o bash, a sessão tmux morre, e o tmux attach sai com o mesmo exit 0.
#   O opencode NÃO crasha — é o bash que sai.
#
# Fix: (1) remain-on-exit on — o pane sobrevive mesmo com o shell morto; (2) no
# reattach, se o pane está morto, respawna o shell automaticamente para que o
# usuário nunca veja um prompt "dead" — o terminal reabre sem toque manual.
# (3) destroy-unattached off (padrão) mantém a sessão viva entre quedas.
case "$1" in
  ttyd|'') sess="hok-ttyd" ;;
  *[!0-9]*) exec sleep 300 ;;
  *) sess="hok-terminal-$1" ;;
esac
# FIX clone-bug: se FORCE_NEW, mata sessão existente antes de criar
if [ "$FORCE_NEW" = "1" ]; then
  tmux kill-session -t "$sess" 2>/dev/null
  # Espera um instante para o tmux liberar o recurso
  sleep 0.3
fi
if tmux has-session -t "$sess" 2>/dev/null; then
  tmux set-option -t "$sess" mouse on \; set-option -t "$sess" status off
  # Se o pane está morto (shell morreu na queda), respawna silenciosamente
  if tmux list-panes -t "$sess" -F '#{pane_dead}' 2>/dev/null | grep -q '^1$'; then
    tmux respawn-pane -t "$sess" \; set-option -t "$sess" remain-on-exit on
  fi
else
  tmux new-session -d -s "$sess" \; set-option -t "$sess" mouse on \; set-option -t "$sess" status off \; set-option -t "$sess" remain-on-exit on
fi
# FIX log-rotativo-tui (30/08): spawn do helper de captura rotativa em
# background. Resolve o problema da TUI (opencode/claude) em alternate
# screen — tmux history_size=0 e a scrollbar do scrollback não funciona.
# O helper escreve /var/log/hok-term/<sess>.log a cada 2s com capture-pane,
# rotacionando em 5MB. Idempotente: mata PID antigo antes de criar novo.
if [ -x /root/hokma/tmux-capture.sh ]; then
  nohup /root/hokma/tmux-capture.sh "$sess" >/dev/null 2>&1 &
fi
exec tmux attach -t "$sess"