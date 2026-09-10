# ADENDO — Diagnóstico DeepSeek Native: erro "tool_calls must be followed by tool messages"

**Sessão:** 2026-09-10
**Status:** Investigação completa — causa raiz identificada. Nenhuma correção aplicada (aguarda decisão).

---

## 1. Resumo Executivo

O erro recorrente do DeepSeek nativo em requests de resume no build mode:

> "An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'. (insufficient tool messages following tool_calls message)"

**NÃO é governor/rate limit.** É um **bug estrutural na montagem do histórico do resume** no HOK OS: o payload enviado à API termina com uma mensagem `assistant` contendo `tool_calls` sem nenhuma mensagem `tool` respondendo ao `tool_call_id`. O OpenRouter (Claude/Nemotron) tolera o histórico malformado; a API nativa DeepSeek valida e rejeita.

---

## 2. Metodologia de Reprodução

- Binário de diagnóstico isolado (`hokma_diag`), porta **9912**, DB em `/tmp/opencode/deepseek_diag/memory.db`
- Log do **payload JSON exato antes do POST** (gated por env var `DIAG_PAYLOAD_DIR`, removido após o diagnóstico)
- Fluxo: `orchestrate` → `approve` (resume) com `deepseek-native/deepseek-flash`, build mode, plan 2 arquivos
- Evidências preservadas em: `contexto-hokma/diag_deepseek_toolcalls_20260910/`

## 3. Resultado da Reprodução

| Tentativa | Resume executou? | Erro DeepSeek? |
|---|---|---|
| try1 | sim | ❌ erro |
| rep2 | sim | ❌ erro |
| rep3 | sim | ❌ erro |
| rep4 | **não** (run_engine falhou antes) | ✅ sem erro (LLM não chamado) |
| rep5 | sim | ❌ erro |
| rep6 | sim | ❌ erro |

**Conclusão: 100% reproduzível quando o resume chama o LLM (5/5).** A única não-reprodução ocorreu porque o `run_engine` falhou antes do resume — early-return em `pending_action.go:495`. O bug é determinístico: o payload malformado é sempre o mesmo.

## 4. Payload Exato do Request que Falha

```json
{
  "model": "deepseek-flash",
  "messages": [
    {"role": "system", "content": "Mutacao 1 ja executada: Executar tarefa no engine opencode..."},
    {"role": "system", "content": "Nenhum subagente configurado. Resolva a tarefa diretamente com as tools disponiveis."},
    {"role": "assistant",
     "content": "Vou expandir a tarefa e executar as mutações uma por vez...",
     "tool_calls": [{
       "id": "call_00_0GXGknIFclFeu3SVE7EU4605",
       "type": "function",
       "function": {"name": "run_engine", "arguments": "{\"engine\": \"opencode\", \"task\": \"Tarefa de mutacao unica (arquivo 1 de 2)...\"}"}
     }]}
  ],
  "tools": ["... 14 tools ..."]
}
```

**Veredito estrutural:** o assistant com `tool_calls=[call_00_0GXGknIFclFeu3SVE7EU4605]` é a **última mensagem do array** e **não há nenhuma mensagem `role:"tool"` com esse `tool_call_id`**. Tool_call órfão.

## 5. Comparação: DeepSeek (falha) vs claude-sonnet-4 (funciona)

O payload de resume do `claude-sonnet-4` tem **exatamente a mesma estrutura malformada**:
- [0] system
- [1] system
- [2] assistant com `tool_calls=[toolu_bdrk_...]` — **sem tool response seguinte**

**Diferença:** não é estrutural no payload — é a **tolerância da API**:
- **OpenRouter** (Claude e fallback Nemotron): aceita o histórico malformado (leniente)
- **API nativa DeepSeek** (`api.deepseek.com`): valida estritamente e rejeita

Prova adicional: o mesmo payload malformado, no fallback para `nvidia/nemotron-3-super-120b-a12b:free` via OpenRouter, funcionou (`finish=stop`).

## 6. Causa Raiz (arquivo:linha)

### 6.1 Causa primária — `pending_action.go:520-528`
O resume chama `RunOrchestrator` **sem o campo `ApprovalResult`**:

```go
resumedResp := RunOrchestrator(ctx, OrchestratorRequest{
    Task:       progressMsg,
    ConvID:     convID,
    TenantID:   tenantID,
    Mode:       "build",
    MaxSteps:   15,
    Model:      state.UsedModel,
    ResumeFrom: state,
})  // <-- ApprovalResult ausente
```

### 6.2 Código de correção já existe, mas está morto — `agent_orchestrator.go:352-367`
A injeção das mensagens `tool` para cada `tool_call_id` do último assistant só executa sob condição que nunca é verdadeira:

```go
if req.ApprovalResult != "" {   // <-- sempre falso: ninguém popula o campo
    for i := len(messages) - 1; i >= 0; i-- {
        if messages[i].Role == "assistant" && len(messages[i].ToolCalls) > 0 {
            for _, tc := range messages[i].ToolCalls {
                messages = append(messages, chatMessage{
                    Role:       "tool",
                    ToolCallID: tc.ID,
                    Name:       tc.Function.Name,
                    Content:    req.ApprovalResult,
                })
            }
            break
        }
    }
}
```

O campo `ApprovalResult` **nunca é populado em nenhum lugar do código** (confirmado por grep: apenas declaração na struct e uso na condição).

### 6.3 Mecânica do bug
Em build mode, ao criar a pending_action, o orquestrador retorna **antes** de anexar o tool result (correto por design — o resultado só existe após aprovação do usuário). No resume, o tool message correspondente **deveria** ser injetado, mas a injeção nunca roda. Resultado: histórico termina com tool_call órfão.

## 7. Achado Secundário — Schema incompleto

`db.go:254-266`: o schema de `orchestrator_state` **não inclui a coluna `plan`**. Em produção ela foi adicionada manualmente via `ALTER TABLE`. Num DB fresco (novo deploy), o resume falha com:

```
SQL logic error: no such column: plan (1)
```

Reproduzido no diagnóstico antes do ALTER TABLE manual. **Risco de deploy:** ambiente novo quebra o resume.

## 8. Correção Proposta (não aplicada)

**Mínima:** popular `ApprovalResult` no call de resume em `pending_action.go:520-528`, ex.:
```go
ApprovalResult: "Aprovado e executado: " + truncateStr(result, 200),
```
A injeção existente em `agent_orchestrator.go:352-367` passaria a funcionar e o histórico teria o tool response correto.

**Recomendada também:** adicionar coluna `plan` ao schema em `db.go:254-266` (com migração defensiva `ALTER TABLE` para DBs existentes).

**Decisão pendente do usuário** — nenhuma alteração de código feita. Produção intocada (binário `hokma` MD5 `3df8e8da...`, serviço ativo).

## 9. Arquivos de Evidência

- `contexto-hokma/diag_deepseek_toolcalls_20260910/payload_*_deepseek-flash.json` — payloads exatos (inicial + que falha)
- `contexto-hokma/diag_deepseek_toolcalls_20260910/payload_*_nvidia_nemotron*.json` — fallback que funcionou com o mesmo payload malformado
- `contexto-hokma/diag_deepseek_toolcalls_20260910/run_log.txt` — log da execução com o erro

## 10. Conclusão

| Pergunta | Resposta |
|---|---|
| Reproduzido isoladamente? | ✅ Sim |
| 100% ou intermitente? | ✅ 100% quando o resume chama o LLM (5/5) |
| Payload exato capturado? | ✅ Sim, tool_call órfão comprovado |
| Diagnóstico com arquivo:linha? | ✅ `pending_action.go:520-528` + `agent_orchestrator.go:352-367` |
| É governor/rate limit? | ❌ Não — bug estrutural de formato |
| Correção aplicada? | ❌ Não — aguarda decisão |
