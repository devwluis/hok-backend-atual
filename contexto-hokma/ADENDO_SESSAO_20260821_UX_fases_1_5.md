# ADENDO — SESSÃO 21/08/2026 (PARTE 3) — TERMINAL: FASES UX 1-5 (padrões de clientes SSH mobile)

Sessão após o `ADENDO_SESSAO_20260821_terminal_tui_leitura_adaptativo.md`. Frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082), nginx :3002.

Autorização do usuário: implementar as Fases 1-5 de forma autônoma (cada fase com
commit próprio, backup físico, validação + smoke e deploy fase a fase). Fase 6
(múltiplas sessões simultâneas) NÃO foi iniciada — exige confirmação explícita.

## FASE 1 — Indicador de status de conexão explícito (commit `6b22281`)
- 3 estados visuais no header: **LIVE** (verde) · **Conectando…/Reconectando…**
  (âmbar, com `Loader2` girando) · **Desconectado** (vermelho).
- `everLiveRef` distingue o primeiro connect ("Conectando…") da reconexão após
  queda ("Reconectando…") — reflete o estado real do WebSocket.
- Smoke: LIVE → backend morto → Desconectado → backend de volta → reconexão → LIVE.

## FASE 2 — Copiar texto por long-press (commit `e2d84d0`)
- Long-press (~550ms sem mover >10px) numa linha seleciona a linha inteira
  (`term.selectLines` com `baseY` — a API usa índice do buffer inteiro) e abre um
  menu flutuante "Copiar linha".
- Cópia via `navigator.clipboard.writeText` (fallback `execCommand`) + "Copiado ✓".
- `touchend` após long-press bloqueia o tap do xterm (que limparia a seleção).
- Smoke: seleção persiste (width 375), clipboard com o texto real da linha, swipe
  e tap normal sem regressão.

## FASE 3 — Gestos de swipe nas teclas especiais (commit `be1836d`)
Componente `SwipeKey`: swipe vertical ≥24px dispara a ação uma vez (setinha ↑/↓
pisca); `preventDefault` no touchend evita o click sintético. Toque simples
mantém o comportamento atual. Mapeamento:
- Ctrl ↑ → Ctrl+C (interromper) · Ctrl ↓ → Ctrl+D (EOF)
- Alt ↑ → Ctrl+Alt+G (atalho do terminal/OpenCode) · Alt ↓ → Ctrl+Z (suspende)
- Esc ↑ → Ctrl+L (limpa tela) · Esc ↓ → Ctrl+R (reverse-search)
- Seta ↑ → PageUp · Seta ↓ → PageDown (rolar TUIs/OpenCode)
- Smoke: `sleep 30` + swipe ↑ no Ctrl → interrompido (Ctrl+C); sticky intacto.

## FASE 4 — Temas de cores customizáveis (commit `49c0cdb`)
- 3 temas via opções do xterm (`term.options.theme` em runtime): **Dark**
  (padrão), **Solarized Dark**, **Alto Contraste**.
- Seletor em Configurações (card "Tema do Terminal" com preview de cores);
  persistido em `hokma.terminal.theme.v1` e propagado via `CustomEvent` +
  evento `storage` (outra aba).
- Smoke: troca em runtime (bg rgb(0,43,54) solarized), persistência após reload,
  alto contraste (rgb(0,0,0)).

## FASE 5 — Comandos rápidos customizáveis (commit `4b118be`)
- Botão "✎ Editar" na linha dos atalhos abre painel: mover (↑/↓), remover (✕),
  adicionar comando (input + Enter), restaurar padrão.
- Lista persistida em `hokma.terminal.quick.v1` (máx 24), default = lista atual.
- Smoke: adicionar `git status`, reordenar, remover `uptime`, persistência após
  reload e restauração do padrão.

## DEPLOY / ESTADO
- Fases 1-5 deployadas uma a uma (backups `/var/www/hok-os.bak_fase{1..5}_20260821`).
- Frontend @ main: `6b22281`, `e2d84d0`, `be1836d`, `49c0cdb`, `4b118be` (pushados).
- Bundle atual: `index-*.js` da Fase 5; nginx :3002 200.
- Validações: `npm run typecheck` + `npm run build` OK em todas as fases; smokes
  isolados (Playwright + backend de teste 18090) OK; nenhuma regressão detectada
  nas funcionalidades já validadas (persistência de sessão, scrollback, accessory
  view, modo leitura, modo adaptativo, rolagem manual).

## PENDÊNCIA
- Fase 6 (múltiplas sessões simultâneas — abas Sessão 1/N) **NÃO iniciada**:
  aguarda confirmação explícita do usuário.
- Rolagem manual do terminal: o usuário reportou "quase bom, não 100%" na validação
  anterior (possível instabilidade do servidor ao ler) — re-testar manualmente.

**Data/Hora:** 21/08/2026
**Status:** Fases 1-5 implementadas, validadas e deployadas; Fase 6 bloqueada por confirmação.
