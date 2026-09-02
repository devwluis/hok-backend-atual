# ADENDO — SESSÃO 02/09 (rodada 3) — Revisão dos workflows n8n + fix Alerta RAM e Monitor Disk (401) + pendência Google Sheets

Sessão dedicada à revisão da saúde dos workflows do n8n após os fixes de chat/terminal (rodada 2). Nenhum código Go alterado — correções direto no n8n via API v1 (PUT /workflows).

## Estado geral
- 19 workflows no n8n (12 ativos, 7 inativos).
- Canal: backend HOK acessa via `/flows` (n8n_routes.go) e `/n8n/*`.

## 🔴 Problema 1 — "HOK OS — Alerta RAM" falhando a cada 10min (401)
**Causa:** node "Check RAM" (Code node) chamava `http://172.16.0.1:8082/debug/memory` sem autenticação. O endpoint `/debug/memory` não existe mais (substituído por `/debug/resources` que exige `X-Hok-Token` via `requireHokAuth`). Token do workflow também estava desatualizado.

**Fix (via API n8n):**
- Trocado Code node por **HTTP Request node** (`/debug/resources` + header `X-Hok-Token` atualizado de `484bbd…` para `efb654…`).
- Adicionado node **Parse RAM** (Code) para extrair `mem_percent` do corpo (o HTTP node devolve o JSON como string no campo `data`).
- Validado: exec 31258 (18:30) e 31262 (18:40) = **success**, `used_percent: 49` (abaixo do limiar 80 → If não dispara, correto).

## 🔴 Problema 2 — "Monitor Disk and Self-Heal" falhando a cada 6h (401)
**Causa:** token `X-Hok-Token` desatualizado (`484bbd…`). O endpoint em si já era o correto (`/debug/resources`).

**Fix (via API n8n):**
- Token atualizado para `efb654…`.
- `responseFormat: json` no HTTP GET.
- Node Parse corrigido: antes esperava `r.disk.used_percent` (objeto), mas o endpoint retorna `disk_percent`/`disk_avail` flat → agora lê `obj.disk_percent` e `obj.disk_avail`.
- **Validado E2E:** exec 31286 = **success** (HTTP GET 200 → Parse `used_percent: 55`, `free_gb: 44G` → Check >80% não dispara). Validação feita com schedule temporário a cada 1min; restaurado para `3 */6 * * *` após validar.
- **Atenção:** o PUT via API do node HTTP com header precisa incluir `sendHeaders: true` — no 1º PUT o token não persistiu (voltou ao antigo `484bbd…`); corrigido no 2º PUT e confirmado lendo o workflow de volta.

## 🟡 Problema 3 — "CRM - Sync Google Sheets" falhando a cada 2h — PENDENTE (ação manual)
**Causa:** credencial **"Google Sheets account"** (`MiyzH7ZMXBqgAsDR`, googleSheetsOAuth2Api) com refresh token expirado/revogado. Erro: "Access could not be refreshed because the connected account has revoked access, the refresh token expired, or the account password or permissions changed."

**Não corrigível via API** — OAuth2 requer reconnect manual no browser. O usuário irá reconectar o Google Drive/Sheets `gestordeanunciosbr@gmail.com` na UI do n8n (Configurações → Credenciais → Google Sheets account → Reconnect). Após reconectar, validar com execução manual.

## Observações
- Todos os workflows HOK que usam `X-Hok-Token` agora estão com o token atual `efb654…` (verificado em Alerta RAM, Monitor Disk, CRM Webhook Lead, CRM Meta Ads Lead Webhook).
- "HOK OS — Monitor de Saude" e "HOK OS — Contexto Claude Terminal" usam endpoints públicos (sem token) — OK.
- Método de atualização de workflow via API: **PUT** `/api/v1/workflows/{id}` com payload mínimo `{name, nodes, connections, settings}` (PATCH retorna 405; corpo completo do GET retorna 400 "additional properties").

## Arquivos alterados
- Nenhum arquivo .go/.tsx — correções diretas no n8n (via API).
- Este adendo.

## Pendências
- Reconnect manual do Google Sheets (gestordeanunciosbr@gmail.com) no n8n. ATUALIZAÇÃO: usuário reconectou o **Google Drive account 2** (18:52), mas o workflow usa a credencial separada **"Google Sheets account"** (googleSheetsOAuth2Api, id MiyzH7ZMXBqgAsDR, criada 10/07, atualizada 17/07) que continua revogada — reconnect dela ainda pendente.
- CRM Sync Google Sheets PAUSADO pelo usuário (19:0x) — reativar só após reconnect da credencial Sheets.
- Monitor Disk: validado (exec 31286 success); schedule restaurado para `3 */6 * * *`.
- Commit/push deste adendo (branch hok-backend-atual).