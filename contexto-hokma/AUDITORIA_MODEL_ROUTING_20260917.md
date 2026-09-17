# Auditoria Model Routing — Chat/HOK OS
Data: 17/09/2026 | Backend: hokma v25 | opencode-serve: ativo (PID 1713825, porta 4100)

## Como os Modelos São Carregados

O catálogo de modelos do HOK vem de 4 fontes:
1. **OpenCode Zen** (`fetchOpenCodeZenModels`): `opencode.ai/zen/v1/models` + local `~/.cache/opencode/models.json` para pricing. File `/root/.cache/opencode/models.json` **NÃO EXISTE** → usa fallback API-only (marcador `-free` no ID).
2. **OpenCode Go** (`fetchOpenCodeGoModels`): `opencode.ai/zen/go/v1/models` + local models.json. File **NÃO EXISTE** → fallback API-only.
3. **OpenRouter** (`fetchOpenRouterModels`): `openrouter.ai/api/v1/models` com OR_KEY — pricing real da API.
4. **Hardcoded** (`nativeDeepSeekModels`): `deepseek-native/deepseek-flash` e `deepseek-native/deepseek-v4-pro` — injetados sempre no catálogo.
5. **Frontend** (`refreshOpenCodeModels` → `opencode models openrouter` CLI): lista OpenRouter models que o CLI conhece, categorizados por `categorizeModel()`.

---

## CATEGORIA 1: Free OpenCode Models

Modelos servidos via `opencode/` ou `opencode-go/` prefix (OpenCode CLI como engine).

### Modelos Hardcoded (constantes do sistema)

| Modelo | Constante | Definição | Cargo na Cascade | Gratuito? | Observação |
|--------|-----------|-----------|------------------|-----------|------------|
| `deepseek/deepseek-chat-v3.1` | `ModelA` | `globals.go:26` | Antigo default (ativado via `getActiveModel()`), ainda usado em hermes_client.go:45 (HERMES_MODEL_A fallback), task_agent.go:229, crm_ai.go:117 | ❌ **PAIDO** no OpenRouter | globals.go:12 comentário diz "gratuito/zen" — **ERRADO**. `isFreeModel()` (ai.go:458-460) corretamente o exclui, mas task_agent.go e crm_ai.go NÃO verificam. |
| `nvidia/nemotron-3-super-120b-a12b:free` | `ModelB` | `globals.go:27` | Default atual (`activeModel` ai.go:488), fallback em callOpenCode:176, callHermes hermesModelB:47, callClaudeCode, agentEffectiveModel:213, agent_loop_groq:716 | ✅ Free verificado | Comentários em globals.go e ai.go corretos. |
| `nvidia/nemotron-3.5-lightning:free` | `ModelC` | `globals.go:28` | Primary fallback em callLLMWithFallback, fallbackChain orquestrador (agent_orchestrator.go:469), fallback em routes.go:234, defaultHermesModel (agent_loop.go:36) | ✅ Free verificado | Comentário globals.go:28 diz "Ling variants timeout" mas modelo é NVIDIA Nemotron — **inconsistência de naming**. |

### Modelos Nativos DeepSeek (rota direta api.deepseek.com)

| Modelo | Definição | Cargo na Cascade | Gratuito? | Observação |
|--------|-----------|------------------|-----------|------------|
| `deepseek-native/deepseek-flash` | `models_catalog.go:965` (nativeDeepSeekModels) | Rota direta via DS_URL + DEEPSEEK_API_KEY | ❌ **PAIDO** | Free=false no catálogo. Acesso via DEEPSEEK_API_KEY. Franquia 1M tok/24h. |
| `deepseek-native/deepseek-v4-pro` | `models_catalog.go:974` (nativeDeepSeekModels) | Rota direta via DS_URL + DEEPSEEK_API_KEY | ❌ **PAIDO** | Free=false no catálogo. |

### Modelos do Seletor Frontend (via `opencode models openrouter` CLI)

A função `categorizeModel()` (`models_routes.go:25-48`) classifica os modelos retornados pelo CLI `opencode models openrouter`. **Não é possível listar os modelos exatos sem executar o CLI** (o models.json local não existe e o CLI precisa estar instalado). A lógica de categorização é:

- `deepseek/deepseek-chat` → provider="OpenCode Zen", free=true
- `deepseek/deepseek-r1`, `deepseek/deepseek-v4*` → provider="OpenRouter", free=true ⚠️ ASSUME GRATUITO — pode ser pago
- `google/gemini*` → provider="Google", free=false
- `anthropic/claude*` → provider="OpenRouter", free=false
- `openai/gpt*` → provider="OpenAI", free=false
- `meta-llama*` → provider="OpenRouter", free=false
- `cohere/*`, `mistral/*`, `qwen/*` → provider="OpenRouter", free=false
- Tudo o mais → provider="OpenRouter", free=false **(default = PAGO)**

---

## CATEGORIA 2: Free OpenRouter Models

Modelos chamados via `https://openrouter.ai/api/v1/chat/completions` com OR_KEY.

### Cascade Principal (callLLMWithFallback — `ai.go:826-896`)

| # | Modelo | Constant/Variável | Localização | Gratuito? | Observação |
|---|--------|-------------------|-------------|-----------|------------|
| 1 | `nvidia/nemotron-3-super-120b-a12b:free` | `activeModel` / `ModelB` | `ai.go:488`, `ai.go:483` (fallbackChatModel) | ✅ Free | Ativo = primário da cascade |
| 2 | `nvidia/nemotron-3.5-lightning:free` | `ModelC` | `globals.go:28` | ✅ Free | Fallback se ModelB falhar |
| 3 | `google/gemma-4-31b-it:free` | — | `ai.go:842` | ✅ Free | Fallback se ModelC ativo |
| 4 | `gpt-5.5-free` | — | `ai.go:845` (AIHUBMIX_URL) | ⚠️ **Incerto** | ID com "-free" mas AIHUBMIX pode ter custos reais |
| 5 | `coding-glm-5.3-free` | — | `ai.go:848` (AIHUBMIX_URL) | ⚠️ **Incerto** | Idem — sufixo "-free" não garante gratuidade real |
| 6 | `cerebras/gpt-oss-120b` | — | `ai.go:835` (CEREBRAS_URL) | ❌ **PAIDO** | Sem sufixo "-free", sem verificação de gratuidade na cascade |

### Hermes Models Cascade (agent_loop.go:28-34)

| Modelo | Posição | Gratuito? | Observação |
|--------|---------|-----------|------------|
| `deepseek/deepseek-chat` | hermesModels[0] | ⚠️ **Ambíguo** | É o modelo DeepSeek original (sem -v3.1) — via GROQ_URL/GROQ_KEY, NÃO é OpenRouter |
| `nvidia/nemotron-3.5-lightning:free` | hermesModels[1] | ✅ Free | = ModelC |
| `nvidia/nemotron-3-super-120b-a12b:free` | hermesModels[2] | ✅ Free | = ModelB |
| `inclusionai/ling-3.0-flash-vl:free` | hermesModels[3] | ⚠️ **Free?** | Modelo VL (vision-language) — gratuidade não confirmada em produção |
| `mistralai/mistral-7b-instruct` | hermesModels[4] | ❌ **PAIDO** | Sem sufixo ":free" — bloqueado por isFreeModel() mas LISTADO como fallback |

### Orquestrador (agent_orchestrator.go:209-220, 464-482)

| Modelo | Variável | Gratuito? | Observação |
|--------|----------|-----------|------------|
| ModelA/ModelB/ModelC (variável) | `agentEffectiveModel` | Depende | Retorna modelo do agente ou activeModel ou ModelB |
| `nvidia/nemotron-3.5-lightning:free` | `fallbackChain[0]` | ✅ Free | Fallback explícito do orquestrador (agent_orchestrator.go:469) |
| `nvidia/nemotron-3-super-120b-a12b:free` | `fallbackChain[1]` | ✅ Free | Se modelo != ModelC |

### Subagents / Groq (agent_loop_groq.go:706-719)

| Modelo | Origem | Gratuito? | Observação |
|--------|--------|-----------|------------|
| `MINIMAX_AGENT_MODEL` (env) | — | Depende | Se vazio, usa getActiveModel() (ModelB agora) |
| ModelB `nvidia/nemotron-3-super-120b-a12b:free` | `fallbackAgentModel` | ✅ Free | Fallback final dos subagents |

### Allowed Paid (exceção explícita — ai.go:470-472)

| Modelo | Gratuito? | Observação |
|--------|-----------|------------|
| `deepseek/deepseek-v4.1-flash` | ❌ **PAIDO** | Está em `allowedPaidModels` — uso direto sem fallback free. Aprovado explicitamente pelo usuário. |

---

## CATEGORIA 3: DeepSeek Oficiais Pagos

Modelos que acessam `https://api.deepseek.com/v1/chat/completions` com DEEPSEEK_API_KEY.

| Modelo | Definição | Rota | Gratuito? | Observação |
|--------|-----------|------|-----------|------------|
| `deepseek-native/deepseek-flash` | `models_catalog.go:965` | `routeModel`: deepseek-native/* prefix → DS_URL (ai.go:580-603) | ❌ PAGO | Franquia 1M tok/24h via cache hit nativo |
| `deepseek-native/deepseek-v4-pro` | `models_catalog.go:974` | `routeModel`: deepseek-native/* prefix → DS_URL | ❌ PAGO | Idem |
| `deepseek/deepseek-chat-v3.1` | `ModelA` globals.go:26 | Via OpenRouter (OR_KEY), NÃO nativo | ❌ PAGO | Cai na cascata free se selecionado (routeModel:609) |
| `deepseek/deepseek-v4.1-flash` | `allowedPaidModels` ai.go:471 | Via OpenRouter (OR_KEY) | ❌ PAGO | Exceção — uso direto sem fallback |

### DeepSeek via Groq (NÃO é DeepSeek API)

| Modelo | Localização | Observação |
|--------|-------------|------------|
| `deepseek/deepseek-chat` | `agent_loop.go:29` (hermesModels[0]) | Chamado via GROQ_URL/GROQ_KEY — Groq serve DeepSeek models, mas NÃO é a API oficial da DeepSeek |
| `deepseek/deepseek-r1` | `models_routes.go:29` (categorizeModel) | Classificado como free=true, via OpenRouter |

---

## OPENCODE ↔ OPENROUTER DUPLICATAÇÕES

O modelo `deepseek/deepseek-chat-v3.1` aparece DUAS VEZES no sistema com rotas DIFERENTES:
1. Como **ModelA** — via OpenRouter (OR_KEY) → PAGO
2. Como **`deepseek/deepseek-chat`** (sem -v3.1) — via Groq (GROQ_KEY) → gratuito no Groq

São modelos **DIFERENTES** (versões distintas) mas o código trata ambos como "ModelA" em alguns contextos (hermes_client.go, task_agent.go), criando confusão.

O prefixo `deepseek/` aparece em 4 rotas distintas:
- OpenRouter (OR_KEY): `deepseek/deepseek-chat-v3.1`, `deepseek/deepseek-v4.1-flash`, `deepseek/deepseek-r1`, `deepseek/deepseek-v4`
- DeepSeek API nativa (DS_URL): `deepseek-native/deepseek-flash`, `deepseek-native/deepseek-v4-pro`
- Groq (GROQ_KEY): `deepseek/deepseek-chat`, `deepseek/deepseek-r1`
- Hermes native (hermes_client.go): `deepseek/deepseek-chat` (sem -v3.1)

---

## PROBLEMAS ENCONTRADOS

### 🔴 CRÍTICOS

**1. ModelA falsamente rotulado como FREE**
- `globals.go:12` comentário: "gratuito/zen (DeepSeek chat v3.1 via OpenRouter)" — **ERRADO**, é pago
- `globals.go:13` comentário sobre ModelB: "fallback gratuito" — correto
- **Usado em**: `task_agent.go:229` (sem verificação de gratuidade), `crm_ai.go:117` (verifica isFreeModel mas retorna ModelA como default se não configurado), `hermes_client.go:45` (HERMES_MODEL_A env default)
- **Fix**: Corrigir globals.go:12 comentário e garantir que task_agent.go e crm_ai.go NÃO usem ModelA sem verificação prévia

**2. `deepseek/deepseek-chat` ≠ `deepseek/deepseek-chat-v3.1` — models confundidos**
- `hermesModels[0]` = `deepseek/deepseek-chat` (agent_loop.go:29) — versão antiga, via Groq
- `ModelA` = `deepseek/deepseek-chat-v3.1` (globals.go:26) — versão v3.1, via OpenRouter
- Ambos chamados de "ModelA" em alguns contextos (hermes_client.go) mas são modelos DIFERENTES com rotas DIFERENTES

**3. `mistralai/mistral-7b-instruct` em hermesModels é PAID mas listado como fallback**
- `agent_loop.go:34` — sem sufixo `:free`, será bloqueado por `isFreeModel()` mas aparece na lista de modelos disponíveis

### 🟡 IMPORTANTES

**4. ModelB usado como fallback em 6+ locais — single point of failure**
- callLLMWithFallback (via `fallbackChatModel`), callOpenCode:176, callHermes (hermesModelB:47), callClaudeCode, agentEffectiveModel (agent_orchestrator.go:213), agent_loop_groq:716 (`fallbackAgentModel`)
- Se ModelB falhar, TODOS os sistemas caem simultaneamente em ModelC

**5. ModelB NÃO está em `validatedModels`**
- `globals.go:33-36`: apenas ModelA (true) e ModelC (true). ModelB é o modelo ATIVO mas não está validado.
- Impacto: frontend mostra ModelB como "compatível=null" (não validado) apesar de ser o padrão

**6. `cerebras/gpt-oss-120b` na cascade callLLMWithFallback é PAGO**
- `ai.go:835` — sem sufixo `:free`, sem verificação de gratuidade. Cascade é descrita como "free-only" (agent_orchestrator.go:464-468) mas inclui modelo pago.

**7. AIHubMix models `:free` — gratuidade não confirmada**
- `gpt-5.5-free`, `coding-glm-5.3-free` (ai.go:845, 848) — sufixo "-free" mas AIHUBMIX tem pricing real por modelo. Não há verificação de custo real no código.

**8. `deepseek/deepseek-r1` e `deepseek/deepseek-v4*` categorizados como FREE em `categorizeModel`**
- `models_routes.go:29-30` — assume que todo modelo com prefixo `deepseek/` é free. DeepSeek v4 e R1 podem ser PAGOS no OpenRouter.

**9. `inclusionai/ling-3.0-flash-vl:free` — modelo VL usado como fallback geral**
- `agent_loop.go:32` — vision-language model listado como fallback para chat geral. Pode ser overkill.

**10. Inconsistência de naming do ModelC**
- `globals.go:28` comentário: "FIX: Ling variants timeout with complex prompts" (refere a Alibaba Ling)
- `globals.go:28` valor: `nvidia/nemotron-3.5-lightning:free` (modelo NVIDIA)
- Nome e comentário referem a empresas diferentes — confuso

### 🔵 INFORMACIONAIS

**11. `/root/.cache/opencode/models.json` NÃO EXISTE**
- O catálogo OpenCode Zen/Go usa fallback API-only (marcador `-free` no ID). Sem pricing real confirmado para modelos Zen/Go que não têm `-free` no ID.

**12. `validateModels` em globals.go desatualizada**
- ModelA marcado como `true` (validado) mas é pago — pode induzir frontend a exibir como "confirmado"
- ModelB NÃO está listado (o modelo ATIVO)

**13. `catalogRefreshPaused = true`** (models_catalog.go:991)
- Refresh automático do catálogo pausado (temporário). Catálogo pode estar desatualizado.

**14. `callDeepSeek` (ai.go:121) redireciona para callLLMWithFallback**
- Mantido para compatibilidade mas já não usa DeepSeek API diretamente — log diz "[ai] callDeepSeek → pool gratuito (DeepSeek desativado)"

---

## RESUMO DE USO POR MODELO

### `nvidia/nemotron-3-super-120b-a12b:free` (ModelB)
- **Localização**: globals.go:27
- **Status**: ✅ Free verificado
- **Cargos**: activeModel (default), isFreeModel exception, fallback em 6+ locais, hermesModelB fallback
- **Arquivos**: ai.go:488,483,446,460; agent_orchestrator.go:213; agent_loop_groq.go:716; hermes_client.go:47; opencode_client.go:176; globals.go:35 (validated? NÃO!)
- **Issue**: Não está em validatedModels apesar de ser o modelo ativo

### `nvidia/nemotron-3.5-lightning:free` (ModelC)
- **Localização**: globals.go:28
- **Status**: ✅ Free verificado
- **Cargos**: Primary fallback, defaultHermesModel, orquestrador fallbackChain, routes.go fallback
- **Arquivos**: ai.go:828; agent_orchestrator.go:469,475; agent_loop.go:36,29; routes.go:234; globals.go:36 (validated=true)

### `deepseek/deepseek-chat-v3.1` (ModelA)
- **Localização**: globals.go:26
- **Status**: ❌ PAGO no OpenRouter
- **Cargos**: Antigo default, ainda referenciado em task_agent.go, crm_ai.go, hermes_client.go env default, validatedModels
- **Arquivos**: task_agent.go:229,232; crm_ai.go:117; hermes_client.go:43,45; ai.go:444,446,458; globals.go:34 (validated=true — ERRADO para modelo pago)

### `deepseek-native/deepseek-flash`
- **Localização**: models_catalog.go:965
- **Status**: ❌ PAGO (DeepSeek oficial)
- **Cargos**: Rota nativa direta via DS_URL + DEEPSEEK_API_KEY
- **Arquivos**: models_catalog.go:962-983; ai.go:580-603 (routeModel)

### `deepseek-native/deepseek-v4-pro`
- **Localização**: models_catalog.go:974
- **Status**: ❌ PAGO (DeepSeek oficial)
- **Cargos**: Idem acima

### `deepseek/deepseek-chat` (sem -v3.1)
- **Localização**: agent_loop.go:29
- **Status**: ⚠️ Gratuito no Groq, PAGO no OpenRouter (é versão antiga)
- **Cargos**: hermesModels[0] via Groq, auto-debug utils.go:242, autopatch_loop.go:252
- **Arquivos**: agent_loop.go:29; utils.go:242; autopatch_loop.go:252; models_routes.go:27 (categorizeModel → OpenCode Zen/free)

### `google/gemma-4-31b-it:free`
- **Localização**: ai.go:842
- **Status**: ✅ Free (OpenRouter)
- **Cargos**: Fallback na cascade callLLMWithFallback

### `deepseek/deepseek-v4.1-flash`
- **Localização**: ai.go:471
- **Status**: ❌ PAGO (exceção explícita)
- **Cargos**: allowedPaidModels — uso direto sem fallback free
