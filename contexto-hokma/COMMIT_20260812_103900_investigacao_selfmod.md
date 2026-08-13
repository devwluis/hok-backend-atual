# COMMIT — Investigação: motor de auto-edição (self-mod) está conectado?
Data: 2026-08-12 10:39 · Por: opencode · Método: leitura de código + estado real no servidor

## VEREDITO: NÃO conectado em produção (95% construído, 1 bug de roteamento bloqueia)

## O que EXISTE e funciona (evidência)
1. `self_mod.go` — pipeline completo: executeSelfMod = executa → identifica arquivo →
   commit → smoke test → revert → auditoria. 100% implementado.
2. Scripts: selfmod_commit.sh OK · selfmod_revert.sh corrigido em 11/08 (git revert real)
   · smoke_test.sh existe (mas raso: só GET /health).
3. Repo git do tenant: /root/hokma/tenants/owner/.git-worktree com commits de
   teste de unit (6222094, f23b096...). Branch tenant/owner/main, working tree limpo.
4. Detecção dupla:
   - smart_chat.go:392 `isSelfModCommand` (mensagem do usuário contém cmd bash + path hokma)
   - pending_action.go:209 `detectSelfModification` (toolName=bash_exec + cmd/file/path hokma ou write cmds)
5. Tabela auditoria `self_modifications` em memory.db (schema casa com recordSelfMod) — 0 linhas.
6. Binário em produção (04:29) CONTÉM o código (strings executeSelfMod/AUTOMOD = 10 ocorrências).

## O BUG BLOQUEANTE — roteamento por ToolName, não por ActionType
- handleActionDecision (pending_action.go:366-372) faz `switch pa.ToolName`:
  - case "fs_exec","bash_exec" → resolveFsExecPendingAction (linha 368) — SEMPRE CAI AQUI
  - case "self_mod" → executeSelfMod (linha 371) — NUNCA alcançado
- registerFsExecPendingAction (fs_routes.go:345) cria ToolName="fs_exec" + ActionType="self_mod"
- setPendingAction (pending_action.go:276) mantém ToolName="bash_exec" + ActionType="self_mod"
- Resultado: ActionType="self_mod" é calculado e mostrado no diff, mas a aprovação
  roteia pelo ToolName → executa via resolveFsExecPendingAction/ExecuteApprovedCommand
  SEM commit, SEM smoke test, SEM registro em self_modifications.

## Achados secundários
- smoke_test.sh raso (só /health — não valida efeito do patch).
- Tabela self_modifications DUPLICADA e órfã em hokma.db (schema antigo com
  requested_by/reverted_at/reverted_hash) — recordSelfMod grava em memory.db.
- selfmod_commit.sh: `cp "$FILE" tenants/$TENANT_ID/backend/` exige FILE relativo ao
  worktree; path inválido → commit pode pegar arquivo errado.
- Existe self_mod_test.go (teste unitário, usado para gerar os commits de teste).
- hokma.service ativo desde 04:29; memory.db é o banco ativo (DB_PATH default).

## Correção sugerida (1 linha de lógica + validação)
Trocar o switch para usar ActionType quando presente:
  switch pa.ActionType { case "self_mod": executeSelfMod(pa) }
  mantendo fallback por ToolName (n8n/fs_exec/etc). Depois: go build + restart + teste
  e2e (enviar comando de auto-edição pelo chat, aprovar, verificar commit + linha na tabela).
