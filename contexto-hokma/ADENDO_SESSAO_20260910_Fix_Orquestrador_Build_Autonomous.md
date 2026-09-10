# ADENDO — Sessão 2026-09-10

## Resumo
Fix dos modos do orquestrador: build mode (pending_action surfacing) e autonomous_total (budget gate com userID correto).

---

## 1. Build Mode — Pending Action não surfacava ao usuário

### Problema
Quando o orquestrador delegava para um subagente em modo `build`, o subagente criava um `pending_action` e retornava "Confirma? (responda sim/nao)". Porém, o orquestrador tratava essa resposta como um resultado normal de tool call, adicionava ao array `messages` e continuava o loop — em vez de retornar imediatamente ao usuário para aprovação.

**Efeito colateral**: o modelo respondia "sim" para si mesmo, e o orquestrador retornava "sim" como reply final, sem nunca surfacar o pending_action.

### Causa Raiz
Na seção de delegação do `RunOrchestrator` (linhas 496-510), o output do `runSubagent` era sempre tratado como `SubagentResult.Output` e adicionado como tool result, sem detecção de pending_action.

### Fix
`agent_orchestrator.go` — Adicionada detecção após `runSubagent` retorna:

```go
// FIX: Se o subagente retornou um pending_action (build mode),
// retorna imediatamente ao usuário para aprovação em vez de
// continuar o loop do orquestrador.
if strings.Contains(out, "Confirma?") && req.Mode == "build" {
    resp.Reply = out
    resp.Steps = step
    resp.ModelUsed = usedModel
    resp.Tracing = append(resp.Tracing, AgentTraceEntry{
        Step: step, Kind: "pending_action", Agent: target.Name, Input: subTask,
        Output: truncateStr(out, 600), Ts: time.Now().Format(time.RFC3339),
    })
    saveAgentRun(req, resp)
    return resp
}
```

Aplicado também para o path `req.AgentID != ""` (agente específico).

### Teste
```bash
curl -s -X POST http://localhost:8082/agents/orchestrate \
  -H "X-Hok-Token: $TOKEN" -H "X-Conversation-Id: test_build_fix_002" \
  -d '{"task":"Crie um arquivo /tmp/teste_build_mode2.txt com conteudo Teste Build Fix","mode":"build","max_steps":5,"tenant_id":"owner"}'
```

**Resultado**: 
- `reply`: "Executar tarefa no engine opencode: ... Confirma? (responda sim/nao)"
- `tracing[0].kind`: "pending_action"
- `steps`: 1 (loop interrompido)

Aprovação via `/actions/approve` com `X-Conversation-Id` também funciona.

---

## 2. Autonomous_total — Budget gate com userID incorreto

### Problema
O orquestrador chamava `autonomousBudgetLeft(req.ConvID, req.TenantID, "")` com `userID=""`. O `session_mode` era salvo com `user_id="anonymous"` (via `userIdFromRequest` quando autenticado via `X-Hok-Token`). A query SQL não encontrava correspondência → retornava 0 → bloqueava com "Budget esgotado" mesmo com budget disponível.

### Fix
`agent_orchestrator.go` — Todas as chamadas de `autonomousBudgetLeft` e `autonomousAllow` no orquestrador agora usam `userID="anonymous"`:

```go
// Antes:
left := autonomousBudgetLeft(req.ConvID, req.TenantID, "")
allowed, reason, _ := autonomousAllow(req.ConvID, req.TenantID, "", ...)

// Depois:
left := autonomousBudgetLeft(req.ConvID, req.TenantID, "anonymous")
allowed, reason, _ := autonomousAllow(req.ConvID, req.TenantID, "anonymous", ...)
```

### Teste
```bash
# Configurar budget
curl -X POST http://localhost:8082/session/mode \
  -H "X-Hok-Token: $TOKEN" -H "X-Conversation-Id: test_autonomous_001" \
  -d '{"mode":"autonomous_total","autonomous_budget":5}'

# Rodar orquestrador
curl -X POST http://localhost:8082/agents/orchestrate \
  -H "X-Hok-Token: $TOKEN" -H "X-Conversation-Id: test_autonomous_001" \
  -d '{"task":"Liste os arquivos no diretorio /tmp","mode":"autonomous_total","max_steps":5,"tenant_id":"owner"}'
```

**Resultado**: Budget 5→3 (2 ações consumidas), sem bloqueio indevido.

---

## Commits
- `7f2d6a0` — fix(orchestrator): detect pending_action from subagent + fix autonomous userID
- Pushed to `origin/main`

## Deploy
- Hash: `f6376986a4253d9b231ec7cb5bda6874`
- Backend: `hokma.service` ativo em `:8082`
- Backup: `hokma.bak_20260910_110748`

## Status
- ✅ Build mode: pending_action surfaca ao usuário corretamente
- ✅ Autonomous_total: budget gate funciona com userID correto
- ✅ Plan mode: continuava bloqueando corretamente (não alterado)
- ⚠️ Native model guard: `deepseek-native/*` ainda impede orquestrador de rodar com modelo nativo (comportamento pré-existente, non-blocking)
