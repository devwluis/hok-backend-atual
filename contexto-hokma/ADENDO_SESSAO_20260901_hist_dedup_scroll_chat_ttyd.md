# ADENDO — SESSÃO 01/09/2026 — Terminal ttyd mobile: scroll full-area + chat input funcional + histórico sem duplicação

Consolidado das correções aplicadas nesta sessão no terminal HOK (ttyd) para mobile.

## Contexto
O terminal ttyd (opencode/claude em TUI fullscreen dentro de iframe cross-origin)
tinha três problemas relatados pelo usuário em testes no celular:
1. Scroll das conversas não funcionava com o dedo.
2. O campo de chat da TUI não recebia o toque — teclado não abria / cursor não entrava.
3. O histórico (modal) mostrava as mensagens duplicadas.

## Causa raiz

### Scroll + chat (frontend `TerminalTTYDScreen.tsx`)
O iframe ttyd é **cross-origin**: o touch nunca chega ao pai e o xterm.js não
converte touch em mouse report. Uma camada de gesto (`pointer-events:auto`
cobrindo a área toda) era necessária para traduzir o swipe em copy-mode do tmux.

O problema: essa camada **bloqueava o tap** que deveria chegar ao campo de chat
da TUI. Cada tentativa anterior criava um trade-off (ou scroll, ou chat).

**Solução final (v7)**: no `touchend` de um **tap** (sem movimento), o overlay
desliga o próprio `pointer-events` **sincronamente** (style direto no DOM) antes
de o navegador gerar o **click sintético trusted** → o clique cai no IFRAME
(campo de chat da TUI) → o teclado abre e digita. No **arrasto**, o overlay
captura e traduz em copy-mode (scroll full-area). O overlay religa após ~90ms.

- `disarmGestureOverlay()` / `armGestureOverlay()` em `onGestureEnd`/`onGestureStart`.
- Build deployado: `index-B3pAlODg.js` (assets em `/var/www/hok-os`).

### Histórico duplicado (backend `terminal_routes.go`)
O helper `tmux-capture.sh` grava um **snapshot COMPLETO da tela** a cada mudança
(separado por headers `--- ISO ---`). Numa conversa, cada snapshot novo contém a
tela inteira acumulada → concatenar todos os blocos fazia cada mensagem aparecer
em vários snapshots seguidos.

**Solução**: função `dedupLogLines()` — mantém apenas o **último snapshot**
(o estado mais recente da tela, que já embute a conversa visível), preservando o
header inicial. Zero repetição. Usada no GET `/terminal/ttyd/log` quando `since`
não é informado.

## Arquivos alterados
- `backend/terminal_routes.go` — `dedupLogLines()` + uso no handler do log.
- `web/artifacts/hok-os/src/components/screens/TerminalTTYDScreen.tsx` — overlay de
  gesto v7 (tap → click sintético ao iframe; drag → scroll full-area).

## Builds / deploys
- Backend `hokma` reiniciado (systemd) com o binário novo.
- Frontend deployado em `/var/www/hok-os` (nginx).
- Backup do código: `terminal_routes.go.bak_20260901_151134_hist_dedup` e backup
  do deploy em `/root/backups/hokos_frontend_20260901_chatinput_fix`.

## Testes (usuário, celular)
1. Scroll full-area com o dedo no conteúdo — ✓ funcionando.
2. Toque no campo de chat → teclado abre e digita — ✓ funcionando.
3. Histórico sem mensagens duplicadas — ✓ a princípio.

## Pendências / observações
- Se em conversas MUITO longas o dedup por último snapshot não bastar (conteúdo
  antigo saiu da tela), implementar dedup por delta entre snapshots no backend.
- Commit/push pendente desta sessão.