# ADENDO_SESSAO_20260911_Fix_Roteamento_Nativo_Chat.md

## Resumo

Bug crítico: ao selecionar um modelo `deepseek-native/*` no chat web, a seleção aparecia visualmente mas a chamada real ia para o OpenRouter (créditos OpenRouter sendo consumidos em vez do DeepSeek). Causa: o chat web só honrava a rota nativa no caminho de chat simples (`buildFallbackChat` → `routeModel`); os engines auxiliares (orquestrador/hermes/opencode/n8n) reescreviam o modelo para `ModelB` (OpenRouter) pela política free-only. Corrigido com um guard nativo no topo da cascata + remoção do fallback pago `ModelA`.

## Causa raiz (confirmada por log em produção)

```
07:18:00 [smart_chat] Modelo deepseek-native/deepseek-flash é pago, usando minimax/minimax-m3:free no lugar.
07:18:00 [orchestrator] modelo minimax/minimax-m3:free falhou (...) — fallback deepseek/deepseek-chat-v3.1
```

Fluxo do vazamento (engine = Orquestrador):
1. `tryOrchestrator` (`smart_chat.go:517`) via `req.Model = deepseek-native/*`, `isFreeModel()` = false → reescrevia para `ModelB` (`minimax/minimax-m3:free`, OpenRouter).
2. `ModelB:free` foi **descontinuado** no OpenRouter ("unavailable for free") → fallback para `deepseek/deepseek-chat-v3.1` (ModelA, **PAGO**, OpenRouter).
3. Resultado: modelo nativo nunca chegava na rota nativa; OpenRouter cobrado.

Confirmação adicional por API: `minimax/minimax-m3:free` não existe mais na lista do OpenRouter; `deepseek/deepseek-chat-v3.1` tem pricing pago (0.00000025/0.00000095); o único free verificado agora é `nvidia/nemotron-3-super-120b-a12b:free` (ModelC, pricing 0/0).

## Fixes implementados

### Fix 1 — Guard nativo no topo da cascata (`smart_chat.go`)
- Helpers `isNativeModelSlug()` + `nativeModelSelection(req)` (prioriza `req.Model`, cai para o modelo ativo global).
- `buildFallbackChat` refatorado em `buildFallbackChat` (wrapper) + `buildFallbackChatWithModel(msg, req, agentFailure, model)`.
- Guard no início de `runSmartTextCascade`: se o modelo selecionado/ativo for `deepseek-native/*`, vai DIRETO ao chat → `routeModel` → DS_URL, sem passar por nenhum engine auxiliar.

### Fix 2 — `isFreeModel` não mente mais sobre `ModelA` (`ai.go`)
- Removido `ModelA` da cláusula free. `deepseek/deepseek-chat-v3.1` é PAGO — tratá-lo como free causava cobrança silenciosa no fallback, afetando não só o teste do nativo (bug antigo).

### Fix 3 — Fallback nunca pago (`ai.go`, `agent_orchestrator.go`)
- `callLLMWithFallback`: modelo ativo pago → usa `ModelC` (free verificado), não `ModelB` (delistado); `fallbackModel` sempre free (nunca `ModelA`).
- `agent_orchestrator.go` (orquestrador + subagente): cadeias de fallback passam a usar só `ModelC`.

## Validação (produção)

| Teste | Resultado |
|---|---|
| `POST /chat/smart` com `forceOrchestrator=true` + `model=deepseek-native/deepseek-flash` | `mode: chat`, `engine: chat`, `model_used: deepseek-native/deepseek-flash` ✓ |
| OpenRouter antes/depois | **8.074463543 → 8.074463543 (inalterado)** ✓ |
| Log | `[usage] ... cached=1280` (campo DeepSeek nativo); **sem** "é pago, usando minimax"; **sem** `[orchestrator]` ✓ |
| Smoke chat trivial | `mode: chat`, `model_used: deepseek-native/deepseek-flash` ✓ |
| Health | 200 ✓ |

## Deploy / Commit / Push
- `./deploy.sh`: build `422f08ad`, tests OK, health 200 (retry do health check funcionou), hashes idênticos.
- Backend `5db5fee` (main, hok-backend-atual): "fix: roteamento nativo respeitado no chat (guard deepseek-native) + remove fallback pago ModelA" — `smart_chat.go`, `ai.go`, `agent_orchestrator.go`. Push OK.
- Frontend não alterado. WIP antigo (agent_loop.go/terminal_routes.go/etc.) não entrou no commit.

## Observações
- Com modelo nativo selecionado, engines auxiliares (orquestrador/hermes/opencode via serve/n8n) NÃO são usados para chat — comportamento intencional e previsível para a comparação de custo entre chat web / opencode / Claude Code.
- `ModelB = minimax/minimax-m3:free` está delistado no OpenRouter; a constante não foi alterada (amplo demais), mas não é mais usada como fallback pago.
- opencode nativo (`~/.config/opencode`) e Claude Code (`/anthropic`) permanecem intactos.

## Status final
- [x] build isolado + go test OK
- [x] deploy + validação real (orquestrador + nativo não vaza)
- [x] smoke test
- [x] commit separado + push
- [x] adendo enviado via webhook n8n