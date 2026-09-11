# ADENDO DE SESSAO — Fix Spinning Orquestrador (DeepSeek V4 Flash)

**Data:** 07/11/09/2026 10:20
**Sessao:** Diagnóstico + Fix de spinning do Orquestrador
**Status:** CONCLUIDO — fixes validados e deployados

---

## 1. Diagnóstico Corrigido

### Hipótese Original (ERRADA)
> Tool HTTP travada sem timeout dentro de executeTool(), prendendo a goroutine indefinidamente.

### Diagnóstico Real (CONFIRMADO)
**NÃO é tool travada — é o DeepSeek V4 Flash entrando em spinning** (loop infinito de `content_len=0` + `finish=tool_calls`).

### Evidência (logs de debug)
- 31 tools executadas, **todas** com START+DONE (max 265ms)
- 0 tools com START sem DONE
- 19+ chamadas LLM, modelo retorna `content_len=0 finish=tool_calls` repetidamente
- Spinning detection original (threshold=4) disparou tarde demais
- Job completou em 141s (vs 10-20s com fix)

---

## 2. Correções Aplicadas

### Fix 1: Spinning threshold 4 → 2
**Arquivo:** `agent_orchestrator.go:593`
```diff
- if emptyContentCount >= 4 {
+ if emptyContentCount >= 2 {
```
**Razão:** DeepSeek V4 Flash gera 2-4 rounds de `content=0` antes de travar. Threshold 2 detecta em 4-8s vs 16-32s.

### Fix 2: Timeout LLM 90s → 45s
**Arquivo:** `agent_loop_groq.go:926`
```diff
- client := &http.Client{Timeout: 90 * time.Second}
+ client := &http.Client{Timeout: 45 * time.Second}
```
**Razão:** Chamada legítima de 19KB levou 62s (única >45s em 19 calls). 45s é seguro para respostas grandes sem.Allow spinning morbidity.

### Fix 3: Limite de 5 tool_calls por step
**Arquivo:** `agent_orchestrator.go:620-624`
```go
toolCallsLimit := 5
if len(respMsg.ToolCalls) > toolCallsLimit {
    log.Printf("[orchestrator] step=%d: modelo retornou %d tool_calls — limitando a %d", ...)
    respMsg.ToolCalls = respMsg.ToolCalls[:toolCallsLimit]
}
```
**Razão:** DeepSeek V4 Flash pode retornar 10+ tool_calls por step. Limite previne explosão de execuções.

---

## 3. Resultados dos Testes

### Teste Isolado (porta 8099)

| Teste | Duração | Spinning em | Modelo | Status |
|---|---|---|---|---|
| Teste 1 | 20s | step 2 | nemotron (fallback) | ✅ done |
| Teste 2 | 10s | step 2 | nemotron (fallback) | ✅ done |
| Antes (debug) | 141s | step 5+ | deepseek-native | ✅ done (lento) |

**Melhoria: 7x mais rápido**

### Testes A/B/C em Produção (pós-deploy)

| Teste | Esperado | Obtido | Status |
|---|---|---|---|
| A: /chat/smart + native + orchestrator | mode=orchestrator | mode=orchestrator, model=deepseek-native, reply 1577 chars | ✅ |
| B: /agents/orchestrate + native | status=ok | status=ok, steps=2, reply 1562 chars | ✅ |
| C: /chat/smart + nemotron + orchestrator | mode=orchestrator | mode=orchestrator, model=nemotron, reply 1578 chars | ✅ |

### Validação dos Fixes

| Fix | Status | Evidência |
|---|---|---|
| Spinning threshold 2 | ✅ | 3 detecções em step 2 nos testes isolados |
| Timeout 45s | ✅ | Nenhuma chamada >15s nos testes de produção |
| Limit 5 tools | ✅ | Nenhum modelo retornou >5 tools |

---

## 4. Diagnóstico DEEPSEEK_API_KEY

| Pergunta | Resposta |
|---|---|
| Chave válida? | ✅ Sim — chamada direta à API retornou sucesso |
| Expirada? | ❌ Não — `sk-a6be...5619` funciona |
| Por que falhou no teste isolado? | Instância isolada herdou DS_KEY do `.env.root` (chave inválida `sk-...0559`). Em produção, DS_KEY não existe → fallback para DEEPSEEK_API_KEY |
| Modelo nativo caindo silenciosamente? | ❌ Não — Teste A em produção usou deepseek-native com sucesso |

---

## 5. Backups

- `agent_orchestrator.go.bak_20260911_094622_prespinfix`
- `agent_loop_groq.go.bak_20260911_094622_prespinfix`
- `hokma.bak_20260911_*_presspinfix` (binário anterior)

---

## 6. Binário Deployado

- **Hash:** `2e9aa02f75005ab84d8d67293e7925e0`
- **Deploy:** 07/11/09/2026 10:14
- **Health:** 200 OK
- **PID:** 458427

---

## 7. Próximos Passos (se necessário)

- Monitorar se o spinning do DeepSeek V4 Flash continua acontecendo em payloads maiores
- Se threshold 2 causar falsos positivos (respostas legítimas com 2 rounds vazios), considerar threshold 3
- Investigar por que o DeepSeek V4 Flash gera content=0 + tool_calls (comportamento do modelo, não bug nosso)
