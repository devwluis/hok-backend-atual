# ADENDO — Fix Validado: ApprovalResult no resume (erro "tool_calls must be followed by tool messages" DeepSeek)

**Sessão:** 2026-09-10
**Status:** ✅ Fix implementado e validado 3/3 com DeepSeek nativo. **Deploy aguardando aprovação do usuário.**

---

## 1. Resumo Executivo

O bug estrutural identificado no diagnóstico anterior (adendo `ADENDO_Sessao_20260910_Diagnostico_DeepSeek_ToolCalls`) foi **corrigido e validado**. O resume do orquestrador agora injeta a mensagem `tool` respondendo ao `tool_call_id` do último assistant antes de chamar o LLM, eliminando os tool_calls órfãos que a API nativa DeepSeek rejeitava.

**Resultado: 3/3 testes end-to-end com `deepseek-native/deepseek-flash` — zero ocorrências do erro de formato.**

---

## 2. Fix Aplicado

### 2.1 Fix primário — `pending_action.go` (ApprovalResult)

No resume dentro de `resolveRunEnginePendingAction`, o campo `ApprovalResult` agora é populado com o resultado da execução do `run_engine`:

```go
resumedResp := RunOrchestrator(ctx, OrchestratorRequest{
    Task:       progressMsg,
    ConvID:     convID,
    TenantID:   tenantID,
    Mode:       "build",
    MaxSteps:   15,
    Model:      state.UsedModel,
    ResumeFrom: state,
    // FIX DeepSeek: injeta o resultado da aprovacao como resposta de tool
    // (tool_call_id do ultimo assistant). Sem isso, o historico do resume
    // termina com tool_calls orfaos e a API nativa do DeepSeek rejeita com
    // "An assistant message with 'tool_calls' must be followed by tool messages".
    ApprovalResult: "Aprovado e executado pelo usuario. " + reply,
})
```

A injeção que já existia em `agent_orchestrator.go:352-367` passou a executar de fato (antes era código morto, pois a condição `req.ApprovalResult != ""` nunca era satisfeita).

### 2.2 Fix secundário — `db.go` (coluna plan no schema)

A coluna `plan` foi adicionada ao `CREATE TABLE orchestrator_state` e um guard de migração defensivo foi criado para DBs antigos:

```go
`CREATE TABLE IF NOT EXISTS orchestrator_state (
    ...
    expires_at   TEXT NOT NULL,
    plan         TEXT DEFAULT '[]'
);`,
...
// MIGRATION (10/09): coluna plan em orchestrator_state...
if row := sqliteExec(`SELECT sql FROM sqlite_master WHERE type='table' AND name='orchestrator_state';`); row != "" && !strings.Contains(row, "plan") {
    log.Println("MIGRATION: orchestrator_state → adicionando coluna plan")
    sqliteExec(`ALTER TABLE orchestrator_state ADD COLUMN plan TEXT DEFAULT '[]';`)
}
```

Validado com DB **completamente novo** (sem histórico) — coluna presente no schema desde a criação, sem erro "no such column: plan".

## 3. Evidência: Payload Antes vs Depois

### ANTES (bug — rejeitado pela API DeepSeek)
```
[0] role=system
[1] role=system
[2] role=assistant tool_calls=[call_00_0GXGknIFclFeu3SVE7EU4605]
    ^ ultima mensagem do array — tool_call ORFAO, sem resposta
```

### DEPOIS (fix — aceito pela API DeepSeek)
```
[0] role=system
[1] role=system
[2] role=assistant tool_calls=[call_00_lvcllxHIYhSeovrTMt8Q7097]
[3] role=tool      tool_call_id=call_00_lvcllxHIYhSeovrTMt8Q7097   <-- RESPOSTA PRESENTE
```

Payload corrigido preservado em `contexto-hokma/diag_deepseek_toolcalls_20260910/FIX_VALIDADO/`.

## 4. Validação End-to-End (DeepSeek nativo — 3/3)

Script: `/tmp/opencode/deepseek_validate/run_tests.sh` — binário isolado porta 9913, DB em `/tmp`, fluxo completo.

| Teste | orchestrate | approve a.txt (resume) | approve b.txt (end-of-plan) | Injeções | Erros formato | Resultado |
|---|---|---|---|---|---|---|
| 1/3 | ✅ step=1 plan=2 | ✅ HasPA=yes step=2 | ✅ "Plano concluido — 2 de 2" | 1 | 0 | ✅ PASSOU |
| 2/3 | ✅ step=1 plan=2 | ✅ HasPA=yes step=2 | ✅ "Plano concluido — 2 de 2" | 1 | 0 | ✅ PASSOU |
| 3/3 | ✅ step=1 plan=2 | ✅ HasPA=yes step=2 | ✅ "Plano concluido — 2 de 2" | 1 | 0 | ✅ PASSOU |

**Total: 3/3 PASSOU | 0/3 FALHOU | 0 erros de formato DeepSeek.**

Observação: o teste 3 teve a primeira resposta do modelo como "Tarefa de RECONHECIMENTO" (variação do modelo), mas o fluxo completou normalmente — o fix não depende do tipo de tarefa gerada.

## 5. Verificações de Qualidade

- ✅ `go build` limpo
- ✅ `go vet ./...` limpo (sem warnings)
- ✅ `go test ./...` — `ok hokma_backend 28.045s`
- ⚠️ `gofmt -l` aponta `agent_orchestrator.go` — **pré-existente** (mesmo diff de 202 formatação no backup antes das edições, não introduzido nesta sessão). Não reformatado para evitar diff gigante fora do escopo.

## 6. Logs Temporários

Os logs de diagnóstico (`FIX-DEEPSEEK` e `DIAG-PAYLOAD-DIR`) foram **removidos** após a validação. Código de produção limpo, apenas com os fixes.

## 7. Recomendação de Deploy

O fix está pronto para produção. Sequência recomendada (via `deploy.sh`):
1. Build isolado (`hokma_new`)
2. `go test ./...`
3. Backup do binário atual
4. `systemctl stop hokma` → substituir binário → `systemctl start hokma`
5. Healthcheck (`/health`) + verificação de hash

**ATENÇÃO — migração:** o fix do `db.go` adiciona a coluna `plan` automaticamente em DBs existentes (guard de migração). A produção já tem a coluna (ALTER TABLE manual anterior), então o guard detecta e pula o ALTER. Sem risco de quebra.

**Nenhum deploy foi feito. Aguardando aprovação explícita do usuário.**

## 8. Arquivos Modificados (working tree, não commitados)

- `pending_action.go` — campo `ApprovalResult` no resume (5 linhas + comentário)
- `db.go` — coluna `plan` no CREATE TABLE + guard de migração (12 linhas)
- `agent_orchestrator.go` — injeção já existente reativada (sem mudança líquida nesta sessão)

Backups: `*.bak_20260910_183510`
