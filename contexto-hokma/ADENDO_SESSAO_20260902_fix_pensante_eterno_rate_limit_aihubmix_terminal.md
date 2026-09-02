# ADENDO — SESSÃO 02/09 (rodada 2) — Chat preso em "pensando" + rate-limit 429 + erro aihubmix na TUI do terminal

Sessão dedicada a três bugs correlatos: a bolha "pensando" eterna do chat web,
o tratamento do 429/rate-limit, e o "API Error: 400 not a valid model ID"
persistente na TUI do opencode dentro do terminal HOK.

## 1. Chat preso em "pensando" (bolha eterna) — ROOT CAUSE + FIX

**Problema:** ao testar `z-ai/glm-5.2:free` (rate-limited upstream na
OpenRouter), o chat web ficava na bolha "pensando" por 5+ minutos, às vezes
para sempre.

**Cadeia de falha (log 12:50):**
1. Chat web → opencode serve (canal principal, vem ANTES do claude_code na
   cascata — smart_chat.go).
2. O serve usa o modelo ativo `openrouter/z-ai/glm-5.2:free`; com 429 o POST
   `/message` só estourava após **320s** (`opencodeServeHTTPTimeout`).
3. Só então a cascata seguia: OpenCode(plan) falha → routeModel 429 → pool →
   `minimax-m3:free` responde (5s).
4. Durante ~320s+ o job ficava `running` → frontend mostrava "pensando".

**Correções:**
- **Backend `opencode_serve_client.go`**: novos `doCtx`/`sendMessageCtx` com
  contexto.
- **Backend `opencode_serve_flow.go`**: mensagens simples do serve agora com
  `openCodeServeSyncTimeout=60s` (antes 320s) → fail-fast; cascata segue.
- **Frontend `ChatScreen.tsx`** (send + resumeRunningJob): deadline de 10min
  no polling + job 404 (backend reiniciou) → sai do loop com erro claro, em
  vez de `continue` infinito (bolha eterna).

## 2. Rate-limit (429) classificado — sem quebrar o pool fallback

- **Backend `model_gate.go`**: novo `modelStatusRateLimited`; `classifyModelStatus`
  agora detecta 429/rate-limit/too many requests com mensagem clara
  ("aguarde e tente de novo, ou escolha outro modelo"). Novo helper
  `classifyPermanentModelStatus` (exclui 429) para o routeModel continuar
  caindo no pool em cascata (fallback minimax funciona).
- **Backend `ai.go`**: 4 call sites do routeModel usam
  `classifyPermanentModelStatus` (402/404/410/400 → trava; 429 → pool).
- **Backend `model_gate.go`**: `modelBlockIfExpired` idem.
- Testes: `TestClassifyModelStatus`/`TestClassifyPermanentModelStatus`/
  `TestModelBlockReply` atualizados.

## 3. Erro persistente "API Error: 400 not a valid model ID" na TUI do terminal

**Problema:** a TUI do opencode dentro do terminal HOK ficava em retry
infinito com `aihubmix/coding-glm-5.3-free is not a valid model ID`
(332 ocorrências + 198 de "API Error: 400" no log de captura do terminal).

**Investigação:**
- O SDK `@aihubmix/ai-sdk-provider` JÁ está instalado (opencode baixa
  automaticamente em `~/.cache/opencode/packages/`).
- A API aihubmix aceita `coding-glm-5.3-free` (respondeu), mas o opencode só
  roteia modelos que existem no `models.json` local (77 modelos do provider
  aihubmix). `coding-glm-5.3-free` NÃO existe lá → 400.
- `aihubmix/coding-glm-5.1-free` (existe no models.json) → FUNCIONA.

**Correção (`models_catalog.go`):**
- Nova função `opencodeAIHubMixKnownModelIDs()` lê o `models.json` do opencode
  e devolve o set de model IDs do provider aihubmix.
- `fetchAIHubMixModels()` agora só expõe modelos aihubmix que o opencode
  roteia — removeu ~333 IDs quebrados (incl. coding-glm-5.3-free).
- Catálogo caiu de 934 → 610 modelos (71 aihubmix roteáveis). Valores
  confirmados em produção via `/models/catalog?force=1`.

## Testes / validação
- `go test ./...` 100% verde (199s e 210s nas duas rodadas).
- Modelos free funcionando (re-testados 02/09): `opencode-go/deepseek-v4-flash`,
  `opencode/ling-3.0-flash-fin-free`, `opencode/mimo-v2.5-free`,
  `openrouter/minimax/minimax-m3:free` (5.7s),
  `openrouter/nvidia/nemotron-3-super-120b-a12b:free`.
- `openrouter/z-ai/glm-5.2:free` → rate-limited upstream na OpenRouter (Decart),
  temporário; fallback via pool (minimax) responde.
- AIHubMix free: cota de 10 tentativas esgotada (conta sem saldo) — responde
  com mensagem útil, não erro de roteamento.

## Arquivos alterados
- Backend: `model_gate.go`, `ai.go`, `opencode_serve_client.go`,
  `opencode_serve_flow.go`, `models_catalog.go`.
- Frontend: `src/components/screens/ChatScreen.tsx`.
- Deploy: backend `hokma` (backup `hokma.bak_20260902_*_aihubmix_filter` +
  `*_pensante_fix`); frontend `/var/www/hok-os` (backup `.bak_*_pensante_fix`).

## Pendências
- Commit/push: backend branch `hok-backend-atual` (models_catalog.go, model_gate.go,
  ai.go, opencode_serve_client.go, opencode_serve_flow.go) e frontend branch
  `main` (ChatScreen.tsx).
- Conta AIHubMix sem saldo → free limitado a 10 tentativas (sem recarga).
- `openrouter/z-ai/glm-5.2:free` ainda em rate-limit upstream — re-testar depois.