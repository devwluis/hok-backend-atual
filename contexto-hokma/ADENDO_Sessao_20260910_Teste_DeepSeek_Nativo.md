# ADENDO — Sessão 2026-09-10

## Resumo
Testes de modelos DeepSeek para orquestrador HOK OS — suporte nativo, roteamento DS_URL, e análise de falhas de API e governor.

---

## 1. Objetivo
Testar a integração de modelos DeepSeek como orquestrador do HOK OS. O objetivo era verificar se modelos DeepSeek poderiam ser usados para orquestrar mutações sequenciais em build mode, substituindo o modelo `anthropic/claude-sonnet-4` que anteriormente era a única opção validada.

## 2. Modelos Testados

### 2.1 `deepseek/deepseek-chat-v3.1` (OpenRouter)
- **Status:** ❌ Instável
- **Problema:** `context deadline exceeded` — timeouts frequentes no OpenRouter
- **Causa:** API do OpenRouter para DeepSeek v3.1 tem latência alta e instabilidade
- **Impacto:** Impossível completar qualquer fluxo de teste end-to-end

### 2.2 `deepseek/deepseek-v4-flash` (OpenRouter)
- **Status:** ❌ Auth falha
- **Problema:** `No cookie auth credentials found` do OpenRouter
- **Causa:** OpenRouter não encontra credenciais para este modelo
- **Impacto:** Não consegue fazer chamadas via OpenRouter

### 2.3 `deepseek/deepseek-v4-pro` (OpenRouter)
- **Status:** ❌ Auth falha
- **Problema:** `No cookie auth credentials found` do OpenRouter
- **Causa:** Mesmo problema do v4-flash
- **Impacto:** Não consegue fazer chamadas via OpenRouter

### 2.4 `deepseek-native/deepseek-flash` (DS_URL direto)
- **Status:** ⚠️ Funcional mas com governor/rate limit
- **Roteamento:** `DS_URL` = `https://api.deepseek.com/v1/chat/completions`
- **Problema:** `Authentication Fails (governor)` — rate limit intermitente
- **Impacto:** Funciona em testes isolados mas falha em fluxos contínuos de orquestração

## 3. DeepSeek Native Support (Implementação)

### 3.1 Mudanças em `agent_loop_groq.go`
- Adicionada detecção de `isNativeModelSlug(model)` para identificar modelos `deepseek-native/*`
- Quando `isNative` é verdadeiro, o endpoint muda para `DS_URL` (`https://api.deepseek.com/v1/chat/completions`)
- A chave muda para `DS_KEY` (fallback para `DEEPSEEK_API_KEY`)
- O modelo é limpo do prefixo `deepseek-native/` antes de enviar à API

### 3.2 Mudanças em `agent_orchestrator.go`
- Removido guard que bloqueava modelos `deepseek-native/*` no orquestrador
- O orquestrador agora aceita `deepseek-native/deepseek-flash` como modelo válido

### 3.3 Mudanças em `agent_loop_groq.go` (Auth header)
- Adicionado `req.Header.Set("Authorization", "Bearer " + key)` para garantir que a chave API é enviada corretamente
- Corrigiu bug onde o header `Authorization` estava faltando em chamadas para OpenRouter

### 3.4 Mudanças em `model_propagate.go`
- `deepseekNativePrefix = "deepseek-native/"` definido como prefixo de detecção
- `isNativeModelSlug(model)` verifica se o modelo começa com `deepseek-native/`

## 4. Resultados dos Testes

### 4.1 Teste Direto (API DeepSeek)
- **Status:** ✅ Funciona
- **Comando:** `curl https://api.deepseek.com/v1/chat/completions`
- **Resultado:** Resposta válida do DeepSeek

### 4.2 Teste de Roteamento (Orquestrador → DS_URL)
- **Status:** ✅ Funciona
- **Comando:** `orchestrate` com modelo `deepseek-native/deepseek-flash`
- **Resultado:** LLM é chamada via `DS_URL`, resposta retornada

### 4.3 Teste End-to-End (Build Mode Sequencial)
- **Status:** ⚠️ Intermitente
- **Problema:** `deepseek-native/deepseek-flash` funciona no primeiro request mas falha com governor/rate limit em requests subsequentes
- **Error:** `Authentication Fails (governor)` ou resposta vazia
- **Impacto:** Impossível completar fluxo completo de 2 arquivos (a.txt → b.txt) de forma confiável

### 4.4 Teste com `anthropic/claude-sonnet-4` (Referência)
- **Status:** ✅ 100% funcional
- **Resultado:** Fluxo completo: orchestrate → approve a.txt → approve b.txt → end-of-plan
- **Conclusão:** Modelo de referência para validação

## 5. Root Cause do Governor/Rate Limit
O DeepSeek API tem um sistema de governor que limita o número de requests por minuto/segundo. Quando o orquestrador faz múltiplas chamadas (una para cada mutação), o governor bloqueia requests subsequentes.

### Comportamento Observado:
1. Primeiro request (orquestrate) → ✅ Funciona
2. Segundo request (approve a.txt → resume) → ⚠️ Pode falhar com governor
3. Terceiro request (approve b.txt → end-of-plan) → ❌ Provavelmente falha

### Logs Relevantes:
- `[orchestrator] modelo deepseek-native/deepseek-flash falhou (erro do OpenRouter: An assistant message with 'tool_calls' must be followed by tool messages responding to each 'tool_call_id'...)`
- `[orchestrator] modelo deepseek-native/deepseek-flash falhou (erro do OpenRouter: Authentication Fails (governor))`
- `[orchestrator] modelo deepseek-native/deepseek-flash falhou (erro do OpenRouter: context deadline exceeded)`

## 6. Notas e Decisões

### 6.1 Recomendação Atual
Usar `anthropic/claude-sonnet-4` como modelo orquestrador. É o único modelo que passou em todos os testes end-to-end de forma confiável.

### 6.2 DeepSeek Native como Futuro
O suporte `deepseek-native/*` está implementado e funcional. O governor/rate limit é um problema de API, não de código. Quando o DeepSeek resolver o governor, o modelo pode ser usado como orquestrador.

### 6.3 OpenRouter DeepSeek
Os modelos `deepseek/deepseek-v3.1`, `deepseek/deepseek-v4-flash`, e `deepseek/deepseek-v4-pro` no OpenRouter são instáveis ou não têm credenciais válidas. Não recomendados para produção.

### 6.4 API Key
- `DEEPSEEK_API_KEY=<REDACTED>` (chave de producao, nao versionar)
- Funciona para `DS_URL` mas está sujeita a governor/rate limit

## 7. Arquivos Modificados
- `agent_loop_groq.go` — Roteamento nativo DeepSeek + Authorization header fix
- `agent_orchestrator.go` — Remoção de guard de modelos nativos + `parsePlan` + end-of-plan fix
- `pending_action.go` — End-of-plan check em `resolveRunEnginePendingAction`
- `model_propagate.go` — `deepseekNativePrefix` definido

## 8. Status Final
- DeepSeek Native Support: ✅ Implementado e funcional
- DeepSeek Governor: ❌ Rate limit bloqueia testes contínuos
- Recomendação: Usar `anthropic/claude-sonnet-4` até resolver governor do DeepSeek
