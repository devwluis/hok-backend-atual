# AUDITORIA HOK OS — Backend `/root/hokma/backend`

Data: 2026-08-14 · Sessão opencode · Leitura apenas, nada alterado

## FASE 0 — Snapshot

| Item | Resultado |
|---|---|
| git log -30 | 30 commits, últimos: `3569052` (timeout cmd aprovados, hoje), `446b10c` (segurança SaaS), `330b3ef` (fix criação workflow) |
| .go files | **67** arquivos, **16.376** linhas (top: n8n_tools.go 963, agent_loop_groq.go 945, routes.go 893) |
| Binário vs fonte | **OK** — prod `hokma` difere do build fresco em só **197 bytes = metadado VCS embutido** (`446b10c+dirty` vs `3569052+dirty`). Binário atualizado com o fonte (deploy de hoje 18:59 inclui o fix de timeout). `go version -m` confirma |
| TODO/FIXME | 6 (whatsapp_routes.go:15/231/235, pending_action.go:45, agent_loop_groq.go:784, smart_chat.go:419) — nenhum crítico |

⚠️ Higiene de repo: `git status` mostra binários backup soltos (`hokma.pre_security_fix`, `hokma.prev_*`, `hokma_app`, `hokma_test`), arquivos `.bak.*`, `patch*.b64`, `skill_state/`, `.claude/` — tudo não versionado, deixando o build sempre `+dirty` e inchando o working tree.

## FASE 1 — Segurança

**Rotas: 66 em `main.go:75-159` + CRM/WhatsApp (`crm_routes.go:52-59`) + `/n8n/debug` (init, `debug_n8n.go:483`) + `/v1/hermes/*` (init, `hermes_route.go:35-36`).**

Auth — 58 handlers com `requireHokAuth`/JWT/owner. Públicas por design (justificadas): `/health`, `/auth/register|login|owner-check` (rate-limited 10/min, `auth.go:92/156/262`), webhooks WhatsApp (verify token) e `/webhook` (valida `N8N_TOKEN` constant-time, `routes.go:570-585`). Exceções verificadas e OK: `handleMe` usa JWT Bearer (`auth.go:215`), `/conversations` usa `requireOwnerToken` (`conversations_routes.go:153`).

| Achado | Severidade | Ref |
|---|---|---|
| **`handleAgentSuggestions` SEM auth** — expõe `lastSuggestion` do agente proativo (dado interno) publicamente | MÉDIO | `proactive_agent.go:164-178`, rota `main.go:158` |
| **`getClientIP` confia cegamente em `X-Forwarded-For`** → rate limit dos 3 endpoints de auth pode ser **bypassed** rotacionando o header → brute force em owner-check ilimitado | **ALTO** | `utils.go:16-22`, usado em `auth.go:92/156/262` |
| `handleSettings` compara token com `!=` (não constant-time) e com lógica própria, divergente de `roleAuthorized` | BAIXO | `settings_routes.go:53` |
| SQL via `fmt.Sprintf` + escape manual `'→''` em **6 pontos** — não-injetável em SQLite (escape `''` é correto), mas viola o padrão `sqliteExecParams` da CLAUDE.md e é frágil | SUSPEITO | `routes.go:112`, `202`, `203`, `564`, `652`, `657` (webhook memory_get) |
| `getSQLiteCount` com allowlist — **OK** | OK | `db.go:90-109`, allowlist em `db.go:91` |
| Secrets hardcoded em `.go` — **nenhum** (0 ocorrências sk-/hex/ghp) | OK | — |
| **`automation.go` está arquivado corretamente** — deletado em `b6275a9`; só ref viva é um comentário (`n8n_shared_utils.go:41`). Rota `/automation/design` só existe em `.bak` | OK | `main.go.bak.20260810173830:148` |
| **`hok-api-2026` hardcoded em 3 skills VIVAS** (curl com `X-Hok-Token: hok-api-2026`). Testado: **401 hoje** (token morto), mas é segredo admin que já foi válido, está em disco e em backup SQL | **ALTO** | `skills/design_automation_customizada.md:16`, `skills/Testar Endpoint POST.md:6`, `skills/Testar API Hokma.md:6` |
| `backups/memory_backup.sql:13581` guarda token em `app_settings` — backup com segredo em disco | MÉDIO | `backups/memory_backup.sql:13581` |

## FASE 2 — Roteamento LLM

**Caminhos**: `/chat/smart` → `handleSmartChat` (`smart_chat.go:24`) → `runSmartText` (`smart_chat.go:264`). Ordem real de prioridade:

1. `containsSecurityKeyword` → DeepHat (`smart_chat.go:270`)
2. **`trySkillForMessage` (skill router) — `smart_chat.go:278`**
3. `containsN8nKeyword` → `RunAgentLoop` (n8n agent, `smart_chat.go:281`; engine em `agent_loop_groq.go:583`)
4. Claude Code (`smart_chat.go:294`)
5. Hermes (`smart_chat.go:316`)
6. Chat padrão `routeModel` (`smart_chat.go:346`)

O path **CRM** (`crm_ai.go`) é separado: `aiReplyHandler` (`crm_ai.go:343`, rota `POST /crm/leads/{id}/ai-reply`, com auth), modelo `CRM_AI_MODEL` (minimax-m3, `crm_ai.go:113`). **Hermes-gateway**: `/v1/hermes/chat` com auth (`hermes_route.go:59`) + `callHermes` como fallback no chat.

**BUG REPRODUZIDO (criar workflow → skill morta)**: decisão em `smart_chat.go:278` — o skill router roda **ANTES** do checador de keyword n8n (linha 281). A skill `design_automation_customizada.md` tem "cria um workflow" e "automatiza" na seção "Quando usar" (linhas 11-12) → o LLM do router escolhe ela → cria pending action cuja ação é `curl http://127.0.0.1:8082/automation/design -H "X-Hok-Token: hok-api-2026"` (linha 16) → **rota morta** (arquivada) → ao aprovar, nada acontece, e o `RunAgentLoop`/`n8n_create_workflow` (com o fix `maxConsecutiveValidationFails=2`, `agent_loop_groq.go:635`) **nunca é atingido**.

Fixs do commit `330b3ef` (validação/retry) estão intactos em `agent_loop_groq.go:680-695`, `pending_action.go:481` — funcionam, mas ficam invisíveis porque a skill intercepta antes.

## FASE 3 — Testes

Existem **9** arquivos `_test.go`: `guardrail_workflowid`, `mcp_n8n_validate`, `n8n_xml_guard`, `parser`, `self_mod`, `tenant_isolation`, `tenant_isolation_e2e`, `crm_debounce`, `pending_action_validate` (2 últimos não versionados). Com `HOK_TOKEN` set: **`go test ./...` → OK (4.16s)**. Sem env o pacote nem compila o init (`main.go:50` log.Fatal) — detalhe: teste local precisa de env. Não há cobertura para **rota de skill router / `trySkillForMessage`** nem para o **webhook `/webhook`**.

## Lista priorizada (corrigir antes de tocar no frontend)

**CRÍTICO**
1. Skill morta `design_automation_customizada.md` interceptando "criar workflow" — remover a skill ou trocar a ação para o fluxo `n8n_create_workflow` (`smart_chat.go:278` é a decisão). É o bug ativo reportado.

**ALTO**
2. `hok-api-2026` hardcoded em 3 skills (`.md:16`, `Testar Endpoint POST.md:6`, `Testar API Hokma.md:6`) — remover e nunca embutir token.
3. `getClientIP` confiar em `X-Forwarded-For` → rate limit bypassável (`utils.go:16`).

**MÉDIO**
4. `handleAgentSuggestions` sem auth (`proactive_agent.go:164`).
5. `backups/memory_backup.sql` com token em disco (`:13581`).
6. Migrar os 6 `fmt.Sprintf` SQL restantes para `sqliteExecParams` (`routes.go:112/202/203/564/652/657`).

**BAIXO**
7. `handleSettings` comparação não constant-time (`settings_routes.go:53`).
8. Higiene do repo: limpar binários/backups soltos e versionar/remover os `_test.go` untracked.