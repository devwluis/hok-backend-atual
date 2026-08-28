# Adendo — Sessão 28/08 · Deploy do MODO AUTÔNOMO + smoke test REAL em produção

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_PROPOSTA_MODO_AUTONOMO_20260828.md (proposta),
ADENDO_SESSAO_20260828_modo_autonomo_implementacao.md (implementação + E2E 8099).

---

## Deploy (padrão das sessões)

- Backups: `hokma.bak_pre_autonomous_<ts>`, `memory.db.bak_pre_autonomous_<ts>`
- `systemctl stop hokma` → `cp hokma_test hokma` → `start` → ativo, 8082 OK
- Nada mais tocado; opencode-serve intacto

## Smoke test REAL em produção (porta 8082)

Conversa de teste `smoke_auto_prod` (mode:autonomous, budget:2 via
POST /session/mode — GET confirmou o upsert):

| # | Verificação | Resultado |
|---|---|---|
| 1 | POST /session/mode {autonomous, budget 2} | ok; GET lê o estado ✓ |
| 2 | Hermes autônomo: `rm -rf /` | `hermes_autonomous_blocked` — aviso direto, **sem execução, sem pendência** ✓ |
| 3 | Hermes autônomo: listar /opt/data | `hermes_autonomous` — **executou de verdade**: respondeu com conteúdo real (SOUL.md, auth.json, config.yaml, leads-monitor-workflow.json...) ✓ |
| 4 | Mesma ação (1ª repetição) | executou de novo; budget 1→0 ✓ |
| 5 | Mesma ação (2ª repetição) | `hermes_autonomous_blocked` — **circuit breaker: "mesma ação repetida 3x"** ✓ |
| 6 | Ação nova (budget 0) | `hermes_autonomous_blocked` — "budget esgotado (0/5) — recarregue via POST /session/mode" ✓ |

Auditoria real (autonomous_audit, ordem): blocked (2) → ok (1) → ok (0) →
blocked_cb (0) — cada tentativa com agente, ação, hash e budget_left ✓.
Budget final na tabela: 0. Serviços ativos, 0 panics.

Obs.: mensagens com keywords do n8n ("ambiente", "workflow", etc.) são
capturadas pelo agente n8n ANTES do hermes na cascata — o smoke usou
formulação sem keywords para atingir o hermes.

## Limpeza

Linhas de teste removidas do banco de produção (session_mode e
autonomous_audit da conv smoke_auto_prod) — mesma prática das sessões
anteriores.

## Próxima etapa (anotado)

**Frontend**: os 3 botões do ChatScreen (Planejar / Construir / Autônomo)
chamando `POST /session/mode` {mode, autonomous_budget?} — o backend já
lê o session_mode como fonte do modo quando o request não traz mode.

## Pendências

- Commit/push da rodada completa (modo autônomo) aguardando aprovação —
  inclui: autonomous.go, session_mode.go, gates nos tryX, args dos 3
  clients, watcher do serve, autonomous_audit, 8 testes, adendos.