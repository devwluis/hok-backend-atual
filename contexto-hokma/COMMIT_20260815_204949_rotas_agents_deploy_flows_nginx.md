# COMMIT — Rotas /agents, /deploy/status, /flows + conexão do frontend (nginx)

Data: 2026-08-15 20:49 · Por: Washington + Claude Code (sessão local, no servidor)
Escopo: backend Go (`/root/hokma/backend`) + frontend hok-os + nginx (`/etc/nginx/sites-enabled/hokma-web`)
Branch: `main` (backend) · rotas read-only, sem push ainda (aguardando aprovação conjunta)

---

## 1. Contexto e regras da sessão
- **Autonomia alta** (executar sem pausar): investigar/editar código, build/vet/test local,
  testar isolado (porta 8090), **git commit** (um por escopo), gerar/enviar adendo.
- **Autonomia baixa** (parar e aprovar): push, build/substituição em produção, systemctl
  restart, ações irreversíveis, decisões de arquitetura fora do escopo.
- **Nota de coordenação:** outra sessão (opencode) cuida de `auth_routes.go`
  (register/login/me com JWT) em clone separado — **não** mexi nesse escopo.
- Regras registradas na memória do projeto (regra-de-autonomia, coordenação de sessões).

## 2. Rotas novas (commitadas no backend)
- **`68410d4`** `feat`: novas rotas read-only
  - `GET /agents` — monitores de trigger em runtime (levels disco/memória/erros), status
    + last_fired/last_msg; somente leitura.
  - `GET /deploy/status` — branch e commit do repo backend + estado dos serviços systemd
    (hokma/nginx); histórico de deploys ainda não registrado (nota honesta, lista vazia).
  - `GET /flows` — mapeia workflows do n8n para a tela Flow Builder (name/steps/status),
    reaproveitando `fetchN8nWorkflowsItems` (contagem de steps); sem POST.
  - Todas com `requireHokAuth` (401 sem token) + `setCORS`.
- **`7c93439`** `fix`: /deploy/status apontava git para o repo raiz (`ROOT_PATH=/root/hokma`
  no default **não é** o repo do backend). Criado `repoRoot() = ROOT_PATH + "/backend"`,
  campo `repo` na resposta. Confirmado: default `ROOT_PATH` precisa do sufixo `/backend`.

### Validação
- `go build ./...` e `go vet ./...` **OK**.
- **Teste isolado na porta 8090** (não tocou a produção 8082): binário compilado em `/tmp`,
  servidor de teste subiu, todos os endpoints respondem:
  - `/agents` sem token → **401**; com token → 200, 3 monitores.
  - `/deploy/status` → 200, branch `main`, commit `7c93439`, repo `/root/hokma/backend`,
    serviços `hokma`/`nginx` ativos.
  - `/flows` → 200, 1 flow (`self-heal`, status draft).
  - Servidor de teste derrubado e binário removido ao final.

## 3. Conexão do frontend (hok-os) — a peça que faltava era o **nginx**
- As telas `DeployScreen`, `FlowScreen` e `AgentScreen` **já chamam** `/deploy/status`,
  `/flows` e `/agents` via `hokGet` (centralizado em `src/lib/hok-api.ts`), com header
  `X-Hok-Token` (bate com `requireHokAuth`) e Server URL configurável nas Configurações.
- O build servido (`/var/www/hok-os/assets/index-*.js`) já continha as 3 rotas.
- **Problema encontrado:** a config `/etc/nginx/sites-enabled/hokma-web` tem um
  `location ~ ^/(...) { proxy_pass http://127.0.0.1:8082 }` (porta 3002) listando
  explicitamente os paths proxyados. As rotas `agents|deploy|flows` **não estavam na
  regex** → o nginx cairia no `location / { try_files ... /index.html }` e devolveria o
  **HTML do SPA** em vez do JSON do backend, quebrando as telas (silencioso, só visível
  por E2E).

### Correção
- Backup da config → adicionado `agents|deploy|flows` à regex do `location` de proxy.
- `nginx -t` OK → **`nginx -s reload`** (gracioso, não-disruptivo, reversível via backup).
- **E2E validado via porta 3002:** `/agents` e `/flows` com token inválido → **JSON**
  (`{"status":"unauthorized"}`), NÃO o HTML do SPA (proxy → backend funcionando);
  rotas inexistentes (`/qualquercoisa`) → **HTML do SPA** preservado (fallback intacto);
  nginx ativo.

### Lição registrada na memória (rota-backend-exige-regex-nginx)
Toda rota nova do backend **fora de `/api/`** precisa também entrar na regex do `location`
do nginx (3002→8082), senão o SPA devolve HTML em vez do JSON. Ao criar rota nova, incluir
na regex + `nginx -t` + reload.

## 4. Pendências
- ⏸️ **Push pausado** por regra de autonomia baixa — vamos **aprovar tudo junto** ao final
  (backend `main`: commits `68410d4` e `7c93439`; aguardando também push do repo de contexto).
- Frontend: enviar adendo via webhook (este documento) para a pasta contexto no Drive
  (workflow "Contexto Claude Terminal", POST JSON `{"titulo","conteudo","filename"?}`).
- Segurança SaaS da auditoria prévia segue em aberto (8082 exposta sem auth em alguns
  endpoints, OwnerGate client-side, etc.) — não parte deste escopo.
- Arquivos untracked do backend (`.claude/`, `CLAUDE.md`, `skill_state/`, `skills/`,
  `hokma.prev_*`) não pertencem a este escopo — não commitados.

---

*Gerado a partir de sessão real no servidor (15/08/2026). Complementa HOK_MASTER_CONTEXT.md
e os COMMIT_* anteriores da pasta.*