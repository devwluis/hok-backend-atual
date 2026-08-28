# Adendo — Sessão 28/08 11:20 · Pendência 4: migração do Google Drive para Service Account (n8n)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** roadmap das pendências (adendos 27-28/08), pedido de migração de Washington (OAuth2 → Service Account).

---

## Motivo

O OAuth2 do Google expirava a cada ~7 dias (app em modo Testing no Google
Cloud; publicação bloqueada por verificação). Migração para Service Account —
credencial permanente, sem expiração.

## O que foi feito

1. **Validação da Service Account (teste direto)**: `aevo-crawler@hok-crm.iam.
   gserviceaccount.com` — JWT → token → listou a pasta **CaixaPreta-Hok** com
   sucesso (permissão Editor conferida por Washington).
2. **Credencial nova no n8n** (criada via banco, encriptada com o Cipher do
   n8n — sanidade testada descriptografando a credencial existente):
   - ID: **`iqQ8uORl5MKkbdgu`** · Nome: "Google Drive Service Account" ·
     Tipo: **`googleApi`** (Google Service Account API nativa do n8n 2.34.4)
   - Data: `email` (client_email), `privateKey`, `scopes` (drive) — mesma
     ownership da antiga (project GWYqscFxkFXwfkUK, role owner)
3. **Referências trocadas nas 4 workflows** (5 nodes `googleDrive`):
   - `enSxay8DwbAurLcj` — **HOK OS — Contexto Claude Terminal** (ativa)
   - `cQp43X754bFkVI5W` — Temp - Listar Arquivos Contexto-Hok (ativa)
   - `KIhlSkw8d86RhTY0` — Temp - Listar Pastas (inativa)
   - `mdzFUoKg0deQWwhh` — Aevo - Drive Crawler → CRM (inativa)
4. **Teste real e prova definitiva**: webhook dos adendos disparado ANTES e
   DEPOIS do restart do n8n (aprovado) — execução pós-restart `success` com a
   definição recarregada do banco (referencia só a `googleApi`) → arquivo
   criado no Drive. **A migração está ativa e provada.**
5. **OAuth2 mantida como fallback** (não excluída — "Google Drive account 2",
   `bdgxPVbfHa0sa3pT`).
6. Backup do banco do n8n: `n8n_db_pre_migracao_SA_20260828_111210.sqlite`.

## Notas
- 2 arquivos de teste ficaram na CaixaPreta-Hok (TESTE_MIGRACAO_SA_20260828.md
  e TESTE_MIGRACAO_SA_POS_RESTART_20260828.md) — a SA não tem permissão de
  deletar (403); remover manualmente se desejado.
- Este adendo foi registrado via o próprio webhook dos adendos — usando a
  nova credencial (validação adicional).

## Status
**Pendência 4 CONCLUÍDA** — todas as 4 pendências do roadmap fechadas
(1: sessão zumbi; 2: OpenTUI conviver; 3: popup/reconnect opção b; 4: SA).

## Próximos itens em aberto (operacional)
- Commits pendentes de sessões anteriores (backend working tree) — aguardando
  aprovação.
- drive_creds.env: a renovação OAuth do refresh token fica SEM necessidade
  (a SA não expira) — manter como está até decidir se o arquivo ainda é
  necessário (icons.go usa o refresh token OAuth).