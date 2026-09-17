# Adendo — Sessão Chat-Web-HOK / 36:56/17/09/2026

## Fix Crítico 1: ModelA PAGO — comentário + checagem isFreeModel

### Problema
- `globals.go:12` dizia "Modelo A: gratuito/zen" — **ERRADO**, ModelA é PAGO no OpenRouter
- `task_agent.go:229` usava `ModelA` diretamente sem verificar gratuidade
- `crm_ai.go:117` retornava `ModelA` como default sem verificação isFreeModel()

### Ações
1. **`globals.go:12`**: comentário corrigido `gratuito/zen` → `PAGO via OpenRouter`
2. **`task_agent.go:224-234`**:
   - Comentário corrigido (ModelA é PAGO, não FREE)
   - Adicionada checagem: `model := ModelA; if !isFreeModel(model) { model = ModelB }`
   - Payload usa `model` (variável checada) em vez de `ModelA` direto
3. **`crm_ai.go:112-120`**:
   - `return ModelA` → `if isFreeModel(ModelA) { return ModelA }; return ModelB`

### Validação
| Teste | Resultado |
|-------|-----------|
| `go build` | ✅ PASS |
| Modelo padrão ativo | ✅ `nvidia/nemotron-3-super-120b-a12b:free` (ModelB) |
| GET /models/available | ✅ active=ModelB, modelA=deepseek/deepseek-chat-v3.1 |
| GET /deepseek/credits | ✅ balance: $1.96 USD |

---

## Fix Crítico 2: remover mistralai/mistral-7b-instruct de hermesModels

### Problema
`agent_loop.go:34` — `mistralai/mistral-7b-instruct` listado em `hermesModels` como fallback gratuito, mas é PAGO (sem sufixo `:free`). `isFreeModel()` bloqueia mas o modelo continua visível na lista.

### Ação
- **`agent_loop.go:34`**: removida 1 linha da lista `hermesModels`
- Lista atualizada: `deepseek/deepseek-chat`, `ModelC`, `ModelB`, `inclusionai/ling-3.0-flash-vl:free`

### Validação
| Teste | Resultado |
|-------|-----------|
| `go build` | ✅ PASS |
| hermesModels | ✅ 4 modelos (todos free) |

---

## Fix Crítico 3: remover cerebras/gpt-oss-120b da cascade callLLMWithFallback

### Problema
`ai.go:854-859` — bloco `Cerebras/Llama-70B` (`gpt-oss-120b`, via `CEREBRAS_API_KEY`) incluído na cascade `callLLMWithFallback`, documentada como free-only. Modelo é PAGO.

### Ação
- **`ai.go:854-859`**: removido bloco inteiro (6 linhas)
- Cascade agora: HOK/Ativo → HOK/Fallback → OR/Nemotron-3-super-free → OR/Gemma-4-31B → AIHubMix/GPT-5.5-free → AIHubMix/GLM-5.3-Flash-free

### Validação
| Teste | Resultado |
|-------|-----------|
| `go build` | ✅ PASS |
| Cascade providers | ✅ 6 providers (sem Cerebras) |

---

## Observação sobre commit Fix 3

O primeiro commit do Fix 3 (`37a9bb0`) incluiu erroneamente mudanças pré-existentes do ai.go (Gemini Vision remoção, ModelA→ModelB migration, etc.) que já estavam no working tree mas não commitadas. Corrigido com:
1. `git reset --soft abd91da` — desfazer commit incorreto
2. Commit separado (`1dea94a`) para mudanças pré-existentes do ai.go
3. Re-aplicação isolada do Fix 3 (`f8caa6e`) — apenas 6 deleções

## Arquivos Modificados Nesta Sessão
- `/root/hokma/backend/globals.go` — Comentário ModelA corrigido
- `/root/hokma/backend/task_agent.go` — Checagem isFreeModel adicionada
- `/root/hokma/backend/crm_ai.go` — Checagem isFreeModel no getCRMModel
- `/root/hokma/backend/agent_loop.go` — mistralai/mistral-7b-instruct removido
- `/root/hokma/backend/ai.go` — Cerebras/gpt-oss-120b removido da cascade

## Commits
- `abd91da` — FIX CRÍTICO 1: ModelA PAGO
- `1e51f97` — FIX CRÍTICO 2: mistralai removido
- `1dea94a` — pre-session: mudanças ai.go (sessão anterior)
- `f8caa6e` — FIX CRÍTICO 3: cerebras removido

## Serviços
- `hokma.service :8082` — REINICIADO ✅
- `opencode-serve :4100` — ativo (não reiniciado, já estava ativo)
