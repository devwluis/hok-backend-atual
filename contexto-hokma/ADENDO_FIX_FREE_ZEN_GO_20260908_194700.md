# ADENDO — Correção badge FREE em OpenCode Zen/Go + deploy produção (08/09/2026)

**Data:** 2026-09-08 19:47 UTC
**Operador:** opencode (mimo-v2.5-free)

## Causa raiz do badge errado

### OpenCode Go (`fetchOpenCodeGoModels`)
`isFree := true` por padrão + a API Go (`https://opencode.ai/zen/go/v1/models`) NÃO expõe pricing → TODOS os 35 modelos eram marcados como free. Modelos claramente pagos (kimi-k3, glm-5.2, gpt-5.6-luna, deepseek-v4-pro...) apareciam com badge FREE.

### OpenCode Zen (`fetchOpenCodeZenModels`)
Critério por sufixo `-free` funcionava parcialmente, mas a API Zen sem auth retorna só 70 modelos e sem pricing — `big-pickle` (cost zero) ficava como pago.

## Correção aplicada

1. **Fonte de verdade**: `/root/.cache/opencode/models.json` — catálogo OFICIAL completo do opencode (213 providers, ~7.5k modelos, atualizado pelo CLI/serve do próprio servidor) com `cost.input`/`cost.output` reais por modelo.
2. **`opencodeLocalProviderModels()`**: lê a lista completa de um provider (id, nome, custo, contexto) do models.json.
3. **`fetchOpenCodeZenModels()` / `fetchOpenCodeGoModels()`**: usam models.json como fonte primária; fallback gracioso para a API (com critério de custo/sufixo `-free`) se o arquivo estiver ausente.
4. **`fetchOpenCodeCLIModels()`**: removido o mesmo bug `isFree := true` para `opencode-go/*` — agora usa `opencodeModelIsFree()`.
5. **Frontend (`ChatScreen.tsx`)**: grupo "OpenCode Zen ZEN" agora filtra `!m.free` — elimina a duplicação dos `-free` que apareciam no fim da listagem ZEN.

## Confirmação do models.json como catálogo oficial

- 213 providers, 7.574 modelos totais (cache de runtime teria ~5)
- Metadata de config por provider (api/env/npm/doc) — curadoria oficial
- `opencode` → "OpenCode Zen" (102 modelos), `opencode-go` → "OpenCode Go" (35)
- mtime atualizado automaticamente (19:29 hoje) pelo CLI rodando no servidor

## Evidência pós-deploy (produção)

| Fonte | Total | Free | Pago |
|-------|-------|------|------|
| OpenCode Zen | 101 | 30 | 71 |
| OpenCode Go | 33 | 1 | 32 |
| OpenRouter | 431 | 21 | 410 |
| **Total** | 650 | 56 | 594 |

- `opencode/big-pickle`: **free=True** (source=zen-models-json) ✓
- `opencode-go/ox-alpha-free`: **free=True** (source=go-models-json) ✓
- `opencode-go/glm-5.3-flash`: **AUSENTE** (paidDenylist preservado) ✓
- `opencode-go/muse-spark-1.2-contributor`: **AUSENTE** (paidDenylist preservado) ✓

Diferenças vs expectativa (explicadas): Zen 30 free em vez de 31 (`muse-spark-1.2-contributor-free` no denylist); Go 33 em vez de 35 (`glm-5.3-flash` + `muse-spark-1.2-contributor` no denylist).

## Testes

- `go build` ✓ | `go vet` ✓ | `go test ./...` → **ok** (186s) ✓

## Validação pós-deploy

| Teste | Resultado |
|-------|-----------|
| hokma.service | active (running) ✓ |
| /health | {"status":"ok"} ✓ |
| GET /session/mode | 200 (autonomous_total preservado) ✓ |
| POST /session/mode (4 modos) | 200/200/200/200 ✓ |
| POST /chat/smart | 200 ✓ |
| Modo Auto (bundle) | `{id:"auto",label:"Auto",provider:"HOK"}` ✓ |
| Duplicação ZEN | Eliminada (`zen.filter(m => !m.free && ...)`) ✓ |

## Backups

- `hokma.bak_pre_deploy_20260908_194504` (binário antigo, md5 `08269a0d...`)
- Binário novo em produção: md5 `12222cdb...`
- Frontend: `index-Crc8ThkV.js`