# Adendo — Sessão 28/08 · Implementação do MODO AUTÔNOMO TOTAL (snapshot + rollback)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_PROPOSTA_SNAPSHOT_ROLLBACK_MODO_AUTONOMO_TOTAL_20260828.md
(proposta aprovada), commit 36c2d30 (modo autônomo, commitado separado).

---

## Decisões fechadas por Washington (implementadas)

1. `mode:"autonomous_total"` — flag explícita no POST /session/mode (nunca
   default); snapshot automático na ATIVAÇÃO.
2. Budget alto **50** (body pode especificar) — rede de segurança mesmo se o
   CB falhar.
3. Rollback **manual por padrão** (CB para + avisa + comando pronto), flag
   `auto_rollback` opcional; **recovery.sh standalone** como camada fora do
   agente.
4. Snapshot **inclui o volume do hermes** (/opt/data + /root/.hermes); n8n
   fora do escopo.
5. Branch `task/<nome>` para código de tarefas grandes — orientação de uso
   (o snapshot é agnóstico de branch).

## Implementação

- **recovery.go** (novo): `gitSnapshot` (commit checkpoint + tag
  `snapshot/<id>`, tolera "nothing to commit"); `snapshotCreate` (código +
  `sqlite3 .backup` do memory.db + tar do volume do hermes `c9b33b8c…` +
  tar `/root/.hermes` + cópia do `.env` + META.json); `triggerRecovery`
  (systemd-run do script — roda FORA do cgroup, funciona com o hokma caído;
  `HOK_RECOVERY_DRY_RUN=1` loga sem executar); endpoint
  `POST /recovery/rollback {checkpoint_id}` (valida pattern
  `^[A-Za-z0-9_.-]+$` + existência; fallback: checkpoint da conversa).
- **recovery.sh** (standalone): para hermes-gateway + hokma → `git reset
  --hard <tag>` + `clean -fd` → restore do banco (remove WAL/shm) → restore
  do volume (rm + tar) → `/root/.hermes` → `.env` → sobe os serviços. Log em
  `/root/hokma/recovery.log`.
- **session_mode**: migration aplicada (CHECK com `autonomous_total` +
  colunas `checkpoint_id`/`auto_rollback`; linhas preservadas) no banco real
  e no CREATE TABLE do db.go (migration condicional no initSQLite — aplicada
  manualmente para não travar no lock com a produção rodando).
- **session_mode.go**: aceita autonomous_total; `normalizeAutonomousBudget`
  → 50; body `auto_rollback`; snapshot na ativação (1x por conv).
- **autonomous.go**: `isAutonomousLike`; `sessionModeSetCheckpoint`;
  `autonomousTotalEnsureSnapshot` (fallback quando o request traz o mode
  direto); `autonomousTotalMaybeRollback` (auto_rollback dispara o
  recovery.sh quando o CB/budget bloqueia).
- **smart_chat.go**: tryX tratam total (`*_autonomous_total` no mode do
  reply); **"volte pro checkpoint"** no chat → rollback da conversa
  (`recovery_started`/`recovery_none`/`recovery_error`).
- **main.go**: rota `/recovery/rollback`.

## Validação

- **Go**: build + vet limpos; suíte completa PASS (10.7s) — testes novos:
  budget do total (50), isAutonomousLike, pattern do checkpoint (rejeita
  traversal), TestSnapshotGit (repo temp: snapshot → mudança → rollback
  restaura o estado do checkpoint).
- **E2E isolado (8099, HOK_RECOVERY_DRY_RUN=1)**:
  - POST autonomous_total → budget 50, snapshot criado na ativação:
    tag `snapshot/auto_<ts>`, memory.db válido (19 linhas), volume do hermes
    (55 MB), /root/.hermes, .env, META.json ✓
  - GET → checkpoint_id gravado na session_mode ✓
  - Hermes TOTAL executando → `hermes_autonomous_total`, budget 50→49
    (o hermes respondeu erro de modelo do provider no isolado — não afeta o
    mecanismo; confirmado no smoke real após deploy)
  - Blocklist no total → `hermes_autonomous_total_blocked` (aviso direto) ✓
  - "volte pro checkpoint" no chat → `recovery_started` (DRY-RUN logado) ✓
  - POST /recovery/rollback → `recovery_started`; **traversal rejeitado**
    (`../etc/passwd` → checkpoint inválido) ✓
- **Limpeza pós-E2E**: commit/tag do checkpoint removidos (reset --mixed),
  diretório de snapshot removido, banco de produção limpo (conv de teste).
- **Commit 36c2d30** (modo autônomo) feito SEPARADO antes desta rodada.

## Pendências

- **NÃO deployado/restart/push** — aguardando aprovação de Washington.
- Após deploy: smoke real (autonomous_total com snapshot + rollback real via
  recovery.sh — inclui restart do hokma/hermes-gateway, brevemente).
- Push dos commits (36c2d30 + total) aguardando aprovação.
- Frontend: 3 botões + modo total (próxima etapa).