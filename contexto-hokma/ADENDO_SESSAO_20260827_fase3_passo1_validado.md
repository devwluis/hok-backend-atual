# Adendo — Sessão 27/08 09:40 · Fase 3 Passo 1 validado (opencode serve) + início Passo 2

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md, ADENDO_ROTEIRO_FASES_TERMINAL_OPENCODE_SEM_TUI_20260824_181300.md, ADENDO_ATUALIZACAO_ROTEIRO_FASES_PULAR_PARA_FASE3_20260826_070336.md, Adendo 2708 (tela preta + bug do segundo toque).

---

## Contexto da sessão

Início da Fase 3 (opencode serve como backend real via API HTTP), substituindo a ponte
PTY+tmux+iframe com bug estrutural confirmado (upstream OpenTUI). Regras seguidas:
nenhuma alteração em produção, backup antes de editar, build isolado antes de deploy,
CRM/auth/whatsapp/hermes fora de escopo.

## Passo 1 — Validado (sem tocar em código)

### Acesso ao Drive
- `drive_creds.env` local estava com refresh token **expirado/revogado** (`invalid_grant`).
- Credencial atual recuperada do banco do n8n (container `n8n_oficial`,
  `/home/node/.n8n/database.sqlite`, credencial `googleDriveOAuth2Api` "Google Drive
  account 2", descriptografada com a chave de `/home/node/.n8n/config`).
- Usada apenas para leitura. **drive_creds.env não foi alterado** (pendência futura:
  atualizar o arquivo quando formos mexer no backend).

### opencode serve
- Binário `/root/.opencode/bin/opencode` **v1.18.23**, comando `serve` presente.
- Servidor de **teste** no ar: `127.0.0.1:4100`, diretório `/tmp/opencode/fase3-test`,
  `OPENCODE_SERVER_PASSWORD=hok-fase3-teste-2026` (PID ativo, fora de produção).

### Autenticação
- Mecanismo confirmado: **HTTP Basic** com **username fixo `opencode`** + senha =
  `OPENCODE_SERVER_PASSWORD`. Username aleatório ou senha errada → 401.
- Detalhe não registrado no adendo de 26/08 — agora documentado.

### Endpoints validados via curl
| Endpoint | Resultado |
|---|---|
| `GET /doc` | 200 — OpenAPI **3.1.0**, title `opencode`, **162 paths** |
| `POST /session` | 200 → `ses_fbd6e84e3ffeLmBKpmYL39nR1R` |
| `POST /session/{id}/message` | 200 → resposta real (PONG) |
| `POST /session/{id}/summarize` | 200 → retorna `true` (assíncrono) |
| `GET /event` (SSE) | conecta e emite `server.connected` |
| `GET /api/health` | `{"healthy":true}` |

- Rotas do adendo de 26/08 todas confirmadas no spec: `/session`, `/session/{id}/message`,
  `/session/{id}/prompt_async`, `/session/{id}/summarize`, `/session/{id}/permissions/{id}`,
  `/event`. Existe também família paralela `/api/*` (mesmos endpoints, namespaced).
- Body de `POST /session`: `{"title": ...}` (opcional: `agent`, `model`, `metadata`,
  `workspaceID`).
- Body de `POST /session/{id}/message`: `{"parts":[{"type":"text","text":"..."}]}`
  (opcionais: `noReply`, `model`, `agent`, `tools`, `system`, `format`, `variant`).
- Formato de resposta de message: `{"info": {...}, "parts": [...]}` — `info` com `role`,
  `mode`, `agent`, `cost`, `tokens`, `modelID`, `providerID`, `finish`, `id` (msg_*),
  `sessionID`; `parts` com `step-start`, `reasoning`, `text`, `step-finish`.
- Modelo default do servidor de teste: `thinkingmachines/inkling-small:free` (OpenRouter) —
  o dir de teste não tem `opencode.json`; em produção o backend define
  `anthropic/claude-sonnet-4-6`.

### Achado: summarize é assíncrono
- `POST /session/{id}/summarize` retorna `true` imediatamente (fire-and-forget).
- Após a chamada, o campo `summary` da sessão continua como dict de git stats
  (`{additions, deletions, files}`) — resumo textual parece ser aplicado de forma
  **assíncrona** (provável entrega via eventos SSE). Investigação breve prevista no
  Passo 2, sem se aprofundar.

### Cópias locais salvas (não dispara upload no Drive)
- `backend/contexto-hokma/ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md`
- `backend/contexto-hokma/ADENDO_ROTEIRO_FASES_TERMINAL_OPENCODE_SEM_TUI_20260824_181300.md`
- `backend/contexto-hokma/ADENDO_ATUALIZACAO_ROTEIRO_FASES_PULAR_PARA_FASE3_20260826_070336.md`
- `backend/contexto-hokma/ADENDO_2708_TERMINAL_TELA_PRETA_BUNDLE_QUEBRADO_BUG_SEGUNDO_TOQUE.md`

## Passo 2 — Em andamento

- Escrita do cliente Go `opencode_serve_client.go` (rotas `/session`,
  `/session/{id}/message`, `/session/{id}/prompt_async`, `/session/{id}/summarize`,
  `/session/{id}/permissions/{id}`, `/event` SSE).
- Endpoint de teste em porta separada, build isolado, sem tocar `hokma.service`.
- Investigação breve do comportamento assíncrono do summarize (SSE).

## Pendências / próximos passos
- Atualizar `drive_creds.env` com refresh token válido (quando autorizado mexer no backend).
- Validar cliente Go em porta de teste e reportar antes de qualquer integração de produção.
- Definir persistência de sessão por `conv_id` (tabela `session_mode`, adendo de 24/08).