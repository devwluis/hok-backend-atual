# COMMIT — Motor de auto-edição (self-mod) COMPLETADO E VALIDADO EM PRODUÇÃO
Data: 2026-08-12 10:45 · Por: opencode · Binário hokma deployado 10:42

## Correções aplicadas (após investigação COMMIT_20260812_103900)
1. **ROTEAMENTO (bug principal)** — pending_action.go `resolvePendingAction`:
   agora checa `pa.ActionType == "self_mod"` ANTES do switch por ToolName →
   `executeSelfMod` finalmente alcançável (antes caía sempre em resolveFsExecPendingAction).
2. **self_mod.go `executeSelfMod`** — resolve o comando do caminho fs_exec via
   `pendingExecCommands[pa.ID]` (antes chamava executeTool("fs_exec") → erro
   "ferramenta desconhecida"); executa via bashExecTool (timeout 30s + blocklist).
   `runSmokeTest(modifiedFile)` agora passa o arquivo alterado ao smoke script.
3. **fs_routes.go** — `registerFsExecPendingAction(..., selfMod bool)`:
   - /fs/exec (handleExec) → ActionType="fs_exec" (comportamento normal, SEM commit)
   - /chat/smart com isSelfModCommand → ActionType="self_mod" (pipeline completo)
4. **smart_chat.go** — chamada com selfMod=true.
5. **selfmod_commit.sh** — resolução robusta de caminho (absoluto/relativo,
   backend/frontend/hok-os), detecção de "nada a commitar", mensagem com diff stat.
6. **smoke_test.sh** — validação REAL: export PATH (go está em /usr/local/go/bin,
   fora do PATH do systemd), healthcheck + `go build` do backend quando o arquivo
   é .go (timeout 180s) + validação JSON.
7. **hokma.db** — tabela órfã self_modifications (schema antigo) DROPADA.
8. **go test ./...** — backups soltos (security_fix_backup_*, selfmod_fix_backup_*)
   movidos para /root/backups/code_snapshots/ (lição 8 do master context).

## Validação E2E em produção (evidência)
- Detecção: POST /chat/smart com comando de auto-edição → "🔒 ... Aguardando aprovacao"
  com action_type="self_mod" ✓
- Aprovação POST /actions/approve → pipeline completo:
  - id=2: commit 998524c4, smoke_test_passed=1, status="applied" ✓
  - id=1 (1º teste, PATH quebrado): commit e688f43a → smoke FALHOU → git revert
    (c86e346) → status="rolled_back" ✓ (prova o rollback automático)
- Auditoria em memory.db self_modifications (2 linhas) ✓
- Regressão /fs/exec: action_type="fs_exec", execução normal sem commit ✓
- Worktree tenant limpo após cleanup (21d8d7e remove artefato e2e) ✓
- go build + go vet + go test ./... OK · hokma.service ativo ✓

## Fluxo agora funcional (pilar 2 da DECISAO_FOCO_SAAS)
Usuário pede auto-edição → gate de aprovação com diff preview → aprovação →
executa → commit no worktree do tenant → smoke real (build) → rollback automático
se falhar → auditoria na tabela self_modifications.

## Pendências restantes (não-bloqueantes)
- smoke_test.sh só valida .go/.json — frontend .ts/.tsx sem build check
- hokma.prev_20260812_1042 (binário anterior) mantido como fallback
- ExecuteApprovedCommand (caminho fs_exec normal) ainda sem timeout explícito
