#!/bin/bash
# ─────────────────────────────────────────────────────────────────────────────
# Recovery do HOK OS — restaura um checkpoint do modo autônomo total.
# Uso: /root/hokma/recovery.sh <checkpoint_id>
# Standalone: depende apenas de git/sqlite3/docker/systemctl — funciona mesmo
# com o hokma caído (camada de segurança FORA do agente).
# ─────────────────────────────────────────────────────────────────────────────
set -u
ID="$1"
ROOT="/root/hokma"
SNAP="$ROOT/snapshots/$ID"
LOG="$ROOT/recovery.log"
BACKEND="$ROOT/backend"

log() { echo "$(date '+%F %T') $*" >> "$LOG"; }

if [ ! -d "$SNAP" ]; then
  echo "checkpoint $ID nao existe" | tee -a "$LOG"
  exit 1
fi

log "=== recovery $ID iniciado ==="

# 1. hermes-gateway parado (o volume /opt/data está montado nele)
docker stop hermes-gateway >>"$LOG" 2>&1 || log "aviso: docker stop hermes-gateway falhou"

# 2. hokma parado (o memory.db está aberto)
systemctl stop hokma >>"$LOG" 2>&1 || log "aviso: systemctl stop hokma falhou"

# 3. código: reset pra tag do checkpoint (o snapshot commita + tageia)
if ! git -C "$BACKEND" reset --hard "snapshot/$ID" >>"$LOG" 2>&1; then
  log "ERRO: git reset --hard snapshot/$ID falhou — abortando (serviços voltando)"
  docker start hermes-gateway >>"$LOG" 2>&1 || true
  systemctl start hokma >>"$LOG" 2>&1 || true
  exit 1
fi
git -C "$BACKEND" clean -fd >>"$LOG" 2>&1 || log "aviso: git clean falhou"

# 4. banco (restore do snapshot; remove WAL/shm antes)
if [ -f "$SNAP/memory.db" ]; then
  rm -f "$BACKEND/memory.db-wal" "$BACKEND/memory.db-shm"
  if sqlite3 "$BACKEND/memory.db" ".restore '$SNAP/memory.db'" >>"$LOG" 2>&1; then
    log "banco restaurado"
  else
    log "ERRO: restore do banco falhou"
  fi
fi

# 5. volume do hermes (/opt/data) — decisão 4: incluído no escopo
if [ -f "$SNAP/hermes_optdata.tgz" ]; then
  VOL=$(docker inspect hermes-gateway --format '{{range .Mounts}}{{if eq .Destination "/opt/data"}}{{.Name}}{{end}}{{end}}' 2>/dev/null)
  if [ -n "$VOL" ] && [ -d "/var/lib/docker/volumes/$VOL/_data" ]; then
    rm -rf "/var/lib/docker/volumes/$VOL/_data"/*
    tar xzf "$SNAP/hermes_optdata.tgz" -C "/var/lib/docker/volumes/$VOL/_data" || log "ERRO: restore do volume falhou"
    log "volume hermes restaurado ($VOL)"
  else
    log "aviso: volume do hermes nao encontrado ($VOL)"
  fi
fi

# 6. /root/.hermes (bind do hermes-gateway)
if [ -f "$SNAP/hermes_root_home.tgz" ]; then
  rm -rf /root/.hermes
  mkdir -p /root/.hermes
  tar xzf "$SNAP/hermes_root_home.tgz" -C /root || log "ERRO: restore /root/.hermes falhou"
  log "/root/.hermes restaurado"
fi

# 7. .env (cópia simples — nunca vai pro git)
if [ -f "$SNAP/.env" ]; then
  cp "$SNAP/.env" "$BACKEND/.env"
  chmod 600 "$BACKEND/.env"
  log ".env restaurado"
fi

# 8. subir tudo
docker start hermes-gateway >>"$LOG" 2>&1 || log "aviso: docker start hermes-gateway falhou"
systemctl start hokma >>"$LOG" 2>&1 || log "aviso: systemctl start hokma falhou"

log "=== recovery $ID concluido ==="