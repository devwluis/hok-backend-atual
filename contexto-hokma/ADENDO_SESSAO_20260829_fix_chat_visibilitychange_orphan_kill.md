# Adendo — Sessão 29/08 21:xx · Bug do Chat: retorno some na troca de aba e "pensando" fica órfão

**Origem:** opencode (terminal) **Data/hora:** 29-08-2026 21:5x
**Sintomas reportados (Washington):**
1. Envio comando → Hok fica "pensando" normal → troco de aba → volto → retorno some (some visual, histórico ok).
2. Modo autônomo (e autônomo total): efeito "pensando" para mas a IA continua trabalhando em segundo plano — não sei se parou ou se continua.

---

## Investigação

### Descobertas

1. **O sistema de jobs assíncronos JÁ EXISTIA** desde a Fase Maior (27/08) — `chat_jobs.go` (startChatJob com `context.Background` + 10min timeout + persistência em `conv_messages`), `GET /chat/job` para polling, frontend com `AbortController`, `async: true` no body, `useEffect([conversationId])` de retomada ao trocar conversa.
2. **O frontend em produção** é `/root/hokma-web/artifacts/hok-os/src/components/screens/ChatScreen.tsx` (não `/root/hokclaw-frontend/`, que tem código quebrado e é uma cópia paralela não usada).
3. **O bundle em `/var/www/hok-os/assets/index-BPQg1Axo.js`** era de 14:20 (hoje cedo) e foi reconstruído nesta sessão.

### Causa raiz do Bug 1 (retorno some ao trocar aba)

O `useEffect([conversationId])` que retoma o polling **só dispara quando o `conversationId` muda**. Se você troca de aba (Tab no Chrome) e volta, sem trocar de conversa, o efeito NÃO dispara — a bolha do assistente continua "pendente" mas o polling está parado. Resultado: `setLoading(false)` foi chamado pelo AbortError (linha 1024-1032 do `try/catch`), a bolha some, o job continua rodando no backend até 10min.

### Causa raiz do Bug 2 (modo autônomo órfão)

O `chat_jobs.go:67` usa `context.Background()` com 10min — **o job SEMPRE sobrevive** à desconexão. Isso é desejado (Fase Maior), mas dá a impressão de "órfão" porque a UI perdeu o vínculo. No modo autônomo total, o LLM pode levar minutos e o usuário não vê progresso — combina com o sintoma reportado.

Adicionalmente, o caminho **síncrono** (`POST /chat/smart` sem `async: true`, ou `POST /` direto via `handleRoot`) **NÃO passava o `r.Context()` até o `exec.CommandContext`** — se o cliente HTTP desconectasse, o `claude`/`opencode` CLI continuava rodando até o timeout interno de 5min, e o resultado era descartado (response writer fechado). Esse caminho síncrono ainda existe para casos legados e estava **invisível** porque a maior parte do tráfego já é `async:true`.

---

## O que foi feito

### Backend (mudanças defensivas — não mudam comportamento visível do async)

1. **`autonomous.go`**: novo registry `autonomousJobs` (sync.Map de jobID → {convID, agent, pid, started, cancel}). Funções:
   - `autonomousJobRegister(jobID, convID, agent, pid, cancel)`
   - `autonomousJobUnregister(jobID)`
   - `autonomousJobKillByConv(convID) → int` (cancela todos da conversa, loga `[AUDIT] orphan kill`)
   - `autonomousJobWatchOrphan(ctx, convID)` — goroutine que vigia `ctx.Done()` e mata os jobs da conversa quando o request context cancelar.
   - Cinto-e-suspensórios para o caminho síncrono — o principal continua sendo o `CommandContext` recebendo o ctx do request.

2. **`smart_chat.go`**:
   - `autonomousJobWatchOrphan(r.Context(), convId)` adicionado no início do `handleSmartChat` (linha ~52).
   - `tryClaudeCode(ctx, ...)` e `tryOpenCode(ctx, ...)` agora recebem e propagam `ctx`.

3. **`claude_code_client.go`**:
   - `callClaudeCode`, `callClaudeCodePlan`, `callClaudeCodeAutonomous`, `callClaudeCodeApproved`, `runClaudeCodeCLI`, `runClaudeCodeWithModel` agora recebem `ctx context.Context` (primeiro arg).
   - `runClaudeCodeWithModel` usa `context.WithTimeout(parentCtx, claudeCodeTimeout)` — se o parent cancelar (cliente desconectou), o timeout derivado também cancela e o `exec.CommandContext` mata o processo claude CLI.

4. **`opencode_client.go`**:
   - `callOpenCode`, `callOpenCodeApproved`, `callOpenCodeAutonomous`, `callOpenCodePlan`, `runOpenCodeCLI` agora recebem `ctx context.Context`.
   - Mesma mecânica do claude: parent ctx + timeout derivado.

5. **`pending_action.go`**:
   - `resolvePendingAction`, `resolveClaudeCodePendingAction`, `resolveOpenCodePendingAction` agora recebem `ctx context.Context` e propagam para `callClaudeCodeApproved` / `callOpenCodeApproved`.
   - Resolve Aprovação/Rejeição morre junto com o request se o cliente desconectar (evita executar CLI orfão).

6. **`agent_loop_groq.go`**:
   - `executeTool(ctx, name, argsJSON)` agora recebe ctx e propaga para `callClaudeCodeApproved(ctx, prompt)` no caso `claude_code`.

7. **Tests ajustados** para a nova assinatura: `tenant_isolation_e2e_test.go`, `tenant_isolation_test.go`, `guardrail_workflowid_test.go`, `opencode_client_test.go`, `pending_action_fix_test.go`. Todos passam `context.Background()` (testes não precisam de cancelamento real).

### Frontend (mudança principal — resolve o Bug 1)

8. **`/root/hokma-web/artifacts/hok-os/src/components/screens/ChatScreen.tsx`**:
   - Novo `useEffect([conversationId])` que registra um listener `document.visibilitychange`. Quando a aba volta a ficar visível:
     - Se `loadingRef.current` é true (envio ativo), deixa o polling em andamento cuidar.
     - Senão, chama `GET /chat/job?conv_id=X`. Se há job `running`, reanexa o polling silenciosamente (recria `AbortController`, atualiza UI com `setLoading(true)`). Se há job `done`, traz a resposta.
   - É independente do `useEffect([conversationId])` existente — cobre o caso "trocar de aba sem trocar de conversa".

### Build & deploy

9. **Backend**: `go build -o hokma_test .` (em `/root/hokma/backend/`) — compila limpo. `go vet ./...` — sem warnings. **Binário `hokma_test` está pronto** mas NÃO está em produção (substituição do `hokma` rodando + restart do systemd ainda pendente de aprovação).

10. **Frontend**: `PORT=3002 BASE_PATH=/ npx vite build` — compila em 5.59s. Bundle copiado para `/var/www/hok-os/`:
    - `index.html` atualizado
    - `assets/index-BqOXiLk4.js` (novo, contém `visibilitychange`)
    - `assets/index-BhuOhFku.css` (mesmo hash, sem mudança)
    - Backup do bundle antigo em `assets.bak_20260829_21xx_pre_visibilitychange/`

---

## O que NÃO foi feito (decisões)

- **NÃO** adicionei `DELETE /chat/job?id=X` para kill switch via API — o caminho async já tem timeout 10min no backend e o frontend aborta localmente. Adicionar kill switch é mudança de escopo maior; pode ser feito em sessão futura se aparecer caso real.
- **NÃO** troquei `chatJobAsyncTimeout` para 30min — 10min é conservador e evita jobs zumbis. Modo autônomo total que estourar isso já tem `autonomousMaxDuration = 10min` no circuit breaker.
- **NÃO** mexi em `/root/hokclaw-frontend/` (cópia paralela quebrada, com `TerminalTTYDScreen.tsx` apontando para arquivos inexistentes — `tsc --noEmit` falha). O repo de produção é `/root/hokma-web/artifacts/hok-os/`.

---

## Status

**Pronto para produção** (com as ressalvas de sempre):
- ✅ Backend compila (`go vet` limpo, `go build` OK)
- ✅ Frontend compila (`tsc --noEmit` limpo, `vite build` OK)
- ✅ Bundle novo deployado em `/var/www/hok-os/`
- ⚠ **Backend `hokma_test` ainda NÃO está em produção** — aguardando aprovação para `cp hokma_test hokma && systemctl restart hokma` (regra do CLAUDE.md).
- ⚠ **Working tree tem mudanças não commitadas** — adendos anteriores, `.gitignore`, e agora os 6 arquivos Go + 1 arquivo TS deste adendo.

## Próximos passos sugeridos

1. Você testar a nova UI (trocar de aba com chat em modo autônomo e voltar) — confirmar que o retorno agora aparece.
2. Se aprovado, parar `hokma`, copiar `hokma_test` sobre `hokma`, reiniciar — verificar log `[AUDIT] orphan kill` quando reproduzir o cenário.
3. Commit local das mudanças (push só com aprovação explícita).