# ADENDO — SESSÃO 04/09/2026 — Correção: política estrita free-only (vazamento de crédito OpenRouter)

Sessão de aplicação de patches na investigação do adendo anterior (`ADENDO_SESSAO_20260904_investigacao_vazamento_pago_or.md`). **Modo estrito free-only**: somente modelos com pricing 0/0 confirmado do OpenCode Zen/Go ou OpenRouter (`:free` ou `pricing 0/0`). Nada de Gemini direto via `GEMINI_KEY`, nada de OpenAI via chave própria.

## 1. Backups criados (timestamp)

```
ai.go.bak_20260904_203436       (28.882 bytes)
routes.go.bak_20260904_203436   (27.349 bytes)
```

Localização: `/root/hokma/backend/`

## 2. Mudanças aplicadas

### 2.1 `ai.go:783-794` — `callLLMWithFallback` cascata
**REMOVIDO:** provider "Gemini/Flash-Lite" (`gemini-2.5-flash-lite` pago via `GEMINI_KEY`).

A cascata agora segue direto de `Cerebras/Llama-70B` (gpt-oss-120b) → `OR/Llama-70B` (free) → `OR/Gemma-4-31B` (free) → `AIHubMix/GPT-5.5-free` → `AIHubMix/GLM-5.3-Flash-free`. Todos free confirmados.

```diff
-		{
-			Name:    "Gemini/Flash-Lite",
-			URL:     "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
-			AuthEnv: "GEMINI_KEY",
-			Model:   "gemini-2.5-flash-lite",
-		},
+		// FIX 04/09: removido "Gemini/Flash-Lite" (gemini-2.5-flash-lite, PAGO)
+		// da cascata — política estrita: SOMENTE modelos free via OpenCode/
+		// OpenRouter (pricing 0/0 confirmado). Nada de Gemini direto via
+		// GEMINI_KEY, nada de OpenAI via chave própria. A cascata segue
+		// direto para os fallbacks OpenRouter/AIHubMix free abaixo.
 		{
 			Name:    "OR/Llama-70B",
```

### 2.2 `routes.go:215-232` — rota `/smart` fallback
**REMOVIDO:** bloco `callGeminiText(geminiKey, "gemini-2.5-flash-lite", msgs)` (pago).

Quando o modelo ativo falha → `callOR` (padrão) → se falhar → pula direto para `callOR("meta-llama/llama-3.3-70b-instruct:free", msgs)`.

```diff
 		reply, err = callOR(normalizeModelSlugForAPI(getDefaultChatModel()), msgs)
 		if err != nil {
-			geminiKey := os.Getenv("GEMINI_KEY")
-			if geminiKey != "" {
-				reply, err = callGeminiText(geminiKey, "gemini-2.5-flash-lite", msgs)
-			}
-			if geminiKey == "" || err != nil {
-				reply, err = callOR("meta-llama/llama-3.3-70b-instruct:free", msgs)
-			}
+			// FIX 04/09: removido callGeminiText("gemini-2.5-flash-lite", PAGO)
+			// desta cascata — política estrita: SOMENTE modelos free via
+			// OpenCode/OpenRouter. Nada de Gemini direto via GEMINI_KEY.
+			// Pula direto para o Llama-70B free (próximo fallback).
+			reply, err = callOR("meta-llama/llama-3.3-70b-instruct:free", msgs)
 			if err != nil {
-				respondJSON(w, map[string]string{"status": "error", "reply": "Todos os modelos indisponíveis: " + err.Error()})
+				respondJSON(w, map[string]string{"status": "error", "reply": "Todos os modelos free indisponíveis: " + err.Error()})
```

### 2.3 `routes.go:293-308` — rota `/vision` (Gemini fallback)
**REMOVIDO:** bloco `callGeminiVision(geminiKey, ...)` (gemini-2.0-flash pago).

A versão `gemini-2.0-flash-exp:free` foi removida do OpenRouter em 03/09 (logs confirmam "No endpoints found"). Sem substituto free viável. Política estrita: falhar com erro é melhor que cobrar crédito. Mantido o registro em `attempted` apenas para diagnóstico.

```diff
-	// 2. Gemini fallback
+	// 2. Gemini fallback — REMOVIDO 04/09 (política estrita free-only)
+	// O fallback usava "gemini-2.0-flash" (PAGO via GEMINI_KEY direto).
+	// A versão :exp:free foi removida do OpenRouter em 03/09 ("No endpoints
+	// found") e não há substituto free viável agora. Política estrita:
+	// falhar com erro claro é melhor que cobrar crédito. Mantemos o
+	// registro em `attempted` apenas para diagnóstico.
 	if reply == "" {
-		geminiKey := req.GeminiKey
-		if geminiKey == "" {
-			geminiKey = GEMINI_KEY
-		}
-		if geminiKey != "" {
-			var err error
-			reply, err = callGeminiVision(geminiKey, req.ImageB64, req.MimeType, req.Prompt)
-			attempted = append(attempted, "gemini-2.0-flash")
-			if err != nil {
-				log.Printf("⚠ Gemini Vision falhou: %v", err)
-				reply = ""
-			}
-		}
+		attempted = append(attempted, "gemini-2.0-flash (desativado, política free-only)")
+		log.Printf("⚠ Vision: Gemini fallback desativado (política free-only 04/09)")
 	}
```

### 2.4 `routes.go:310-325` — rota `/vision` (OpenAI fallback)
**REMOVIDO:** bloco `callOpenAIVision(openaiKey, ...)` (gpt-4o pago).

```diff
-	// 3. OpenAI fallback
+	// 3. OpenAI fallback — REMOVIDO 04/09 (política estrita free-only)
+	// O fallback usava "gpt-4o-mini" (PAGO via OpenAI key). Sem tier free
+	// disponível — prefere-se falhar com erro a cobrar crédito.
 	if reply == "" {
-		openaiKey := req.OpenAIKey
-		if openaiKey == "" {
-			openaiKey = OAI_KEY
-		}
-		if openaiKey != "" {
-			var err error
-			reply, err = callOpenAIVision(openaiKey, req.ImageB64, req.MimeType, req.Prompt)
-			attempted = append(attempted, "gpt-4o")
-			if err != nil {
-				log.Printf("⚠ OpenAI Vision falhou: %v", err)
-				reply = ""
-			}
-		}
+		attempted = append(attempted, "gpt-4o-mini (desativado, política free-only)")
+		log.Printf("⚠ Vision: OpenAI fallback desativado (política free-only 04/09)")
 	}
```

### 2.5 `ai.go:309-356` — função `callOpenAIVision` desativada
**SUBSTITUÍDO:** corpo da função inteira por log + retorno de erro.

A função é mantida para não quebrar compilação dos call-sites (`smart_chat.go:178`), mas sempre retorna erro. Quando invocada, loga o motivo da desativação.

```diff
 func callOpenAIVision(openaiKey, imageB64, mimeType, prompt string) (string, error) {
-	if openaiKey == "" { openaiKey = OAI_KEY }
-	... (47 linhas de payload OpenAI + http.NewRequest + client.Do + decode) ...
-	return apiResp.Choices[0].Message.Content, nil
+	// FIX 04/09: política estrita free-only. O fallback usava "gpt-4o-mini"
+	// (PAGO via chave OpenAI). Sem tier free viável agora — prefere-se
+	// falhar com erro claro a cobrar crédito. Esta função é mantida
+	// apenas para não quebrar compilação dos call-sites (smart_chat.go);
+	// sempre retorna erro.
+	log.Printf("⚠ callOpenAIVision desativado (política free-only 04/09)")
+	return "", fmt.Errorf("OpenAI Vision desativado — política free-only (HOK 04/09). Use OpenRouter com modelos free (LLM-70B, Gemma, AIHubMix free)")
 }
```

## 3. Grep final (confirmação)

Comando: `grep -rEn "gemini-2\.5-flash-lite|gemini-2\.0-flash|gpt-4o|gpt-4o-mini|gemini-2\.5-flash[^:]" /root/hokma/backend/ --include="*.go"`

**Resultado: 14 ocorrências, todas INERTES (zero chamada real):**

| Arquivo:linha | Tipo | Conteúdo |
|---|---|---|
| `agent_loop.go:21` | comentário FIX | `FIX 04/09: removido "google/gemini-2.5-flash"` (FIX antigo, doc) |
| `ai.go:310` | comentário FIX | `política estrita free-only. O fallback usava "gpt-4o-mini"` (doc) |
| `ai.go:439` | comentário FIX | `permanece como ModelB (google/gemini-2.5-flash) por padrao` (doc antiga) |
| `ai.go:750` | comentário FIX | `removido "Gemini/Flash-Lite" (gemini-2.5-flash-lite, PAGO)` (doc) |
| `autopatch_loop.go:245` | comentário FIX | `FIX 04/09: removido "google/gemini-2.5-flash"` (FIX antigo, doc) |
| `globals.go:14` | comentário FIX | `FIX 01/09: ModelB era "google/gemini-2.5-flash"` (FIX antigo, doc) |
| `model_gate_test.go:57` | fixture de teste | `modelForOpenRouter` testa slug com `/` (não chama API) |
| `normalize_model_test.go:14` | fixture de teste | `normalizeModelSlugForAPI` testa slug (não chama API) |
| `opencode_client.go:127` | comentário FIX | `os fallbacks globais como "google/gemini-2.5-flash"` (doc) |
| `routes.go:221` | comentário FIX | `removido callGeminiText("gemini-2.5-flash-lite", PAGO)` (doc) |
| `routes.go:278` | comentário FIX | `gemini-2.0-flash-exp:free foi REMOVIDO do OpenRouter` (doc) |
| `routes.go:294` | comentário FIX | `O fallback usava "gemini-2.0-flash" (PAGO via GEMINI_KEY)` (doc) |
| `routes.go:300` | log diagnóstico | `"gemini-2.0-flash (desativado, política free-only)"` em `attempted` |
| `routes.go:308` | log diagnóstico | `"gpt-4o-mini (desativado, política free-only)"` em `attempted` |

**Zero chamada real a modelo pago. ✅**

## 4. Testes (build, vet, test)

### `go build -o hokma_test .`
✅ **PASS** — binário gerado (19.530.002 bytes, timestamp 20:35).

### `go vet ./...`
✅ **PASS** — sem warnings.

### `go test` (suíte focada em modelos/fallbacks/gates)

```
=== RUN   TestModelForOpenRouter
--- PASS: TestModelForOpenRouter (0.00s)
=== RUN   TestModelBlockReply
--- PASS: TestModelBlockReply (0.00s)
=== RUN   TestModelBlockIfExpired
--- PASS: TestModelBlockIfExpired (0.00s)
=== RUN   TestNormalizeModelSlugForAPI
--- PASS: TestNormalizeModelSlugForAPI (0.00s)
=== RUN   TestModelConstants
--- PASS: TestModelConstants (0.00s)
=== RUN   TestNormalizeQuotes
--- PASS: TestNormalizeQuotes (0.00s)
PASS
ok  	hokma_backend	0.012s
```

### `go test` (suíte completa)
✅ **PASS** — `ok hokma_backend 24.262s` (sem falhas)

## 5. Estado final

| Item | Estado |
|---|---|
| `ai.go.bak_20260904_203436` | ✅ Backup criado |
| `routes.go.bak_20260904_203436` | ✅ Backup criado |
| `ai.go` modificado | ✅ 2 mudanças (callOpenAIVision + callLLMWithFallback) |
| `routes.go` modificado | ✅ 2 mudanças (rota /smart + rota /vision) |
| `hokma_test` (binário isolado) | ✅ Compilado, 19.5 MB |
| Build/vet/test | ✅ Tudo PASS |
| `hokma.service` em produção | ⏸️ **NÃO reiniciado** — aguardando aprovação |
| Commit/push | ⏸️ **NÃO feito** — aguardando aprovação |

## 6. Aviso sobre o smart_chat.go (não tocado nesta sessão)

O `smart_chat.go` ainda referencia `callGeminiVision` (linhas 129, 166, 176) e `callOpenAIVision` (linha 178) para o caminho de visão do chat. Como:
- `callOpenAIVision` foi desativada (sempre retorna erro) — o caminho smart_chat também fica bloqueado para OpenAI Vision.
- `callGeminiVision` **continua funcional** porque não foi tocada (a remoção foi só nos fallbacks de `routes.go`). O `smart_chat.go:166` ainda chama Gemini Vision.

**Decisão pendente:** se quiser aplicar a política estrita também no `smart_chat.go`, é preciso:
- Substituir `callGeminiVision` em `smart_chat.go:129, 166, 176` por `callORVision(ModelB, ...)` (já testado em produção em 03/09).
- A linha 178 (`callOpenAIVision`) já está coberta pela desativação da função.

**Esta sessão NÃO tocou o `smart_chat.go`** — apenas os 4 pontos solicitados. Fica como follow-up se você quiser endurecer mais.

## 7. Próximo passo

Aguardando aprovação para:
1. **Restart do `hokma.service`** com o binário `hokma_test` (após validar mais).
2. **Commit/push** dos 2 arquivos modificados para `hok-backend-atual` (não feito nesta sessão).
3. **Possível extensão ao `smart_chat.go`** se quiser política estrita também no fluxo de voz+visão.

**Data/Hora:** 04/09/2026, ~20:36 UTC
**Status:** Correções aplicadas, testadas, com backups. **Sem restart, sem commit, sem push — aguardando aprovação.**
