# Adendo — Sessão 27/08 10:30 · Fase 3 Passo 2 (persistência por conv_id) + commit

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_SESSAO_20260827_fase3_passo1_validado.md, ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md.

---

## Contexto

Passo 2 da Fase 3: persistência da sessão do opencode serve por conv_id,
usando o schema `session_mode` do adendo de 24/08 (tenant_id, user_id,
conv_id, mode, autonomous_budget, set_by, updated_at) — a tabela não existia
nem no código nem no banco (verificado); o adendo original de `session_mode`
não está local nem no Drive (schema fornecido pelo usuário no pedido).

## O que foi feito

### Persistência (opencode_serve_persist.go — arquivo novo)
- Tabela `session_mode` **estendida** com `opencode_session_id TEXT NOT NULL
  DEFAULT ''` — uma linha por conv_id (PK tenant_id, user_id, conv_id).
- `CREATE TABLE IF NOT EXISTS` adicionado ao `initSQLite` (db.go, com backup
  `db.go.bak_20260827_101323_sessionmode`) — padrão canônico do projeto.
- `getOpenCodeServeSessionID(convID, tenantID, userID)` — SELECT por chave.
- `setOpenCodeServeSessionID(...)` — INSERT ... ON CONFLICT DO UPDATE
  (nunca duplica linha por conv).
- `getOrCreateOpenCodeServeSession(...)` — devolve a sessão persistida
  (reused=true) ou cria nova e persiste (reused=false). Se a sessão
  persistida não existir mais no servidor (getSession falha), recria.

### Cliente/handlers (opencode_serve_client.go, backup .bak_20260827_100831)
- `POST /opencode/serve/session` agora aceita `{conv_id, tenant_id, user_id}`
  → get-or-create; retorna `{sessionID, reused, conv_id}`.
- Nova rota `GET /opencode/serve/sessions?conv_id=&tenant_id=&user_id=` →
  linha persistida (mode, set_by, opencode_session_id, updated_at).
- Tudo continua atrás de `OPENCODE_SERVE_TEST=1` + `requireHokAuth` +
  fail closed sem `OPENCODE_SERVE_PASSWORD`.

### Correção de bug no caminho
- Primeira versão criava a tabela num `init()` Go — **panic** no startup
  (db global ainda nil: initDB só roda no main). Corrigido movendo o
  CREATE TABLE para o initSQLite e removendo o init().

## Validação (isolado: porta 8090, DB de teste /tmp, OPENCODE_SERVE_TEST=1)

| Cenário | Resultado |
|---|---|
| convA nova | `reused:false` → `ses_fbd48ee05ffejcSXPPsR7A6VlW` |
| reabrir convA | `reused:true`, mesma sessionID (sem duplicar) |
| convB nova | `reused:false` → `ses_fbd48d974ffeql778UXlvATyE5` |
| reabrir convB | `reused:true`, mesma sessionID |
| GET /opencode/serve/sessions (convA) | linha persistida: mode=plan, set_by=opencode_serve, sid correto |
| SELECT direto no SQLite | 2 linhas (convA, convB), PK único por conv |
| sessão órfã (sid falso no banco) | detectada inexistente no servidor → recriada (reused:false) |
| go vet / testes opencode | limpos / ok |
| produção | hokma.service ativo na 8082, binário intocado |

## Commit
- Branch local `main` (remote origin = hok-backend-atual, sem push — aguardando
  aprovação explícita).
- Arquivos do commit (somente Fase 3): `opencode_serve_client.go`,
  `opencode_serve_persist.go`, `db.go`, `.gitignore` (hokma_fase3_test) +
  adendos de contexto. Trabalho pendente de outras sessões no working tree
  NÃO entrou no commit.

## Pendências / próximos passos
- Etapa 3 (aguardando aprovação explícita): substituição do `tryTerminalExec`
  no fluxo real do chat — plano a revisar antes de começar (toca produção).
- Manter leitura do resumo via `GET /session/{id}/message` filtrando
  `mode=compaction` (NÃO usar session.summary — reforço do usuário).
- Atualizar `drive_creds.env` com refresh token válido (pendência do Passo 1).