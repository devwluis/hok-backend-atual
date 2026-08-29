# Adendo — Sessão 29/08 · Monitor do celular (Redmi Note 10) via túnel Termux + n8n

**Origem:** opencode (terminal) **Data/hora:** 29-08-2026

---

## Contexto

Redmi Note 10 do usuário (Termux + sshd) estava **resetando sozinho** — causas:
RAM estourada (3.9/5.5 GB) + espaço 94% cheio + load ~21. Acesso via
**túnel reverso**: `ssh -N -f -R 8022:localhost:8022 root@179.197.64.133`
(do Termux) — o celular fica acessível em localhost:8022 do servidor.

## Limpeza no celular (via túnel, autorizada)

| Ação | Liberado |
|---|---|
| Vídeos (gravação de tela de 2.99 GB + 17 vídeos — DCIM/Movies/Download) | ~2.9 GB |
| Instaladores antigos do /sdcard/Download (cursor, opencode-desktop, didi, Antigravity) | ~1.6 GB |
| Downloads do Termux (.deb/.apk/zips) | ~0.6 GB |
| Cache + zip duplicado | ~0.05 GB |
| Backups do HokOS_Backups de junho (mantido o de 12/06 — opção 2) | ~5.6 GB |
| **Total** | **~10.7 GB (94% → 84%, 17 GB livres)** |

## Endpoint novo no HOK (backend)

- `celular_status.go`: `GET /celular/status` (X-Hok-Token) — conecta em
  localhost:8022 (túnel), parseia `free -h` / `df -h /storage/emulated` /
  `uptime` → JSON: RAM (total/usado/percent/disponível), espaço
  (total/usado/livre/percent), load 1/5/15. Se o túnel fechar →
  `{"status":"tunel_fechado"}` (sem erro 500 — o workflow alerta).
  Nota: `/proc/loadavg` é restrito no Android → load via `uptime`.
- Rota em main.go + nginx (regex do proxy ganhou `celular`).
- Deploy em produção (backup `hokma.bak_pre_celular_*` → stop/cp/start →
  active + ping ok). Endpoint validado com o celular REAL (RAM 65%,
  espaço 84%, load 22).

## Workflow no n8n (importar)

- Arquivo: `contexto-hokma/workflow_celular_monitor.json` (JSON de import do
  n8n): Schedule `*/10 * * * *` → HTTP GET `172.17.0.1:8082/celular/status`
  (header X-Hok-Token) → If (espaço>90% OU RAM>85% OU load>15 OU
  tunel_fechado) → **Telegram** (mesmo chat do Alerta RAM) + Log.
- Importar: n8n (imoveischaves.com) → editor → Import from file → ativar.
- Aviso: o token do HOK está embutido no JSON (trocar se quiser).

## Pendências

- Commit/push do backend (celular_status.go + main.go) aguardando aprovação.
- Túnel: quando o celular reiniciar, reabrir Termux + `sshd` + o túnel
  (`ssh -N -f -R 8022:localhost:8022 root@179.197.64.133`).