# Adendo — Sessão Chat-Web-HOK / 16:17/17/09/2026

## Diagnóstico + Fix: Seletor de Modelos do Chat mostrando catálogo antigo/pago

### Problema
O dropdown de modelos do Chat HOK (seletor do usuário no app) mostrava:
- Categoria "OpenCode Zen" com dezenas de modelos (GPT-5, Claude Opus, Gemini, Grok, etc.)
- Categoria "OpenRouter PAGO" com DeepSeek V4.1 Flash e DeepSeek V4 Flash 0731 como pagos via OpenRouter

Isso violava a política: só OpenRouter :free confirmados + DeepSeek oficial pago.

### Causa raiz
O `index.html` (`/var/www/hok-os/index.html`) referencia `/assets/index-C0ZEEth4.js` como bundle principal. Este bundle importa `/assets/index-CUg8fESz.js` (que contém hok-models.ts). Ambos faziam fetch de `/models/catalog` (637 modelos completos, incluindo pagos), e continham nos FALLBACK_MODELS e PAID_MODEL_ALLOWLIST modelos pagos do OpenRouter.

**Erro crítico da sessão anterior**: o fix de `/models/catalog` → `/models/available` foi aplicado ao bundle errado (`index-DQ6f2GhX.js`, 456154 bytes), que NÃO é referenciado pelo `index.html`. O bundle servido (`index-C0ZEEth4.js`, 455829 bytes) permaneceu com `/models/catalog`.

### Ações aplicadas

#### 1. Backend — `/models/available` reescrito (sessão anterior, confirmado)
- **`models_routes.go`**: reescrito completo
  - `fetchOpenRouterFreeModels()`: consulta `https://openrouter.ai/api/v1/models` com `OR_KEY`, filtra pricing.prompt=="0" && pricing.completion=="0", cache 20min
  - `getDeepSeekOfficialModels()`: 2 modelos deepseek-native como "DeepSeek Oficial"
  - Cada modelo inclui `source: "openrouter_free"` ou `"deepseek_official"`
  - Removeu `refreshOpenCodeModels()`, `categorizeModel()`, import `os/exec`

#### 2. Bundles frontend corrigidos (esta sessão)
- **`/var/www/hok-os/assets/index-C0ZEEth4.js`** (bundle principal, referenciado por index.html):
  - `/models/catalog` → `/models/available` (2 ocorrências)
  - Removido `deepseek/deepseek-v4-flash-0731` do FALLBACK_MODELS e PAID_ALLOWLIST
  - Removido `deepseek/deepseek-v4.1-flash` do PAID_ALLOWLIST
- **`/var/www/hok-os/assets/index-CUg8fESz.js`** (importado por ModelsScreen):
  - Idem ao anterior
- **`/tmp/hokma_terminal_20260914_101633/lib/hok-models.ts`** (fonte, sessão anterior):
  - `/models/catalog` → `/models/available`

#### 3. Backups criados
- `/var/www/hok-os/assets/index-C0ZEEth4.js.bak_20260917_160800`
- `/var/www/hok-os/assets/index-CUg8fESz.js.bak_20260917_160800`

### Verificação final

| Teste | Resultado |
|-------|-----------|
| `go build` | ✅ PASS |
| `go vet .` | ✅ 1 aviso pre-existente (drive_connector.go) |
| Backend `/` | status=online, version=v25 ✅ |
| `/models/available` | 27 modelos (25 OpenRouter Free + 2 DeepSeek Oficial) ✅ |
| `/models/available` — paid in OpenRouter | 0 (should be 0) ✅ |
| `/models/available` — models without source | 0 (should be 0) ✅ |
| `/models/available` — ModelA in list | 0 (should be 0) ✅ |
| `/models/catalog` (compatibilidade) | 637 modelos, 6 providers ✅ |
| `/models/catalog` references em bundles JS | 0 ✅ |
| Paid OpenRouter models em bundles | 0 (todos removidos) ✅ |
| FALLBACK_MODELS em bundles | apenas deepseek-native (2) ✅ |
| PAID_MODEL_ALLOWLIST em bundles | apenas deepseek-native (2) ✅ |

### Fluxo atual do seletor

```
Frontend (index-C0ZEEth4.js) → getModels() → /models/available
  → getFreeModels()      → 25 modelos free (OpenRouter Free)
  → getPaidModels()      → 2 modelos (DeepSeek Official via PAID_ALLOWLIST)
  → getZenModels()       → 0 (sem Zen em /models/available)
  → FALLBACK_MODELS      → 2 deepseek-native (apenas se API falhar)
```

### Fluxo do chat dropdown

```
Chat component → F0(!0), $0(!0), J0(!0) → getPaidModels, getFreeModels, getZenModels
  → La (model list) populado de /models/available
  → La.filter(!.free && .provider!=="OpenCode Zen") → paid models (2 deepseek-native)
  → La.filter(.free) → free models (25 OpenRouter Free)
  → La.filter(.provider==="OpenCode Zen") → zen (0)
```

### Arquivos Modificados
- `/root/hokma/backend/models_routes.go` — reescrito completo (sessão anterior)
- `/tmp/hokma_terminal_20260914_101633/lib/hok-models.ts` — /models/catalog → /models/available
- `/var/www/hok-os/assets/index-C0ZEEth4.js` — FALLBACK/ALLOWLIST limpos, endpoint corrigido
- `/var/www/hok-os/assets/index-CUg8fESz.js` — FALLBACK/ALLOWLIST limpos, endpoint corrigido

### Serviços
- `hokma.service :8082` — REINICIADO via systemd (binary atualizado)
- `nginx :3002` — ativo (sem alteração, serve bundles estáticos)
