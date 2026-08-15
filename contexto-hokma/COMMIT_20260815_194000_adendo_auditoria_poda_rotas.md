# ADENDO 15/08 — Backend Hokma: auditoria fechada, poda de código morto e novas rotas

**Origem:** Claude Code / opencode (terminal)
**Data/hora:** 15-08-2026 19:40

---

## Contexto
Continuação do fechamento da auditoria 14/08 (backend `/root/hokma/backend`) e da varredura SaaS 15/08. Branch: `hok-backend-atual`.

## O que foi feito

### Segurança (auditorias 14/08 e 15/08)
- `a7848a9` — `getClientIP` agora usa `RemoteAddr` como verdade; só confia em `X-Forwarded-For` via Cloudflare (ranges oficiais). Fecha bypass de rate limit em auth.
- `9680ebb` — n8n keyword agora precede o skill router em `runSmartText`; "crie um workflow" cai no `agent_loop` (`n8n_create_workflow`), não na skill morta `design_automation_customizada`.
- `65a797a` — token morto removido de 3 skills e patch script; auth em `/agent/suggestions`; `handleSettings` constant-time; 6 SQL `fmt.Sprintf` migrados para `sqliteExecParams`.
- `6358f69` — auth em `/n8n/status` (vazava URL interna do n8n) e `whatsappVerifyHandler` em constant-time (`subtle.ConstantTimeCompare`).

### Poda de código morto (mapa SaaS)
- `42e7bda` — remove `pipeline_checkpoint.go` e `n8n.go` (jsonError movido para utils.go) + 28 símbolos sem uso em 10 arquivos; -434 linhas; build/vet/test OK.
- `a486930` — remove `registerFSRoutes`, `registerDebugRoutes`, ramo degraded morto do hermes health; migra `getSQLiteCount` para `sqliteExecParams`.
- `749fe57` — remove `requireHokAuth` duplicado em `handleFileWrite`.
- `79a347f` — consolida `env_tools.go`+`debug_tools.go` em `utils.go` e `skill_state.go` em `skill_exec.go`; remove `skills_handler.go` (skillsDir morta); 5 arquivos viram 3.
- `c35c7c4` — versiona `crm_debounce_test.go` e `pending_action_validate_test.go`; teste hermético via `t.Setenv`.
- `977c443` + `c3130a3` — scripts órfãos movidos para `/root/scripts_legado/`; README limpo.

## Trabalho em andamento (sem commit)
- **Tenant isolation em conversas** (`conversations_routes.go`): GET e upsert filtram por `tenant_id` via JWT, mantendo conversas legadas sem tenant visíveis.
- **`db.go`**: novo `sqliteExecQuoted` + `parseQuotedRows` (formato CSV-style, escapa aspas/newlines) substituindo parsing frágil com `SplitN("|")`.
- **Rotas novas**: `/agents` (`agents_routes.go`, monitores de trigger em runtime), `/deploy/status` (`deploy_routes.go`, git + systemd read-only), `/flows` (`flow_routes.go`, mapeia workflows n8n p/ Flow Builder).
- **`n8n_routes.go`**: extrai `fetchN8nWorkflowsItems` reutilizado por `/n8n/workflows` e `/flows`, com contagem de steps.
- **`settings_routes.go`**: chaves secretas de API nunca mais devolvidas em texto puro — apenas `<chave>Configured: true/false`.

## Estado
- `go build` e `go vet` OK.
- Pendente: commit do trabalho em andamento + push; deploy isolado antes de substituir binário de produção.

## Nota
Token OAuth do rclone (`gdrive:`) expirou — envio deste adendo feito via workflow n8n "HOK OS — Contexto Claude Terminal" (webhook `contexto-hok-terminal`), que usa credencial OAuth própria do n8n para o Google Drive. Reautenticar rclone quando necessário.
