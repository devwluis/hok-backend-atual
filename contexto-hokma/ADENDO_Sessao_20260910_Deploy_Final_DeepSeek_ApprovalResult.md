# ADENDO — Deploy em Produção: Fix ApprovalResult (DeepSeek tool_calls órfãos) + Schema plan

**Sessão:** 2026-09-10
**Status:** ✅ Deploy concluído, smoke test em produção aprovado, commit + push realizados.

---

## 1. Resumo Executivo

O fix do `ApprovalResult` no resume (elimina tool_calls órfãos que a API nativa DeepSeek rejeitava) e o fix de schema (coluna `plan` no `CREATE TABLE` de `orchestrator_state`) foram **implantados em produção** com sucesso.

Smoke test real em produção (`deepseek-native/deepseek-flash`, build mode, 2 arquivos): **PASSOU** — zero ocorrências do erro "tool_calls must be followed by tool messages", end-of-plan correto.

## 2. Processo de Deploy

Executado via `./deploy.sh` (processo padrão):

| Etapa | Resultado |
|---|---|
| Build isolado (`hokma_new`) | ✅ hash `8a29ed9a3e154a3cf03bed54900b335d` |
| `go test ./...` | ✅ `ok hokma_backend 23.361s` |
| Backup do binário | ✅ `hokma.bak_20260910_185125` |
| Substituição + restart | ✅ serviço active |
| Health check (com retry) | ✅ HTTP 200 |
| Verificação de hash build vs rodando | ✅ idênticos |

## 3. Hashes do Binário

| Momento | MD5 |
|---|---|
| **Antes** | `3df8e8da5a067bb10b67f1f301a9d584` |
| **Depois** | `8a29ed9a3e154a3cf03bed54900b335d` |

Hashes diferentes → troca de binário comprovada.

## 4. Pré-deploy: Isolamento Confirmado

- Nenhum binário de teste rodando (porta 9913 parada)
- Zero linhas de teste no DB de produção: `orchestrator_state` e `pending_actions` sem conv `val*`/`diag_*`
- Validação usou porta 9913 + DB em `/tmp` — sem vazamento

## 5. Smoke Test em Produção (porta 8082, serviço real)

Fluxo: `orchestrate` → `approve a.txt` (resume) → `approve b.txt` (end-of-plan)
Modelo: `deepseek-native/deepseek-flash`, build mode.

| Passo | Resultado |
|---|---|
| orchestrate | ✅ step=1, plan=2 |
| approve a.txt (resume) | ✅ HasPA=True, step=2 — DeepSeek respondeu `finish=tool_calls` |
| approve b.txt (end-of-plan) | ✅ "Plano concluido — 2 de 2 arquivos processados." |
| State final | ✅ limpo (0) |
| Erros "tool_calls must be followed by tool messages" | ✅ **0** (desde o deploy) |
| Arquivos mutados | ✅ `a.txt` com AAA, `b.txt` com BBB |

Log do resume confirma a injeção: `(state restaurado, 4 messages)` — system + system + assistant(tool_calls) + tool(response).

## 6. Migração de Schema em Produção

- Coluna `plan` no DB de produção: ✅ presente (`PRAGMA table_info` → `11|plan|TEXT|0|'[]'|0`)
- O guard de migração detectou que a coluna já existia (via ALTER TABLE manual anterior) e **pulou o ALTER** — nenhum erro
- DB fresco (testado em validação): coluna criada direto no `CREATE TABLE` — sem "no such column: plan"

## 7. Commit + Push

- **Commit:** `8c297c1` — "fix(orchestrator): ApprovalResult no resume elimina tool_calls orfaos (DeepSeek native) + schema plan no CREATE TABLE"
- **Push:** `7f2d6a0..8c297c1 main -> main` (fast-forward, 5 commits)
- Repo: `https://github.com/devwluis/hok-backend-atual.git`
- **Observação:** o push foi para a branch `main` (branch de tracking do checkout de produção — mesmo fluxo dos commits anteriores). A branch remota `hok-backend-atual` existe em paralelo e está divergida (27 commits de fixes de terminal de 03/09 fora da main); **não foi tocada** (um push lá seria non-fast-forward e exigiria force — decisão pendente do usuário).

## 8. Arquivos Alterados neste Fix

- `pending_action.go` — `ApprovalResult` populado no resume
- `db.go` — coluna `plan` no CREATE TABLE + migration guard
- `agent_orchestrator.go` — injeção reativada (código já existente)
- `contexto-hokma/` — 4 adendos + evidências do diagnóstico

## 9. Conclusão

| Item | Status |
|---|---|
| Fix ApprovalResult | ✅ Em produção e validado |
| Fix schema plan | ✅ Em produção e validado |
| Smoke test produção (DeepSeek nativo) | ✅ Passou |
| Erros de formato DeepSeek | ✅ Zero |
| Commit + push | ✅ `8c297c1` |
| Rollback necessário | ❌ Não |
