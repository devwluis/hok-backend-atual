# Adendo — Catálogo de modelos automático direto com OpenCode CLI — 2026-08-27

Pedido do usuário: "atualize a lista de IAs e deixe automática a busca por
modelos novos direto com opencode para o chat HOK".

## O que foi feito

1. **`models_catalog.go`** — nova fonte no catálogo unificado (`/models/catalog`,
   que alimenta a lista de IAs do chat HOK):
   - `fetchOpenCodeCLIModels()` roda `opencode models` (binário local) com
     timeout de 90s e converte a saída `provider/id` em itens do catálogo:
     - `opencode/<id>` → OpenCode Zen
     - `opencode-go/<id>` → OpenCode Go (free)
     - `google/<id>` → Google (mantém prefixo `google/` para dedupe/uso real)
     - `openrouter/*` ignorados (a fonte OpenRouter API já cobre com pricing)
   - Injeta `GEMINI_API_KEY` a partir do `GEMINI_KEY` do .env no processo do
     CLI (o provedor google do opencode lê GEMINI_API_KEY; valor nunca logado).
   - Cache próprio com TTL 12h, integrado ao `refreshCatalog` (background a
     cada 5 min + `?force=1`) e ao `mergeModels` (prioridade: Zen → Go →
     OpenRouter → CLI; a CLI só complementa o que as APIs não têm).
2. **`models_routes.go`** — `/models/available` (lista direto do opencode CLI)
   agora tem TTL de 10 min com mutex: re-roda `opencode models openrouter`
   automaticamente (antes rodava só na primeira requisição, para sempre).

## Resultado em produção (porta 8082)

- Catálogo: **510 → 539 modelos** (31 free, 508 pagos)
- Grupos: OpenCode Zen 63, Google 17 (novo), OpenRouter 417, OpenCode Go 42
- Log: `[catalog] OpenCode CLI: 122 modelos descobertos via `opencode models``
- Auth preservada: 401 sem token, 405 para POST.

## Validação

- Backup dos arquivos: `models_catalog.go.bak_20260827_023301`,
  `models_routes.go.bak_20260827_023301`; binários de produção antigos:
  `hokma.bak_predeploy_models_*`, `hokma.bak_predep2_models_*`.
- Build isolado `hokma_test`, teste em porta 18085 com env limpo (simulando
  systemd): catálogo 539 modelos, Google 17, sem erros/panics.
- Primeira versão em produção não listava Google (CLI sem GEMINI_API_KEY no
  env do systemd) — fix do env injetado e redeploy validado: Google 17 ✓.

## Pendências

- Commit/push no branch `hok-backend-atual` (aguardando confirmação).

## Adendo 2 — deepseek-v4-pro do OpenCode Go (27/08, ~03:00)

Usuário apontou que faltava `deepseek-v4-pro` do OpenCode Go na lista do chat.
Causa: a fonte CLI tirava o prefixo `opencode-go/` (deduplicava com o id Zen)
e a API Go marcava free=False. Fixes:

- `models_catalog.go`: CLI mantém `opencode-go/<id>` (formato aceito pelo
  `--model` do opencode); API Go passa a marcar free=True por padrão (tier
  gratuito) — só vira pago com pricing não-zero explícito.
- `opencode_client.go` (`opencodeModelID`): pass-through de `opencode-go/`
  (antes virava `openrouter/opencode-go/...` = id inválido).
- Produção validada: grupo OpenCode Go com 31 modelos canônicos, incluindo
  `opencode-go/deepseek-v4-pro` FREE ✓ (catálogo 528 modelos, 51 free).
- Backups: `models_catalog.go.bak_20260827_030000`,
  `opencode_client.go.bak_20260827_030000`, `hokma.bak_predep3_gofix_*`.
