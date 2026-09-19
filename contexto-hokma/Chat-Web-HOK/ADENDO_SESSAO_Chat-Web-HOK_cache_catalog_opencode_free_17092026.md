# Adendo — Sessão Chat-Web-HOK / 19:30/17/09/2026

## Objetivo
Corrigir a lista de modelos da tela **Chat** do HOK OS, que continuava exibindo
modelos OpenCode Zen/Go **PAGOS** (Kimi K2.7, GPT-6 Astra, Claude Opus 4.8)
mesmo após o usuário "limpar cache" do navegador — e, depois, garantir que os
modelos **FREE** do OpenCode voltassem a aparecer (a política é
"só free real + DeepSeek oficial pago").

---

## Causa raiz REAL (evidência de rede — não suposição de código)

A tela Chat rodava um **bundle antigo preso em cache** (PWA / `Cache-Control`
`immutable` de 1 ano) que chamava **`/models/catalog`** — endpoint que
retornava **637 modelos**, incluindo **69 Zen + 35 Go** com os pagos.
O bundle NOVO chama **`/models/available`**.

Prova em `nginx access.log` — contagem de endpoints `/models/`:

| Endpoint | Chamadas |
|---|---|
| `/models/available` | 89 (bundle novo) |
| `/models/catalog`   | **3124** (bundle antigo) |
| `/models/select`    | 259 |

Transição do dispositivo (Android Chrome 152):

| Momento | Evento |
|---|---|
| 17/Sep 14:03:01 | carregou `index-C0ZEEth4.js` (**antigo**) |
| 17/Sep 16:39:12 | carregou `index-BqNmD4uF.js` (**novo**) |
| 17/Sep 16:33:54 | última chamada mobile a `/models/catalog` |

Auditoria de modelo ativo (`journalctl -u hokma`):

- 14:03–14:42 → `opencode/union-alpha`, `opencode/nemotron-3.5-lightning-free` (ZEN, do catalog)
- 16:45+ → `inclusionai/ling-3.0-flash-vl:free`, `google/gemma-4-26b-a4b-it:free` (OpenRouter Free)

O bundle antigo renderiza `zen.filter(m => !m.free)` → grupo **"OpenCode Zen"**
com os **62 modelos PAGOS**. Exatamente os nomes relatados pelo usuário.

**Por que as correções anteriores não resolveram:** as sessões anteriores só
corrigiram `hok-models.ts`/`ModeSelector.tsx` para usar `/models/available` e
reconstruíram o bundle. Qualquer cliente preso no bundle antigo continuava
chamando `/models/catalog`, que **nunca foi corrigido** e devolvia os pagos.
"Limpar cache" não resolve se o bundle vem de PWA/camada própria de cache.

---

## Fix 1 — `/models/catalog` com a mesma política

`models_catalog.go` → `handleModelsCatalog`:

- Filtra a resposta para `m.Free || strings.HasPrefix(m.ID, "deepseek-native/")`
  (free real + DeepSeek oficial).
- `?full=1` preserva o catálogo completo (637) para debug/admin.
- Headers `Cache-Control: no-cache, no-store, must-revalidate` + `Pragma` + `Expires: 0`.

Resultado: `/models/catalog` → **40** (38 free + 2 DeepSeek), **0 pagos Zen/Go**.
Neutraliza **qualquer** cliente cacheado, mesmo o bundle antigo.

---

## Fix 2 — OpenCode FREE volta ao `/models/available`

A reescrita anterior do `/models/available` havia removido **TODOS** os modelos
do tier OpenCode (Zen/Go) — inclusive os de **custo ZERO confirmado**. Isso
deixou o seletor só com OpenRouter Free + DeepSeek.

`models_routes.go` → `handleModelsAvailable`: lê o catálogo em memória e
adiciona os grupos **"OpenCode Zen"** e **"OpenCode Go"** com `Free == true`
(custo 0/0 confirmado no `models.json` local via `opencodeModelIsFree`).

Free readicionados:

- **OpenCode Zen (7):** `muse-spark-1.3-contributor-free`, `nemotron-3.5-lightning-free`,
  `big-pickle`, `ling-3.0-flash-fin-free`, `nemotron-3-ultra-free`, `mimo-v2.5-free`, `union-alpha`
- **OpenCode Go (2):** `union-alpha`, `ox-alpha-free`

Pagos continuam **fora** (Kimi K2.7, GPT-6 Astra, Claude Opus 4.8, Muse Spark 1.2...).

---

## Estado final verificado (produção)

| Verificação | Resultado |
|---|---|
| `/models/available` | **36** = DeepSeek Oficial (2) + OpenRouter Free (25) + OpenCode Zen (7) + OpenCode Go (2) |
| paid OpenCode | **NONE** ✅ |
| `/models/catalog` | 40 (38 free + 2 DeepSeek), 0 pagos Zen/Go ✅ |
| `/models/catalog?full=1` | 637 (escape hatch admin) ✅ |
| Headers | `no-cache, no-store, must-revalidate` / `Pragma: no-cache` / `Expires: 0` ✅ |
| Verificação | local (127.0.0.1:8082) **e** externo (Cloudflare) ✅ |

---

## Arquivos modificados

- `/root/hokma/backend/models_catalog.go` — `handleModelsCatalog` (política + headers)
- `/root/hokma/backend/models_routes.go` — `handleModelsAvailable` (free OpenCode Zen/Go)

## Backups (binário)

- `hokma.bak.20260917_185850_pre_catalog_policy`
- `hokma.bak.20260917_192508_pre_opencode_free_restore`

## Serviços

- `hokma.service :8082` — REINICIADO (19:25, `active`, PID 1787620)

## Observação de deploy

`cp` do binário sobre um executável em execução falha com **"Text file busy"**.
Usar sempre `cp novo /path/bin.new && mv -f bin.new bin` (rename atômico) antes
do `systemctl restart`.
