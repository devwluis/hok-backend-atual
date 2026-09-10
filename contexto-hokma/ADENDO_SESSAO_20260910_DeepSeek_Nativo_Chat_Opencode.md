# ADENDO_SESSAO_20260910_DeepSeek_Nativo_Chat_Opencode.md

## Resumo

Implementação do DeepSeek NATIVO (api.deepseek.com via `DEEPSEEK_API_KEY`, NÃO via OpenRouter) em dois canais: (1) seção isolada "DeepSeek" no seletor do chat web HOK OS com os modelos oficiais `deepseek-flash` e `deepseek-v4-pro`; (2) provider custom isolado "deepseek-native" no opencode (config local + auth.json). Objetivo: aproveitar a franquia de 1M tokens/24h de cache hit (desconto 80-98% em prompt repetido), que só vale na rota nativa.

## Descobertas técnicas

- **Modelos oficiais da API** (confirmado via `GET api.deepseek.com/models`):
  `deepseek-flash` e `deepseek-v4-pro`. Os IDs `deepseek-v4-flash`, `deepseek-chat`, `deepseek-reasoner` são ALIAS aceitos mas a API responde com o canônico `deepseek-flash` (os antigos citados pelo usuário como "v4-flash" foram descontinuados).
- `callAPI()` (ai.go) JÁ loga `[usage]` com cache hit/miss (reutilizado, nada novo de logging).
- `DS_URL` (main.go) já existia — só o roteamento faltava.
- Catálogo (models_catalog.go) já tinha a ordem de providers com "DeepSeek" — bastou injetar 2 modelos com esse provider para o grupo isolado aparecer no seletor (front agrupa por provider).

## Changes

### Backend (Go) — /root/hokma/backend
- **`models_catalog.go`**: nova func `nativeDeepSeekModels()` injeta sempre no catálogo:
  - `deepseek-native/deepseek-flash` → label "DeepSeek V4 Flash" (pago, provider "DeepSeek")
  - `deepseek-native/deepseek-v4-pro` → label "DeepSeek V4 Pro" (pago, provider "DeepSeek")
  - Integração no `refreshCatalog()` após o merge (IDs deduplicados pelo merge existente).
- **`ai.go`**: novo bloco no `routeModel()` ANTES do bloco `deepseek` (OpenRouter):
  - `deepseek-native/*` → `callAPI(DS_URL, DEEPSEEK_API_KEY, ...)` direto (fora do gate free-only, caminho explícito como gemini/gpt/deephat).
  - Key lida de `os.Getenv("DEEPSEEK_API_KEY")` com fallback `DS_KEY` — nunca hardcoded.
  - TRAVA de segurança idêntica às demais: erro permanente não cai na cascata (auditModelBlock).

### Frontend — hok-os (src/lib/hok-models.ts)
- `PAID_MODEL_ALLOWLIST` + `FALLBACK_MODELS`: adicionados os 2 IDs nativos com label limpo.

### OpenCode
- `~/.config/opencode/opencode.json`: provider customizável `deepseek-native`
  (`@ai-sdk/openai-compatible`, `baseURL: https://api.deepseek.com/v1`, modelos `deepseek-flash` + `deepseek-v4-pro`). Modelo default `minimax/minimax-m3:free` preservado.
- `~/.local/share/opencode/auth.json`: entrada `deepseek-native` com key **lida do .env** (não committada, não hardcoded).

## Validação — prova real do cache hit

### Chat web (binário isolado, porta 9910):
2 chamadas com o MESMO histórico/prefixo em `/chat/smart` com modelo `deepseek-native/deepseek-flash`:
```
chamada 1: prompt=1534 completion=3 cached=0  cache_miss=1534
chamada 2: prompt=1534 completion=3 cached=1280 cache_miss=254   ← CACHE HIT 1280 tokens
```
**(83% do prompt em cache hit — franquia de 1M tokens/24h sendo efetivamente aproveitada).**

### OpenCode:
`opencode run --model deepseek-native/deepseek-flash "responda apenas: opencode nativo ok"` → respondeu via api.deepseek.com.
`opencode models` lista `deepseek-native/deepseek-flash` e `deepseek-native/deepseek-v4-pro` como selecionáveis.

## Deploy

- Backend: `./deploy.sh` completo (build e7ba3f2, tests OK, health 200, hashes idênticos).
- Frontend: `pnpm build` + copiado dist/public → `/var/www/hok-os` (backup feito). Novo JS/CSS servido pelo nginx (200).
- Catálogo em prod: grupo "DeepSeek" com os 2 modelos visível via `/models/catalog`.

## Commits

- Backend: `6aab550` em `main` (hok-backend-atual) — "feat: DeepSeek nativo no chat web e opencode + cache hit" (só `ai.go` + `models_catalog.go`; WIP antigo de agent_loop/terminal_routes NÃO entrou, conforme instrução).
- Frontend: `c2e2aaa` em `feature/preview-tab` (hok-frontend-atual) — "feat: modelos DeepSeek nativo (deepseek-flash, deepseek-v4-pro) no seletor do chat" (só `hok-models.ts`).
- Push: ambos repos atualizados no GitHub (devwluis).

## BUG DE PROCESSO — DB_PATH vazou teste p/ produção (IMPORTANTE)

Durante o smoke test do binário isolado na porta 9910, eu NÃO setei `DB_PATH` isolado. Como o binário usa o mesmo SQLite default, o `/models/select` do TESTE gravou `activeModel=deepseek-native/deepseek-flash` no DB de PRODUÇÃO, e o `setActiveModel` propagou para `~/.claude/settings.json`, `/home/hokma-agent/.claude/settings.json` e `~/.opencode/opencode.json` — corrompendo a config dos motores (o Claude Code passou a receber um ID que o proxy OpenRouter não aceita).

**Correção:** restaurado modelo ativos para o valor anterior via `/models/select` com `opencode/mimo-v2-pro-free` (confirmado do journal último boot às 05:25) — re-propaga OK nos 3 arquivos.

**Regra de processo para futuros testes de binário isolado:**
```
PORT=9910 DB_PATH=/tmp/hok_test_9910.db ROOT_PATH=/tmp/hok_test_root_9910 ./hokma_new
```
SEMPRE setar `DB_PATH` e `ROOT_PATH` isolados (ex: /tmp) quando rodar binário de teste que entra em /models/select, /chat/smart ou qualquer rota que escreva app_settings. Sem isso, qualquer teste volta a vazar para produção.

## Pendentes / observações

- O `propagateActiveModelToMotors` escreve o ID do modelo ativo nos settings do Claude/OpenCode. Se o usuário selecionar `deepseek-native/*` no chat web (ativo), ele propaga esse ID que o OpenRouter no Claude NÃO aceita. Nessa condição, o chat web funciona (rota nativa, sem tocar OpenRouter), mas o Claude Code do terminal vai apontar pro modelo nativo — para uso real, preferir MANTER o ativo de produção nos modelos OpenRouter (deepseek/deepseek-v4-flash-0731 ou minimax) e selecionar o nativo só via request explícito. Ponto a refinar em sessão futura.
- model-propagate para opencode: para o default (model) ele prefixa `openrouter/` — no back do `deepseek-native` não faz reressesc para a rota nativa. (Sem impacto funcional — o chat web usa routeModel direto.)
- Adendo (este arquivo) commit local + webhook n8n `contexto-hok-terminal` → Drive (conta gestordeanunciosbr@gmail.com).

## Status final

- [x] build isolado (`hokma_new`) OK + `go test ./...` OK
- [x] deploy backend (deploy.sh) + frontend (rebuild + /var/www/hok-os)
- [x] smoke test real: cache hit 1280 tokens provado (chat web), opencode nativo funcionando
- [x] restauração do modelo ativo de produção (bug DB_PATH corrigido + processo documentado)
- [x] commits separados + push (backend + frontend)
- [x] adendo enviado via webhook n8n para o Drive