# ADENDO_SESSAO_20260910_ClaudeCode_Anthropic_Nativo.md

## Resumo

Correção do Claude Code para usar os modelos DeepSeek NATIVOS (`deepseek-native/*`). Diferente do opencode, o Claude Code não tem provider customizado — ele só troca de backend por env vars. O propagate gravava o ID `deepseek-native/*` em `ANTHROPIC_MODEL` mantendo `ANTHROPIC_BASE_URL=https://openrouter.ai/api`, o que gerava `400 ... is not a valid model ID`. Agora, ao selecionar um modelo nativo, o propagate troca `ANTHROPIC_BASE_URL` → rota `/anthropic` da DeepSeek + `ANTHROPIC_AUTH_TOKEN` → `DEEPSEEK_API_KEY` + model ID aceito pela rota, preservando/restaurando as env vars OpenRouter originais via sidecar. Também corrigido o `opencodeModelID` que prefixava `openrouter/` indevidamente.

## Investigação

### O que o propagate gravava (bug)
`writeClaudeSettings()` gravava apenas os 5 campos de modelo com `normalizeModelSlugForAPI(model)`. Como `deepseek-native/deepseek-flash` contém `/`, o normalize é no-op → gravava o ID literal, mantendo `ANTHROPIC_BASE_URL=https://openrouter.ai/api` e a chave OpenRouter. Resultado: OpenRouter rejeita o ID nativo (`400`).

### Rota /anthropic da DeepSeek (confirmada)
- `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic` (rota compatível com protocolo Anthropic na própria infra DeepSeek — não é proxy terceiro)
- `ANTHROPIC_AUTH_TOKEN=<DEEPSEEK_API_KEY>`
- Model IDs aceitos: `deepseek-flash` e `deepseek-v4-pro` (ambos testados)
- Teste real do Claude Code CLI com config temporária (`CLAUDE_CONFIG_DIR`): respondeu via /anthropic.

### Cache hit na rota /anthropic — CONFIRMADO (fecha pendência de 09/09)
O usage vem no formato Anthropic (`cache_read_input_tokens`, não `prompt_cache_hit_tokens`):
```
chamada 1: input_tokens=6835  cache_read=0
chamada 2: input_tokens=179   cache_read=6656   ← 6656 tokens de cache hit
```
A franquia de cache também funciona na rota /anthropic. **(83% do prompt em cache na 2ª chamada.)**

## Mudanças

### /root/hokma/backend/model_propagate.go
- Constantes: `deepseekNativePrefix`, `deepseekAnthropicBaseURL`, `anthropicOrigSuffix` (`.hok_anthropic_orig.json`).
- `writeClaudeSettings()` agora detecta modo nativo:
  - **nativo**: salva `{baseURL, authToken}` originais no sidecar (uma vez, 0600), seta `ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic`, `ANTHROPIC_AUTH_TOKEN=DEEPSEEK_API_KEY`, model = ID sem prefixo (`deepseek-flash`/`deepseek-v4-pro`). Se a key estiver ausente → loga e NÃO aplica (fail-safe).
  - **não-nativo**: restaura base/token do sidecar (se existir), remove o sidecar e aplica `normalizeModelSlugForAPI` (comportamento antigo).
- `propagateToClaudeSettings` não normaliza mais antes (normalização movida para dentro).
- Helper `envString`.

### /root/hokma/backend/opencode_client.go
- `opencodeModelID()` devolve IDs `deepseek-native/*` SEM prefixar `openrouter/` (corrige `~/.opencode/opencode.json`, que ficava `openrouter/deepseek-native/...`).

## Validação (2 cenários, em produção)

### Cenário 1 — selecionar nativo
`/root/.claude/settings.json` e `/home/hokma-agent/.claude/settings.json`:
```
ANTHROPIC_BASE_URL = https://api.deepseek.com/anthropic
ANTHROPIC_AUTH_TOKEN = sk-a6be... (DEEPSEEK_API_KEY)
ANTHROPIC_MODEL = deepseek-flash
sidecar criado com os originais OpenRouter (5f05... root / 2b1d... agent)
```
`claude --print "cenário 1 nativo OK"` → respondeu ✓

### Cenário 2 — voltar para OpenRouter
`/models/select deepseek/deepseek-v4-flash-0731`:
```
ANTHROPIC_BASE_URL = https://openrouter.ai/api        (restaurado)
ANTHROPIC_AUTH_TOKEN = sk-or-v1-5f05... / 2b1d...      (restaurado por arquivo)
ANTHROPIC_MODEL = deepseek/deepseek-v4-flash-0731
sidecar removido
~/.opencode/opencode.json = openrouter/deepseek/deepseek-v4-flash-0731
```
Depois voltei para `deepseek-native/deepseek-flash` (estado que estava sendo testado) e re-validou corretamente.

### Smoke pós-fix
`claude --print` com settings reais (nativo) → respondeu ✓
Cache hit /anthropic: `cache_read=6656` na 2ª chamada ✓

## Deploy / Commit / Push
- `./deploy.sh`: build `5399f0c8`, tests OK, health 200, hashes idênticos. No boot o propagate corrigiu automaticamente os settings corrompidos.
- Backend: commit `aaffc25` em `main` (hok-backend-atual) — "fix: rota /anthropic nativa para Claude Code em modelos deepseek-native + opencodeModelID" (SÓ `model_propagate.go` + `opencode_client.go`). Push OK.
- Git status limpo (WIP antigo não entrou).

## Observações
- O sidecar `.hok_anthropic_orig.json` fica ao lado de cada `settings.json` (root e hokma-agent), preservando os tokens OpenRouter distintos de cada arquivo (não hardcodados).
- Chat web nativo e opencode nativo não foram tocados (permanecem funcionando).
- A pendência do adendo de 09/09 (cache hit na rota /anthropic) fica FECHADA: validado 6656 tokens em cache.

## Status final
- [x] build isolado + go test OK
- [x] deploy backend
- [x] re-propagação do modelo ativo (corrigiu settings corrompidos)
- [x] validação Cenário 1 (nativo) — CLI real OK
- [x] validação Cenário 2 (volta OpenRouter) — restore OK
- [x] cache hit /anthropic confirmado (6656 tokens)
- [x] commit separado + push
- [x] adendo enviado via webhook n8n