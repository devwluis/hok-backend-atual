# COMMIT — Análise da sessão anterior (11/08 + madrugada 12/08)
Data: 2026-08-12 10:38 · Por: opencode (sessão atual, via sshpass/SSH)

## O que foi feito ontem (11/08, via shell/scripts)
1. `selfmod_revert.sh` CORRIGIDO (12:56): antes só fazia `echo "Revertido..."` (bug);
   agora executa `git revert --no-edit` real + novo commit no worktree do tenant.
   Backup do bug: `selfmod_revert.sh.bak_20260811_125621`.
2. Build de teste `hokma_test` (12:55) validando patch com `runSelfModRevert`/`recordSelfMod`.
3. Instalação do opencode (21:50) + tmux `hokma-code`.

## Sessão opencode "Saudação inicial" (12/08 02:37→04:30, ses_00c2355d...)
### Revisão de código
- 4 subagents @explore (infra/rotas/auth · lógica do agente · integração n8n · DB/CRM/whatsapp)
- ~113 bugs: 8 críticos, 26 altos, 40 médios (RELATORIO_VARREDURA_20260812.md na pasta).
- Vazamentos confirmados: token master + hash OwnerGate no bundle público do frontend.

### Sessão 1 — Segurança: IMPLEMENTADO E EM PRODUÇÃO (binário 04:29, serviço up)
- `roleAuthorized` (utils.go:55): token X-Hok-Token com comparação constant-time
  (crypto/subtle) + JWT exige role owner/admin (401/403) — validei no código ao vivo.
- `/webhook` fail-closed quando N8N_TOKEN vazia (routes.go:576) + compare constant-time;
  N8N_TOKEN nova gerada e adicionada ao .env.
- Timeout no `/terminal`; fix path traversal em `/files`; sanitização `log_file` em `/debug/*`;
  auth + validação de path em `/frontend-loop`; auth em `/n8n/debug` e `/v1/hermes/chat`.
- `N8N_TOKEN=` presente no `.env` (confirmado), HOK_TOKEN rotacionado.

### Agente n8n "hok-saude" (PILAR 2 — especialista)
- Criado em n8n 2.34 (id JBRgJSRbQUX8GlLc, credential OpenRouter T0Xd5mw8fldmAHR5),
  instruções PT-BR; evolução: phi-4 → gpt-oss-20b:free → **deepseek-v4-flash** (final).
- Publicado/ativo (activeVersionId 579aeae8, 05:31) e testado respondendo em PT-BR.
- `availableInMCP=0` — ainda não exposto via MCP.

### Mapeamento MCP Telegram (SEM implementar — próximo passo)
- Workflow "HOK OS Multi IA Telegram" (SFd42XABa4HvpPXT, ativo, 6 nós) chama o HOK
  backend via HTTP ("Call HOK Backend") — NÃO usa o agente hok-saude.
- n8n 2.34 tem MCP nativo (`/mcp-server`, agents como tools via `availableInMCP`)
  DESABILITADO (settings vazio); `n8n-mcp` (czlonkowski, :3100) não conhece agents.
- Caminho canônico planejado: habilitar MCP nativo (`PATCH /rest/mcp/settings`,
  `mcp.access.enabled=true`) OU node "Message an Agent".

## Estado atual confirmado (10:38, ao vivo)
- hokma.service ativo desde 04:29 (build 17842128 bytes); n8n_oficial up 5h.
- self-mod AINDA desconectado: `registerFsExecPendingAction` cria ToolName="fs_exec"
  → switch em pending_action.go:368 cai sempre em `resolveFsExecPendingAction`;
  case "self_mod" (executeSelfMod, linha 372) existe mas nunca é alcançado.
- Tabela self_modifications = 0 linhas. Foco atual: DECISAO_FOCO_SAAS_20260812.md.

## Pendências prioritárias (da análise)
1. Conectar motor de auto-edição (decisão ToolName vs ActionType) — coração do pilar 2.
2. Reforçar smoke_test.sh (efeito real, não só /health).
3. Fechar rotas sem auth na 8082 (/introspect, /status), CORS *, fila device global.
4. Trocar/remover token master + hash OwnerGate do bundle público.
5. Decidir destino do crawler do Drive (cron falha; CRM pausado).
6. Avaliar ponte hok-saude → conversa Telegram (MCP nativo ou "Message an Agent").
