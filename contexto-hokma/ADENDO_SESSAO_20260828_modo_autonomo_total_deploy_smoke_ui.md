# Adendo — Sessão 28/08 · Modo Autônomo Total: deploy + smoke + botões UI + descobertas

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_20260828_modo_autonomo_total_implementacao.md
(implementação), commits 6c97a6f (backend) e 68f8645 (frontend).

---

## Deploy (backend, aprovado)

- Push: `1f3942e..6c97a6f main -> main`
- Backup (`hokma.bak_pre_total_*`, `memory.db.bak_*`) → build → stop/cp/start
  → hokma `active` + `/ping: {"status":"ok"}`
- Nota: `recovery.sh` movido para `backend/` (versionado) — a const no Go
  apontava para `/root/hokma/recovery.sh`; corrigida.

## Smoke real (rollback completo)

- Snapshot `auto_20260828_191451` na ativação (tag git + memory.db válido +
  volume hermes 55MB + /root/.hermes + .env + META)
- Rollback via POST /recovery/rollback: banco, volume (`c9b33b8c…`),
  /root/.hermes e .env restaurados; hokma + hermes-gateway voltaram (ping OK)
- Limpeza: tag/commit do checkpoint removidos, snapshots e banco limpos

## Botões na UI (frontend)

- `ModeSelector.tsx`: 4 pills (Planejar/Construir/Autônomo/Autônomo Total)
  + badge checkpoint + toggle auto-rollback + botão Rollback (confirmação)
- **Input de budget inline** (visível só em Autônomo/Total; min 1/max 200;
  aplica ao trocar de modo/OK/Enter; badge mostra o atual)
- Endpoints: GET/POST `/session/mode`, POST `/recovery/rollback`
- Commits: `514ec76` (seletor) + `68f8645` (budget) — push hok-frontend-atual
- Build: `PORT=3010 BASE_PATH=/ npm run build` ✓ — preview 3055 servindo o
  bundle novo

## Teste visual completo do fluxo (via interface = endpoints)

| Passo | Resultado |
|---|---|
| Ativar Autônomo Total (budget 7) | mode autonomous_total, budget 7, checkpoint `auto_20260828_193614` criado ✓ |
| Tarefa hermes autônoma (listar /opt/data) | `hermes_autonomous_total` — **resposta real** (lista de .env, SOUL.md...) ✓ |
| Budget | 7 → 6 → 5 → 4 (1 por chamada, inclui falhas) ✓ |
| Circuit breaker | 3ª ação idêntica → `blocked_cb` ✓ |
| Rollback (botão) | banco + volume + .env restaurados; hokma + hermes-gateway de volta ✓ |
| Auditoria | ok/ok/blocked_cb/ok com budget_left por linha ✓ |

## Descobertas (correções fora do escopo original)

1. **Credencial do hermes marcada exhausted**: o `auth.json` (volume) tinha
   `last_status: exhausted, last_error_code: 429` (às ~19:02, ANTES do
   snapshot) — o hermes rejeita o provider ("No LLM provider configured").
   **Reset realizado** (backup do auth.json + stop/start do hermes-gateway)
   — o hermes voltou a responder. Não foi causado pelo deploy/rollback.
2. **Modelo ativo inválido para o hermes**: `mimo-v2.5-free` (sem prefixo)
   é rejeitado pelo openrouter ("not a valid model ID"). Modelo trocado
   (temporariamente) para `deepseek/deepseek-v4-flash-0731` (o default do
   config do hermes e o que Washington usa) para validar o fluxo. Se quiser
   voltar ao mimo, o ID correto no openrouter precisa de prefixo
   (ex.: mimo/…).

## Pendências

- Commit/push do backend `6c97a6f` já feito; frontend `68f8645` feito.
- Frontend: budget agora customizável na UI ✓ (rodada pedida concluída).