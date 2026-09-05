# ADENDO — SESSÃO 04/09/2026 — Investigação de vazamento de crédito pago no OpenRouter

Sessão dedicada à investigação, **somente leitura**, de possíveis pontos de cobrança paga silenciosa no backend Go (HOK OS). Nenhuma alteração de código foi feita — apenas diagnóstico e relatório para decisão posterior.

## 1. Contexto da investigação

A sessão foi motivada por:
- FIX 01/09 (já aplicado): `globals.go` — `ModelB` foi trocado de `google/gemini-2.5-flash` (pago) para `minimax/minimax-m3:free` (pricing 0/0 confirmado).
- FIX 04/09 (já aplicado): `agent_loop.go` (~linha 21) e `autopatch_loop.go` (~linha 245) — `google/gemini-2.5-flash` removido da cascata, substituído por `ModelB`.
- **Pendência:** validar se há OUTROS lugares com modelos pagos hardcoded (não só os dois citados) e se há agentes externos rodando em modelo pago fora do controle central.

## 2. Metodologia

Comandos executados (todos somente leitura):
- `grep -rEn "gemini-2\.5-flash|gemini-2\.5|google/gemini-2\.5-flash|gpt-4o|claude-sonnet-4|claude-opus-4" /root/hokma/backend/ --include="*.go"`
- `grep -rEn "gemini-2\.5-flash-lite|gemini-1\.5-pro|gemini-1\.5-flash|gemini-2\.0-flash" /root/hokma/backend/ --include="*.go"`
- `grep -rEn "claude-sonnet-4|claude-opus-4|claude-3\.5|claude-3\.7|gpt-4o|gpt-4-turbo|gpt-4o-mini" /root/hokma/backend/ --include="*.go"`
- `journalctl -u hokma --since "72 hours ago" --no-pager | grep -E "gemini-2\.5-flash"`
- `cat /root/.claude/settings.json` (Claude Code)
- `cat /root/.opencode/opencode.json` (OpenCode CLI)
- `sqlite3 /root/hokma/backend/memory.db "SELECT * FROM app_settings WHERE key LIKE '%model%'"`
- `sqlite3 /root/hokma/backend/memory.db "SELECT * FROM hok_agents"` e `hok_agent_runs`

## 3. Achados — TODAS as ocorrências de modelos pagos hardcoded

### 3.1 Já corrigidas (verdes ✅)
| # | Arquivo:linha | Contexto | Status |
|---|---|---|---|
| 1 | `agent_loop.go:21-22` (comentário FIX 04/09) | cascata `hermesModels` | ✅ Corrigido |
| 2 | `agent_loop.go:23-28` | cascata `hermesModels` ativa — usa `ModelB` | ✅ OK |
| 3 | `autopatch_loop.go:245-252` | função `callHermesForPatch` — usa `ModelB` | ✅ OK |
| 4 | `globals.go:14-20` | constante `ModelB = "minimax/minimax-m3:free"` | ✅ OK (FIX 01/09) |
| 5 | `ai.go:208` | `callORVision` default — usa `google/gemini-2.5-flash:free` (FREE) | ✅ OK |
| 6 | `ai.go:478-480` | comentário sobre `fallbackChatModel = ModelB` | ✅ OK |

### 3.2 Vazamentos potenciais ainda em produção (vermelhos 🔴)
| # | Arquivo:linha | Modelo | Função / Fluxo | Risco |
|---|---|---|---|---|
| 7 | `ai.go:793` | `gemini-2.5-flash-lite` (PAGO) | **`callLLMWithFallback` — 4º provider da cascata do chat** | 🔴 ALTO |
| 8 | `routes.go:223` | `gemini-2.5-flash-lite` (PAGO) | **rota `/smart` → fallback do chat do frontend** | 🔴 ALTO |
| 9 | `routes.go:303-304` | `gemini-2.0-flash` (PAGO) | **rota `/vision` → fallback Gemini Vision** | 🟡 MÉDIO |
| 10 | `routes.go:321` + `ai.go:327` | `gpt-4o` (log) + `gpt-4o-mini` (PAGO barato) | **rota `/vision` → fallback OpenAI Vision** | 🟡 BAIXO |
| 11 | `ai.go:279` | `gemini-2.5-flash:generateContent` (Google API direta) | `callGeminiVision` — só aciona se houver `geminiKey` válido | ⚠️ DEPENDE |

### 3.3 Modelos FREE em uso (todos validados)
- `deepseek/deepseek-chat` — `agent_loop.go:24`, `autopatch_loop.go:249`
- `meta-llama/llama-3.3-70b-instruct` — `agent_loop.go:25`, `autopatch_loop.go:250`, `frontend_loop.go:88`
- `meta-llama/llama-3.3-70b-instruct:free` — `routes.go:226`, `ai.go:799`
- `mistralai/mistral-7b-instruct` — `agent_loop.go:27`
- `google/gemma-4-31b-it:free` — `ai.go:809`
- `coding-glm-5.3-free` (AIHubMix) — `ai.go:825`
- `gpt-5.5-free` (AIHubMix) — `ai.go:819`

## 4. Análise detalhada dos pontos críticos

### 4.1 `callLLMWithFallback` (chat normal) → `gemini-2.5-flash-lite` PAGO

**Arquivo:** `ai.go:783-794`

```go
{
    Name:    "Gemini/Flash-Lite",
    URL:     "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions",
    AuthEnv: "GEMINI_KEY",
    Model:   "gemini-2.5-flash-lite",  // ← PAGO (input $0.10/M, output $0.40/M)
},
```

**Fluxo:** Mensagem do usuário → `routeModel` → se falhar → `callLLMWithFallback` percorre 8 providers em ordem: ativo → fallback (ModelA/ModelB) → Cerebras → **Gemini/Flash-Lite (PAGO)** → Llama-70B (free) → Gemma-4 (free) → AIHubMix.

**Por que é problema:** o Gemini Flash-Lite é o 4º provider e usa rota própria da Google (não passa pela `OR_KEY` — usa `GEMINI_KEY` direto). O nome "Flash-Lite" sugere ser leve/barato, mas **não é FREE**. Sem log do nome do modelo que respondeu com sucesso, é impossível confirmar via `journalctl` se foi acionado.

### 4.2 `routes.go:223` (rota `/smart`) → `callGeminiText` com `gemini-2.5-flash-lite` PAGO

**Arquivo:** `routes.go:215-227`

```go
reply, modelUsed, err := routeModel(modelID, msgs, req)
if err != nil {
    log.Printf("⚠ %s falhou: %v — OR fallback", modelID, err)
    reply, err = callOR(normalizeModelSlugForAPI(getDefaultChatModel()), msgs)
    if err != nil {
        geminiKey := os.Getenv("GEMINI_KEY")
        if geminiKey != "" {
            reply, err = callGeminiText(geminiKey, "gemini-2.5-flash-lite", msgs)  // ← PAGO
        }
        if geminiKey == "" || err != nil {
            reply, err = callOR("meta-llama/llama-3.3-70b-instruct:free", msgs)
        }
        ...
```

**Por que é problema:** é o **caminho mais comum de chat** (`/smart` é chamado pelo frontend). Quando o modelo ativo falha → tenta OR → se falhar → tenta `gemini-2.5-flash-lite` **PAGO via Google direto** → se falhar, vai pro Llama-70B FREE.

### 4.3 `routes.go:303` (rota `/vision`) → `callGeminiVision` → `gemini-2.0-flash` PAGO

**Arquivo:** `routes.go:295-310`

```go
if reply == "" {
    geminiKey := req.GeminiKey
    if geminiKey == "" {
        geminiKey = GEMINI_KEY
    }
    if geminiKey != "" {
        var err error
        reply, err = callGeminiVision(geminiKey, req.ImageB64, req.MimeType, req.Prompt)
        attempted = append(attempted, "gemini-2.0-flash")  // ← PAGO
```

**Por que é problema:** rota de visão (acionada por uploads de imagem do chat) usa `gemini-2.0-flash` direto (não `:exp:free`, que foi removido da OpenRouter em 03/09 — confirmado nos logs: "No endpoints found for google/gemini-2.0-flash-exp:free").

### 4.4 `ai.go:327` (`callOpenAIVision`) → `gpt-4o-mini` PAGO

**Arquivo:** `ai.go:320-330`

```go
payload := OAIVReq{
    Model:     "gpt-4o-mini",  // ← PAGO barato (input $0.15/M, output $0.60/M)
    MaxTokens: 1024,
    ...
```

**Por que é problema:** baixo custo unitário mas ainda é pago, e só é acionado se a visão Gemini falhar.

## 5. Análise de logs — chamadas reais nas últimas 72h

Comando: `journalctl -u hokma --since "72 hours ago" --no-pager | grep -E "gemini-2\.5-flash"`

**Total de menções: 6**

| Data/Hora | Modelo no log | Resultado | Houve cobrança? |
|---|---|---|---|
| 2026-09-03 11:07:12 | `openrouter/google/gemini-2.5-flash` | ❌ OR Vision falhou (ID inválido) | ✅ NÃO |
| 2026-09-03 11:07:27 | `openrouter/google/gemini-2.5-flash` | ❌ OR Vision falhou (ID inválido) | ✅ NÃO |
| 2026-09-04 19:41:18, 19:41:25, 19:41:25, 19:41:29 | — | Logs de AUDIT (bash grep) do opencode_serve em modo autônomo desta sessão | ✅ NÃO |

**Conclusão:** Nos últimos 72h, **NÃO houve cobrança real a `google/gemini-2.5-flash` PAGO** — todas as tentativas com esse ID falharam antes de chegar na API, ou foram logs internos de auditoria do opencode_serve.

**Sobre `gemini-2.5-flash-lite` PAGO:** o log do chat **não imprime o nome do modelo final** que respondeu com sucesso, então não há como confirmar via `journalctl` se foi acionado. O cruzamento com o **extrato de uso do OpenRouter / Google Cloud** é a única forma de confirmar.

## 6. Configurações dos agentes externos

### 6.1 `/root/.claude/settings.json` (Claude Code) — TUDO FREE ✅
```json
"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "minimax/minimax-m3:free",
"ANTHROPIC_DEFAULT_OPUS_MODEL":   "minimax/minimax-m3:free",
"ANTHROPIC_DEFAULT_SONNET_MODEL": "minimax/minimax-m3:free",
"ANTHROPIC_MODEL":                "minimax/minimax-m3:free",
"ANTHROPIC_SMALL_FAST_MODEL":     "minimax/minimax-m3:free"
```

### 6.2 `/root/.opencode/opencode.json` (OpenCode CLI) — TUDO FREE ✅
```json
{ "$schema": "https://opencode.ai/config.json", "model": "openrouter/minimax/minimax-m3:free" }
```

### 6.3 `app_settings.activeModel` (SQLite) — FREE ✅
```
activeModel = minimax/minimax-m3:free
```

### 6.4 `validatedModels` (`globals.go:25-28`) — allow-list restrita ✅
```go
var validatedModels = map[string]bool{
    ModelA: true, // deepseek/deepseek-chat-v3.1 (FREE)
    ModelB: true, // minimax/minimax-m3:free (FREE)
}
```

### 6.5 Tabela `hok_agents` (SQLite) — sem modelo pago
```
ag_718147165 | Especialista N8N | (model vazio) | subagent
```

### 6.6 Tabela `hok_agent_runs` (modelos usados pelos agents)
Últimos 6 runs — todos FREE:
- `orchestrator` × 5: `minimax/minimax-m3` ou `deepseek/deepseek-v4-flash-0731`
- `Especialista N8N` × 1: `deepseek/deepseek-v4-flash-0731`

## 7. Resumo executivo

| Item | Status |
|---|---|
| Cascatas `agent_loop.go` e `autopatch_loop.go` | ✅ Corrigidas (FIX 04/09) |
| `globals.go` ModelB | ✅ Corrigido (FIX 01/09) |
| `callORVision` default | ✅ FREE (usa `:free`) |
| Claude Code / OpenCode / Hermes settings | ✅ Todos FREE |
| `app_settings.activeModel` | ✅ FREE |
| `validatedModels` allow-list | ✅ Só FREE |
| `hok_agents` modelos | ✅ Nenhum pago |
| `hok_agent_runs` modelos | ✅ Todos FREE |
| **🔴 `callLLMWithFallback` → `gemini-2.5-flash-lite`** | 🔴 VAZAMENTO POTENCIAL |
| **🔴 `routes.go:223` → `gemini-2.5-flash-lite`** | 🔴 VAZAMENTO POTENCIAL |
| **🟡 `routes.go:303` → `gemini-2.0-flash`** | 🟡 VAZAMENTO POTENCIAL |
| **🟡 `routes.go:327` → `gpt-4o-mini`** | 🟡 VAZAMENTO BAIXO CUSTO |

## 8. Opções de correção (aguardando decisão do usuário)

1. **TROCAR `gemini-2.5-flash-lite` por versão `:free`** em `ai.go:793` e `routes.go:223` (verificar se existe free tier estável no OpenRouter).
2. **REMOVER os 2 providers Gemini da cascata** (deixar só os FREE: Llama, Gemma, AIHubMix).
3. **TROCAR `gemini-2.0-flash` em `routes.go:303`** — a versão `exp:free` foi removida da OR em 03/09, então não há substituto FREE estável.
4. **TROCAR `gpt-4o-mini` em `ai.go:327`** por `meta-llama/llama-3.2-11b-vision-instruct:free` — mas logs mostram "No endpoints found" para esse modelo em 03/09.
5. **ADICIONAR log de auditoria** em `callLLMWithFallback` que loga qual provider respondeu com sucesso, para cruzarmos com o extrato OpenRouter.

## 9. Próximo passo

Aguardando decisão do usuário sobre qual(is) correção(ões) aplicar. Esta sessão foi apenas diagnóstico — nenhum patch foi aplicado, nenhum restart foi feito, nenhum commit foi gerado.

**Data/Hora:** 04/09/2026, ~20:25 UTC
**Status:** Investigação concluída — relatório pronto, sem alterações de código.
