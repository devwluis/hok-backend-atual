# ADENDO — SESSÃO 04/09/2026 (rodada 2) — Política estrita free-only: smart_chat.go

Continuação do adendo `ADENDO_SESSAO_20260904_correcao_politica_free_only.md` (que tratou `ai.go` e `routes.go`). Esta sessão aplica o mesmo padrão no `smart_chat.go`, removendo as últimas chamadas a modelos pagos (`callGeminiVision` e `callOpenAIVision`) do caminho de visão do chat do frontend.

## 1. Contexto

- Sessão anterior (04/09, rodada 1): corrigiu `ai.go` e `routes.go` com 4 mudanças. `callOpenAIVision` foi desativada (sempre retorna erro). `callGeminiText("gemini-2.5-flash-lite", ...)` removida. `callGeminiVision("gemini-2.0-flash", ...)` removida da rota `/vision`.
- Pendência: `smart_chat.go` ainda tinha 3 chamadas a `callGeminiVision` (linhas 129, 166, 176) e 1 a `callOpenAIVision` (linha 178) no fluxo de voz+visão e visão isolada do chat. **Esta sessão corrige isso.**

## 2. Backup criado

```
smart_chat.go.bak_20260904_204611  (41.023 bytes, md5=af21834f551f7a9f5e2ca0912e608348)
```

Localização: `/root/hokma/backend/`

## 3. Mudanças aplicadas

### 3.1 `smart_chat.go:126-142` (caso `req.AudioB64 != "" && req.ImageB64 != ""`)

**ANTES:** DeepSeek Vision falhava → tentava `callGeminiVision` (pago) → label `voice_vision_gemini`.

**DEPOIS:** DeepSeek Vision falhava → tenta `callORVision(ModelB)` (Minimax-M3 free, pricing 0/0) → label `voice_vision_minimax_m3_free`.

```diff
 		reply, vErr := callDeepSeekVision(req.ImageB64, mimeType, prompt)
 		if vErr != nil {
 			log.Printf("DeepSeek VL audio+img falhou: %v", vErr)
-			reply, vErr = callGeminiVision(req.GeminiKey, req.ImageB64, mimeType, prompt)
+			// FIX 04/09: política estrita free-only. Substituído
+			// callGeminiVision (gemini-2.0-flash PAGO via GEMINI_KEY) por
+			// callORVision(ModelB) — Minimax M3 free (pricing 0/0
+			// confirmado) com visão já testada em produção desde 03/09.
+			reply, vErr = callORVision(req.OrKey, ModelB, req.ImageB64, mimeType, prompt)
 			if vErr != nil {
 				resp.Reply = "Erro visao+audio: " + vErr.Error()
 				resp.Mode = "error"
 				break
 			}
-			resp.Mode = "voice_vision_gemini"
+			resp.Mode = "voice_vision_minimax_m3_free"
 		} else {
 			resp.Mode = "voice_vision_deepseek_vl"
 		}
```

### 3.2 `smart_chat.go:167-189` (caso `req.ImageB64 != ""`)

**ANTES:** Fluxo com 4 níveis de fallback: DeepSeek → Gemini → OR/Minimax-M3 → Gemini (de novo!) → OpenAI/GPT-4o-mini.

**DEPOIS:** Fluxo com 2 níveis: DeepSeek → OR/Minimax-M3-free. Se o free falhar, retorna erro claro. **Removidos:** Gemini Vision (linha 166 e 176) + OpenAI Vision (linha 178).

```diff
 		reply, vErr := callDeepSeekVision(req.ImageB64, mimeType, prompt)
 		if vErr != nil {
 			log.Printf("DeepSeek VL falhou: %v", vErr)
-			reply, vErr = callGeminiVision(req.GeminiKey, req.ImageB64, mimeType, prompt)
+			// FIX 04/09: política estrita free-only. Substituído
+			// callGeminiVision (gemini-2.0-flash PAGO via GEMINI_KEY) por
+			// callORVision(ModelB) — Minimax M3 free (pricing 0/0) com
+			// visão confirmada em produção desde 03/09.
+			reply, vErr = callORVision(req.OrKey, ModelB, req.ImageB64, mimeType, prompt)
 			if vErr != nil {
-				log.Printf("Gemini falhou: %v", vErr)
-				// FIX 03/09: fallbacks do OpenRouter eram modelos PAGOS
-				// (qwen2.5-vl-72b-instruct, claude-haiku-4.5) — gastavam crédito
-				// sempre que o Gemini caía em rate-limit. Trocar para ModelB
-				// (minimax-m3:free, pricing 0/0) com visão confirmada.
-				reply, vErr = callORVision(req.OrKey, ModelB, req.ImageB64, mimeType, prompt)
-				if vErr != nil {
-					log.Printf("Minimax M3 VL free falhou: %v", vErr)
-					reply, vErr = callGeminiVision(req.GeminiKey, req.ImageB64, mimeType, prompt)
-					if vErr != nil {
-						reply, vErr = callOpenAIVision(req.OpenAIKey, req.ImageB64, mimeType, prompt)
-						if vErr != nil {
-							resp.Reply = "Erro visao: " + vErr.Error()
-							resp.Mode = "error"
-							break
-						}
-						resp.Mode = "vision_gpt4o_mini"
-					} else {
-						resp.Mode = "vision_gemini"
-					}
-				} else {
-					resp.Mode = "vision_minimax_m3_free"
-				}
-			} else {
-				resp.Mode = "vision_gemini"
+				log.Printf("Minimax M3 VL free falhou: %v", vErr)
+				// FIX 04/09: removido callGeminiVision terciário
+				// (gemini-2.0-flash PAGO) e callOpenAIVision final
+				// (gpt-4o-mini PAGO). Política estrita: falhar com
+				// erro claro a cobrar crédito. ModelB já é o último
+				// fallback free — se falhar, retornamos erro.
+				resp.Reply = "Erro visao: " + vErr.Error() + " (política free-only: sem fallback pago)"
+				resp.Mode = "error"
+				break
 			}
+			resp.Mode = "vision_minimax_m3_free"
 		} else {
 			resp.Mode = "vision_deepseek_vl"
 		}
```

## 4. Sobre o tratamento do `callOpenAIVision` (solicitação do usuário)

O usuário pediu para confirmar que a chamada a `callOpenAIVision` na linha 178 (que na sessão anterior foi **desativada** em `ai.go` — sempre retorna erro) tem "fallback sensato" no smart_chat.

**Decisão:** o caminho que continha `callOpenAIVision` (linha 178) foi **removido** como parte da simplificação do fluxo de visão. Agora o fluxo de imagem termina em `callORVision(ModelB)` — se o ModelB free falhar, retorna erro claro com mensagem `"Erro visao: ... (política free-only: sem fallback pago)"`.

Como `callOpenAIVision` não é mais chamada, a preocupação com "erro não tratado" desapareceu. A função `callOpenAIVision` continua existindo em `ai.go` (sempre retorna erro), mas **nenhum call-site** a invoca mais.

## 5. Grep final (confirmação)

Comando: `grep -rEn "gemini-2\.5-flash-lite|gemini-2\.0-flash|gpt-4o|gpt-4o-mini" smart_chat.go`

**Resultado: 4 ocorrências, todas INERTES (comentários FIX):**

| Linha | Tipo | Conteúdo |
|---|---|---|
| 130 | comentário FIX | `// callGeminiVision (gemini-2.0-flash PAGO via GEMINI_KEY) por` |
| 171 | comentário FIX | `// callGeminiVision (gemini-2.0-flash PAGO via GEMINI_KEY) por` |
| 178 | comentário FIX | `// (gemini-2.0-flash PAGO) e callOpenAIVision final` |
| 179 | comentário FIX | `// (gpt-4o-mini PAGO). Política estrita: falhar com` |

**Zero chamada real a modelo pago no smart_chat.go. ✅**

Confirmação adicional (chamadas executáveis):
```bash
grep -nE "(callGemini|callOpenAI)" smart_chat.go | grep -v "^\s*//\|//.*callGemini\|//.*callOpenAI"
# (resultado vazio)
```

## 6. Testes (build, vet, test)

### `go build -o hokma_test .`
✅ **PASS** — binário gerado (19.529.495 bytes, timestamp 20:46).

### `go vet ./...`
✅ **PASS** — sem warnings, exit 0.

### `go test ./...`
✅ **PASS** — `ok hokma_backend 31.967s` (suíte completa, sem falhas).

## 7. Estado final

| Item | Estado |
|---|---|
| `smart_chat.go.bak_20260904_204611` | ✅ Backup criado |
| `smart_chat.go` modificado | ✅ 2 mudanças (caso voz+imagem + caso imagem) |
| `hokma_test` (binário isolado) | ✅ Compilado, 19.5 MB |
| Build/vet/test | ✅ Tudo PASS |
| `hokma.service` em produção | ⏸️ **NÃO reiniciado** — aguardando aprovação |
| Commit/push | ⏸️ **NÃO feito** — aguardando aprovação |

## 8. Resumo consolidado — 3 sessões da rodada free-only

| Sessão | Arquivo | Mudanças |
|---|---|---|
| 04/09 (rodada 0) | (apenas investigação) | Diagnóstico sem alteração |
| 04/09 (rodada 1) | `ai.go` + `routes.go` | 4 mudanças: cascata chat, rota /smart, rota /vision (2 fallbacks), callOpenAIVision desativada |
| **04/09 (rodada 2 — esta)** | `smart_chat.go` | **2 mudanças: voz+visão (1 Gemini removido) + imagem (2 Gemini + 1 OpenAI removidos)** |

**Total: 6 mudanças em 3 arquivos, política estrita free-only 100% aplicada no caminho de chat+visão.**

## 9. Próximo passo

Aguardando aprovação para:
1. **Restart do `hokma.service`** com o binário `hokma_test` (após smoke test).
2. **Commit/push** dos 3 arquivos modificados para `hok-backend-atual`.

**Data/Hora:** 04/09/2026, ~20:47 UTC
**Status:** Correções aplicadas, testadas, com backup. **Sem restart, sem commit, sem push — aguardando aprovação.**
