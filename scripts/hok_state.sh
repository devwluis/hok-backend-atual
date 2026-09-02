#!/bin/sh
# hok_state.sh — gera HOK_STATE.md (contexto compacto para LLMs lerem rápido).
# FIX 02/09: extrai FATOS determinísticos do repo real (rotas, gates, commits,
# pendências). NÃO usa LLM — não alucina contexto. Rodar manualmente ou via
# systemd timer. Saída: /root/hokma/HOK_STATE.md

set -u

BACKEND_DIR="${HOK_BACKEND_DIR:-/root/hokma/backend}"
ROOT_DIR="${HOK_ROOT_DIR:-/root/hokma}"
OUT="${HOK_STATE_OUT:-$ROOT_DIR/HOK_STATE.md}"
NOW="$(date '+%Y-%m-%d %H:%M:%S')"

# ── Infra ────────────────────────────────────────────────────────────────────
backend_port="8082"
nginx_port="3002"
n8n_port="5678"
hokma_active="$(systemctl is-active hokma 2>/dev/null || echo '?')"
opencode_active="$(systemctl is-active opencode-serve 2>/dev/null || echo '?')"
ttyd_active="$(systemctl is-active hok-terminal 2>/dev/null || echo '?')"
nginx_active="$(systemctl is-active nginx 2>/dev/null || echo '?')"

db_path="/root/hokma/backend/memory.db"
if [ ! -f "$db_path" ]; then db_path="(não encontrado)"; fi

# ── Rotas ativas (backend) ───────────────────────────────────────────────────
# Extrai paths de http.HandleFunc em *.go (exclui _test)
routes="$(
  grep -rhoE 'http\.HandleFunc\("[^"]+"' "$BACKEND_DIR"/*.go 2>/dev/null \
    | grep -v '_test' \
    | sed 's/http.HandleFunc("//; s/"$//' \
    | sort -u
)"
route_count="$(printf '%s\n' "$routes" | grep -c . )"

# ── Gates de aprovação (arquivos com pending) ────────────────────────────────
gates="$(
  grep -lE 'setPendingAction|pending_approval|pendingAction' "$BACKEND_DIR"/*.go 2>/dev/null \
    | grep -v '_test' \
    | sed "s|$BACKEND_DIR/||" \
    | sort
)"
gate_count="$(printf '%s\n' "$gates" | grep -c . )"

# ── Últimos commits (backend) ────────────────────────────────────────────────
commits="$(git -C "$BACKEND_DIR" log --oneline -5 2>/dev/null || echo 'sem git')"

# ── Pendências (mantido à mão — o script NÃO inventa) ────────────────────────
PEND="$ROOT_DIR/PENDENCIAS.md"
if [ -f "$PEND" ]; then
  pendencias="$(cat "$PEND")"
else
  pendencias="(sem PENDENCIAS.md — crie em $PEND para o script incluir)"
fi

# ── Padrões que já falharam (mantido à mão) ──────────────────────────────────
PADROES="$ROOT_DIR/PADROES_FALHAS.md"
if [ -f "$PADROES" ]; then
  padroes="$(cat "$PADROES")"
else
  padroes="(sem PADROES_FALHAS.md — crie em $PADROES para o script incluir)"
fi

# ── Monta o arquivo ──────────────────────────────────────────────────────────
{
  echo "# HOK_STATE.md  (gerado em $NOW — NÃO editar à mão)"
  echo
  echo "## Infra"
  echo "- backend: hokma.service :$backend_port ($hokma_active) | opencode-serve :4100 ($opencode_active) | ttyd (hok-terminal) ($ttyd_active)"
  echo "- frontend: nginx :$nginx_port ($nginx_active) em /var/www/hok-os | n8n :$n8n_port"
  echo "- db: $db_path"
  echo
  echo "## Rotas ativas ($route_count)"
  printf '%s\n' "$routes" | sed 's/^/  - /'
  echo
  echo "## Gates de aprovação ($gate_count arquivos)"
  printf '%s\n' "$gates" | sed 's/^/  - /'
  echo
  echo "## Últimos commits"
  printf '%s\n' "$commits" | sed 's/^/  - /'
  echo
  echo "## Pendências"
  echo "$pendencias"
  echo
  echo "## Padrões que já falharam (NÃO repetir)"
  echo "$padroes"
  echo
} > "$OUT"

echo "HOK_STATE.md gerado em $OUT ($(wc -l < "$OUT") linhas)"