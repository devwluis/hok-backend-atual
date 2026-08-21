# ADENDO — SESSÃO 21/08/2026 (PARTE 5) — TERMINAL: 3 CORREÇÕES (SCROLL AO VOLTAR, BARRA COLADA, TOGGLE CTRL/ALT)

Sessão após o `ADENDO_SESSAO_20260821_FASE6_multisessoes.md`. Frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082), nginx :3002.

Commit `95a2d45` (frontend). Arquivo alterado: `TerminalScreen.tsx` (backup `TerminalScreen.tsx.bak_3fixes_20260821`; `/var/www/hok-os.bak_3fixes_20260821`).

## 1. BUG 1 — Posição de scroll não preservada ao minimizar/voltar
- Causa: o TerminalTabBody desmonta ao trocar de tela do app e remonta com o snapshot
  local/recent — o viewport sempre voltava ao fundo.
- Fix: map em memória do módulo `savedScrollY` (tabId → `buffer.viewportY`), salvo no
  `visibilitychange` (hidden) e no unmount; `pendingRestoreRef` aplica `term.scrollToLine()`
  logo após o histórico/scrollback ser reescrito (remount e volta ao visible), ANTES de
  qualquer auto-scroll do onOutput.
- Smoke: `seq 1 200` + rolar até "147" → minimizar/voltar → linha visível continua "147".

## 2. BUG 2 — Gap entre a barra de teclas e o teclado do Android
- Causa: `bottom` tinha `+8px` fixo (respiro adicionado na accessory view) e o `kbInset`
  ignorava o `visualViewport.offsetTop`.
- Fix: `kbInset = innerHeight - vv.height - vv.offsetTop` e `bottom = kbInset` (zero margem).
- Smoke: teclado simulado 444px → `bottom: 444px`; com `offsetTop=60` → `384px` (444−60).

## 3. BUG 3 — Ctrl/Alt como toggle (ligar/desligar com um toque)
- Causa: `ArmedMod` único ("none|ctrl|alt") + `onPointerDown`+`onClick` duplicados nos
  botões — com o sticky isso era inofensivo; com toggle, o duplo disparo se anulava.
- Fix: `ArmedMods {ctrl, alt}` independentes (toggle por botão; Ctrl+Alt juntos → `ESC +
  Ctrl+tecla`); removido o `onPointerDown` redundante; sticky one-shot, lockout de 350ms e
  Esc desarmando tudo mantidos.
- Smoke: armar → desarmar → Ctrl+Alt → desligar independente → tudo desarmado (verde).

## VALIDAÇÃO
- `npm run typecheck` + `npm run build` OK; smoke isolado (Playwright + backend de teste
  18090) 100% verde nos 3 itens.
- Deploy: bundle `index-DjlP5InC.js`; nginx :3002 200; commit `95a2d45` pushado.

**Data/Hora:** 21/08/2026
**Status:** 3 correções validadas, deployadas e pushadas.
