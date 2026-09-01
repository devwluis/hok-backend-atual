# ADENDO — SESSÃO 31/08/2026 — Limpeza total do catálogo free (remover pagos, manter só free confirmado)

Limpeza total do catálogo de modelos do HOK: auditoria de TODOS os modelos contra o pricing
real das APIs oficiais dos 4 providers permitidos (OpenCode Zen, OpenCode Go, OpenRouter,
AIHubMix) e remoção permanente de modelos confirmados pagos. Aplicado em **produção (8082)**
com aprovação do usuário em cada passo. Commit criado; **push pendente** (sem credencial GitHub
no ambiente).

---

## Descoberta importante: onde fica o catálogo persistido

- O **`backend/hokma.db`** é o banco **CRM** (só a tabela `crm_context_sources`). **NÃO** é o
  catálogo.
- As tabelas do catálogo (`catalog_snapshot`, `catalog_audit`) vivem no **`memory.db`**
  (`DB_PATH=/root/hokma/backend/memory.db`, main.go:44). O `catalog_snapshot` é a fonte de
  verdade da curadoria free (persistido), e o catálogo em memória é reconstruído ao vivo
  pelas 5 fontes (Zen/Go/OpenRouter/AIHubMix/CLI).

## Estado inicial

- Snapshot (`catalog_snapshot`): **115 modelos free** (seed da curadoria free do início do dia).
- Distribuição: AIHubMix 52, OpenCode Go 33, OpenCode Zen 7, OpenRouter 23.

## Fonte de dados (todas consultadas AO VIVO, sem suposição por nome)

| fonte | endpoint | resposta |
|---|---|---|
| OpenRouter | `https://openrouter.ai/api/v1/models` | 200 — pricing `prompt`/`completion` por modelo |
| AIHubMix | `https://aihubmix.com/api/v1/models?type=llm` | 200 — pricing `input`/`output` por modelo |
| OpenCode Zen | `https://opencode.ai/zen/v1/models` | **200 agora** (antes 403) — sufixo `-free` no id marca free |
| OpenCode Go | `https://opencode.ai/zen/go/v1/models` | **200 agora** (antes 403) — tier gratuito, sem pricing |

> Nota: a sessão anterior documentou que as APIs Zen/Go retornavam **HTTP 403** deste host.
> Nesta sessão ambas responderam **200** — o catálogo Zen/Go agora pode ser validado direto
> pela API oficial (e não só via CLI `opencode models`).

## Auditoria (115 modelos) — resultado

Cruzamento snapshot x pricing real: **115/115 confirmados presentes nas fontes oficiais** com
custo zero. Nenhum "falso free" detectado além dos 3 removidos por regra explícita abaixo.

### REMOVIDOS (3) — confirmados pagos, regra explícita (independente do que a API disser)
| modelo | provider | confirmação de pago |
|---|---|---|
| `opencode-go/glm-5.3-flash` | OpenCode Go | OpenRouter `z-ai/glm-5.3-flash` pago; AIHubMix `glm-5.3-flash` = $0.11/$0.39/M |
| `opencode-go/muse-spark-1.2-contributor` | OpenCode Go | OpenRouter `meta/muse-spark-1.2` = **$1.25/$4.25/M** (exato ao informado); AH `muse-spark-1.2` = $1.38/$4.68/M |
| `opencode/muse-spark-1.2-contributor-free` | OpenCode Zen | mesmo modelo Muse Spark 1.2 — regra: remover sempre |

### MANTIDOS (112) — free confirmado na fonte oficial
- **AIHubMix (52):** todas com `input=0 && output=0` na API oficial (incl. `gpt-oss-20b-free`,
  `qwen3.6-plus-preview-free`, `k2.6-code-preview-free`, `gemma-4-*-free`, `nemotron-3-*-free`,
  `coding-glm-5.2-free`, `coding-glm-5.3-free`, `ox-alpha`).
- **OpenCode Go (31):** todos listados na API oficial do tier gratuito (incl. `deepseek-v4-flash`,
  `glm-5.2`, `kimi-k2.6`, `kimi-k2.7-code`, `qwen3.6-plus`).
- **OpenCode Zen (6):** todos com sufixo `-free` na API oficial (incl. `deepseek-v4-flash-free`).
- **OpenRouter (23):** todos com `pricing 0/0` na API oficial (incl. `z-ai/glm-5.2:free`,
  `google/gemma-4-*:free`, `nvidia/nemotron-3-*:free`, `openrouter/free`).

## Candidatos adicionais (Passo 6) — verificação de variantes free
| candidato | variante free? | decisão |
|---|---|---|
| Qwen3/Qwen3.6 | sim — já no catálogo (Go tier + AH `qwen3.6-plus-preview-free`) | já incluso |
| Kimi K2.6/K2.7 Code | sim — já no catálogo (Go tier + AH `k2.6-code-preview-free`) | já incluso |
| GLM-5.2 | sim — OR `z-ai/glm-5.2:free` + AH `coding-glm-5.2-free` + Go tier | já incluso |
| **gpt-oss-120b** | **não** — OR e AH pagos; nenhum dos 4 providers tem variante `-free` | **não adicionado** |
| Gemma 4 | sim — OR `:free` + AH `-free` | já incluso |
| Nemotron 3 | sim — OR/AH/Zen free | já incluso |

> **Caso de atenção:** `aihubmix/coding-glm-5.3-free` (pricing 0/0 confirmado na API oficial do
> AIHubMix) é a variante *coding* gratuita — **distinta** do GLM-5.3-Flash pago (`glm-5.3-flash`
> no AH = $0.11/$0.39; `z-ai/glm-5.3-flash` no OR = pago). Mantido com aprovação do usuário.

## Consistência permanente — `paidDenylist` no código

Problema detectado: o sync diário re-deriva o snapshot do catálogo em memória (repopulado pelas
APIs). Como a API Go/Zen continua listando os modelos removidos (sem pricing exposto), um sync
posterior **re-inseriria** os 3. Fix:

- `models_catalog.go`: `var paidDenylist = map[string]bool{...}` (3 IDs) — filtro no
  `mergeModels` (catálogo em memória) e no `freeCatalogFromCache` (catalog_sync.go, curadoria
  free). O sync **nunca** re-insere, independente do que a API listar.
- Teste novo `TestCatalogPaidDenylist` (merge + curadoria free) — PASS.

## Mudanças aplicadas (com aprovação do usuário)

1. Backups: `models_catalog.go.bak_20260831_184805_denylist`, `catalog_sync.go.bak_*`,
   `catalog_sync_test.go.bak_*`, `memory.db.bak_20260831_184805_cleanup3`.
2. Build isolado `hokma_test` + `go test` (7 testes de catálogo) — **PASS**.
3. `memory.db`: 3× `DELETE FROM catalog_snapshot` + 3× `INSERT INTO catalog_audit` (action
   `removed`, detalhe "CURADORIA 31/08 ... denylist").
4. **Restart produção** (aprovado): `systemctl stop hokma && cp hokma_test hokma && start` →
   `is-active: active`, health 200.
5. Validação: `POST /catalog/sync` → `{"status":"ok","summary":"+0 added, -0 removed,
   0 metadata_changed"}` — **snapshot permanece 112** (denylist impediu re-inserção).
6. `GET /models/catalog` → `freeCount=112 | activeStatus=ok`; denylist ausente do catálogo.

## Total final

**115 → 112 modelos free** no `catalog_snapshot`.

## Git

- Commit: `e928bc9` — `feat(catalog): limpeza total free — remove GLM-5.3-Flash e Muse Spark 1.2 via denylist`
  (5 arquivos: `db.go`, `main.go`, `models_catalog.go`, `catalog_sync.go`, `catalog_sync_test.go`;
  **somente** curadoria — `opencode_client.go`/`smart_chat.go`/`autonomous_allowlist.go` de outra
  tarefa ficaram fora).
- **Push pendente**: `git push origin main:hok-backend-atual` falhou com `could not read Username
  for 'https://github.com'` — sem credencial no ambiente (sem `~/.git-credentials`, sem `gh` CLI).
  Branch local `main` está 11 commits à frente de `origin/hok-backend-atual` (fast-forward limpo).
  Pendente: rodar push em terminal autenticado ou configurar credencial.

## Problemas encontrados no caminho
1. `.env` não é carregado em builds/testes do shell → `go test` falhava com "HOK_TOKEN nao
   definida" (log.Fatal no init) → carregar via `set -a; source .env; set +a`.
2. Auth do `curl` usa header `X-Hok-Token` (requireHokAuth) — necessário para
   `POST /catalog/sync` e `GET /models/catalog`.
3. Push GitHub sem credencial no ambiente (item Git acima).

## Pendências
- **Push** do commit `e928bc9` para `origin/hok-backend-atual` (autenticar ou rodar manualmente).
- Zen/Go agora respondem 200 via API — revisar o `fetchOpenCodeGoModels`/`fetchOpenCodeZenModels`
  (que marcam free como `manual-go`/`cli`) para aproveitar o pricing/sufixo oficial direto da API,
  em sessão futura.
- `rate_limit`/`dataRetention` seguem `null` (APIs oficiais não expõem) — pendência antiga.