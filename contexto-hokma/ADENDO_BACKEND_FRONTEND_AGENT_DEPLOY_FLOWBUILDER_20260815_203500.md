# ADENDO — Backend + Frontend: rotas /agents, /deploy/status, /flows conectadas

**Origem:** Claude Code / opencode (terminal)
**Data/hora:** 15-08-2026 20:35

---

## Resumo

Fechamento do trabalho em andamento: 3 rotas novas no backend Go + 3 telas do frontend (hok-os) conectadas. CRM fora de escopo (pausado, não tocado).

## Commits feitos (backend, repo devwluis/hok-backend-atual, branch main)

| Commit | Escopo |
|---|---|
| `f851609` | refactor: parsing SQL migrado para `sqliteExecQuoted`/`parseQuotedRows` (formato CSV-style com escape de aspas/newlines) e mascara de secrets em `/settings` (nunca devolve token em texto puro, apenas `<chave>Configured: true/false`) |
| `314bac0` | fix: isolamento por tenant em `/conversations` — GET filtra por `tenant_id` via JWT (mantendo conversas legadas sem tenant visíveis), save grava `tenant_id`; parametrizado, sem interpolação de string |
| `68410d4` | feat: rotas novas read-only `/agents`, `/deploy/status`, `/flows` — todas com `requireHokAuth`; `fetchN8nWorkflowsItems` extraído de `/n8n/workflows` e reutilizado com contagem de steps |
| `7c93439` | fix: `/deploy/status` agora aponta o git para o repo do backend (`/root/hokma/backend`) em vez do repo raiz de contexto; adiciona campo `repo` na resposta |

Todos pushados: `79a347f..7c93439 main`.

## Rotas novas e formatos

### GET /agents (auth: requireHokAuth)
Agentes reais = monitores de trigger em runtime (`triggers.go`). Somente leitura — a UI marca ações como não suportadas.
```json
{"status":"ok","agents":[{"id":"on_disk","name":"Monitor de Disco","desc":"...","status":"running","last_fired":"RFC3339 ou vazio","last_msg":""}, ...]}
```

### GET /deploy/status (auth: requireHokAuth)
Status real de produção: git (branch/commit do repo backend) + systemd. Estritamente read-only (sem gatilho de ação).
```json
{"status":"ok","env":{"name":"Production","branch":"main","commit":"...","commit_short":"7c93439","commit_time":"...","commit_msg":"...","repo":"/root/hokma/backend","services":{"hokma":"active","nginx":"active"}},"deploys":[],"deploys_note":"Histórico de deploys não é registrado..."}
```

### GET /flows (auth: requireHokAuth)
Mapeia workflows do n8n para o formato da tela Flow Builder (name, steps, status). Sem POST.
```json
{"status":"ok","flows":[{"name":"HOK OS — Monitor de Saude","steps":4,"status":"active"}, ...]}
```
Produção: 16 flows listados, steps contados via `len(nodes)`.

## Validação
- Teste isolado na porta 8090 (HOK_TOKEN de teste): 3 rotas OK + 401 sem token.
- Produção após deploy: `/agents` 3 agentes, `/deploy/status` branch main/commit 7c93439/serviços active, `/flows` 16 workflows.
- Regressão: `/health` ok, `/conversations` ok (42 conversas).
- `go build` + `go vet` + `go test ./...` OK.
- Deploy: binário substituído (backup `hokma.prev_20260815_202509`), `systemctl restart hokma` ativo.

## Frontend (hok-os)
- Telas já existentes conectadas aos endpoints novos (não foi necessário criar): `AgentScreen.tsx` → `/agents`, `DeployScreen.tsx` → `/deploy/status`, `FlowScreen.tsx` → `/flows`.
- hok-api.ts usa `X-Hok-Token` + `X-Conversation-Id`; trata vazio/erro e nunca inventa campo sem fonte real.
- Build: `PORT=5173 BASE_PATH=/ pnpm run build` (workspace root falha no mockup-sandbox que exige PORT — build direto no `artifacts/hok-os`).
- Deploy em `/var/www/hok-os` (backup: `hok-os.bak_20260815_203500`), bundle novo `index-1nrNflG-.js`.

## Pendências
- `/flows` sem POST (criar/executar requer fluxo de aprovação — a UI marca "Novo Flow"/"Executar" como N/A).
- `/deploy/status` sem histórico real de deploys (apenas nota honesta).
- Agent pause/start não suportado pelo backend (botões disabled na UI).
- Workspace pnpm `mockup-sandbox` quebra o build raiz (exige PORT) — buildar via `artifacts/hok-os` ou passar PORT.

## Problemas encontrados
- **Bug `/deploy/status`**: `git -C ROOT_PATH` (`/root/hokma`) resolvia para o repo raiz de contexto (branch `clean_master`), não para o backend. Corrigido apontando para `ROOT_PATH + "/backend"` + campo `repo` na resposta (commit `7c93439`).
