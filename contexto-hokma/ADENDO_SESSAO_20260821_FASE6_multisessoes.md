# ADENDO — SESSÃO 21/08/2026 (PARTE 4) — TERMINAL: FASE 6 — MÚLTIPLAS SESSÕES SIMULTÂNEAS (ABAS)

Sessão após o `ADENDO_SESSAO_20260821_UX_fases_1_5.md`. Frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), backend em `/root/hokma/backend` (repo `devwluis/hok-backend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082), nginx :3002.

Autorização explícita do usuário para a Fase 6 ("sim fase 6 pode confirmar") com o processo padrão
(backup/commit de segurança, build+vet+test, smoke isolado, deploy após smoke limpo).

## 1. BACKEND (commits)
- `getOrCreate(userKey, sessionID, created, forceNew)` — `forceNew=true` ignora o session_id e o
  `byUser` e cria SEMPRE um pty novo. O `byUser` vira fallback (conexão sem session_id reattach à
  sessão mais recente). O registro já guardava N sessões (map sessions) — o limite de "1 por
  usuário" era só o byUser.
- `/terminal/ws?new=1` — usado pelo frontend ao abrir uma aba nova.
- `terminal_session_test.go` atualizado para a nova assinatura; build + vet + test (HOK_TOKEN=...)
  OK.

## 2. FRONTEND (commit `13992da`)
- **use-terminal multi-sessão**: `sessionsRef` (Map tabId → sessão) com WebSocket, session_id de
  reattach, conn/note/recent/listeners próprios por aba. Abas persistidas em
  `hokma.terminal.tabs.v1` (`[{id, sid}] + activeId`) com MIGRAÇÃO do session único antigo
  (`hokma.terminal.session.v1`). API com tabId: `write/sendResize/subscribeOutput/.../addTab/
  removeTab/setActiveTab/connect/ensureConnected/teardown`.
  - **Fix de migração**: `readTabs()` é chamado UMA vez (useRef) — chamar 2x criava ids divergentes
    (activeTabId fantasma) por causa do `newTabId()` da migração.
- **TerminalScreen**: barra de abas (Sessão 1/2/... com indicador de conexão, ✕ para fechar quando
  há >1, + para nova) + um **TerminalTabBody por aba** (forwardRef com `focus()`/`openLog()`): cada
  uma com seu xterm, scrollback, modo leitura, long-press, swipe, tema e detecção de TUI. Header
  (status), comandos rápidos, barra de teclas e modificadores sticky globais apontando para a aba
  ativa. Snapshot local do xterm por aba (`hokma.terminal.state.<tabId>`).

## 3. BUG CRÍTICO ENCONTRADO E CORRIGIDO — histórico apagado no reattach
Sintoma: com 2 abas, o reattach da sessão 1 (após reload) perdia o histórico (só o prompt), embora
o scrollback chegasse com o conteúdo correto. Causa: o **SIGWINCH** (de qualquer resize) faz o
bash/readline **redesenhar o prompt com `\x1b[A` + `\x1b[K`** — o redesenho entra no ring do
backend e, ao ser reescrito no xterm, apaga as linhas anteriores do scrollback. Disparos:
1. resize do 1º mount no reattach (o pty já tem o tamanho);
2. **aba oculta**: o fit com host 0x0 propõe 6x13 e o resize era enviado;
3. ao voltar a aba oculta → visível (resize de novo).
Correções no `TerminalTabBody`: 1º resize do mount é só visual; aba oculta NUNCA envia resize;
resize só quando a dimensão real mudou (comparação `cols x rows` com o último enviado).

## 4. VALIDAÇÃO (Playwright + backend de teste 18090)
- 1 aba: comando executa; `+`: 2 abas; comandos independentes SEM vazamento entre sessões;
- voltar p/ sessão 1: conteúdo preservado; reload: 2 abas persistem e AMBAS reattach com histórico;
- fechar sessão 2: volta p/ sessão 1; WS cru: reattach sem input não emite stream (pty ocioso).
- Smoke 100% verde após os 3 fixes; typecheck + build OK.

## 5. DEPLOY / ESTADO
- Frontend: `13992da` pushado; deploy `/var/www/hok-os` (backup `.bak_fase6_20260821`); bundle novo; nginx 200.
- Backend: binário `hokma` substituído (backup `hokma.bak_fase6_20260821`); restart do serviço; health 200; auth WS 401 p/ token errado.
- Fases 1-6 completas. Fase 6 liberada pelo usuário.

**Data/Hora:** 21/08/2026
**Status:** Fase 6 implementada, validada e deployada; commits pushados (frontend `13992da`, backend na sequência).
