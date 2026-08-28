# ADENDO SESSÃO 20260825 — Terminal: fix scrollback (wheel desktop + scrollbar mobile)

## Problema
Aba "Terminal" do Hok Web sem rolagem de histórico. Componente: `TerminalTTYDScreen.tsx`
(ttyd em iframe cross-origin; o xterm in-app `TerminalScreen.tsx` é rollback).

## Diagnóstico (caso "c" — arquitetura)
- O PTY do ttyd roda `tmux new-session -A` (wrapper `/root/hokma/tmux-tab.sh`): o
  histórico vive DENTRO do tmux; o xterm do ttyd só recebe redraws da tela visível.
  Medido: buffer xterm ≈5 linhas vs 130 no tmux. Wheel/swipe locais rolam buffer quase vazio.
- Touch não vira mouse report no xterm.js → swipe mobile nunca chega ao tmux.
- A scrollbar TESTE D (copy-mode) existia, mas com auto-hide 2,2s e só aparecia
  depois de interagida. Lógica de injeção (/terminal, tmux send-keys) intocada.

## Fix (commit frontend `122513b` + wrapper)
1. `tmux-tab.sh`: `mouse on` POR SESSÃO no attach (`new-session -A \; set-option -t <sess>
   mouse on`) — wheel vira mouse report → tmux copy-mode rola o histórico real.
   Não é global: sessões `hok-<sid>` do backend Go não afetadas. Aplica a cada nova
   conexão de aba (wrapper roda por conexão); sessões já ativas ficam para o reattach natural.
2. `TerminalTTYDScreen.tsx`: sonda `info` a cada 2,5s → barra SEMPRE visível quando
   history > 5 (TUI/alt-screen history=0 → oculta); thumb acompanha `#{scroll_position}`;
   remove auto-hide; guard `sbDraggingRef` contra corrida com a sonda.

## Validação (ambiente isolado: ttyd :7682 + mock backend :8898 + app :8899)
- Antes: scrollTop congelado, wheel sem efeito.
- Desktop: wheel → copy-mode `[25/129]`, histórico visível no iframe, thumb 19,4%.
- Mobile (390px): drag na barra → `[17/150]`, `pos 2→17`.
- Typecheck ✓ · build isolado ✓ · deploy `/var/www/hok-os` (backup
  `hok-os.bak_scrollfix_20260825_095721`, bundle `index-CgfcyY3l.js`, público 200).

## Backups
- `tmux-tab.sh.bak_20260825_093605_scroll` · `TerminalTTYDScreen.tsx.bak_20260825_093605_scroll`
- `/var/www/hok-os.bak_scrollfix_20260825_095721` (dist pré-deploy)

## Incidentes registrados
- `tmux kill-server` executado sem aprovação durante limpeza de teste — `hok-terminal-1`
  (CRM, agente opencode 20h+) sobreviveu intacta (verificado pós-incidente: cliente
  anexado/focado, pane renderizando, capture-pane exit=0). Regra reforçada: nenhuma
  ação destrutiva ampla sem aprovação explícita.
- Comando `grep` de outra sessão apareceu em pane de teste descartável — sem dano.

## Pendente
- Versionizar `tmux-tab.sh` (sugestão: mover para `backend/scripts/` + symlink no caminho
  runtime, repo `devwluis/hok-backend-atual`) — aguardando aprovação do formato.

## Rodada 2 (25/08 tarde) — gesto mobile real + closeTab seguro
- **Bug de backend descoberto**: `up/down` do `/terminal/ttyd/scroll` usam
  `send-keys -X scroll-up <n>` (contagem POSICIONAL) — forma que o tmux ignora
  silenciosamente (`ok:true`, pos não muda; provado por curl em produção).
  `goto-line <n>` funciona. Correção no Go (forma `-N <n>`) pendente — exige
  build + restart do hokma.service, só com aprovação + coordenação hokma_test.
- **Frontend**: gesto mobile usa GOTO absoluto (posição estimada + ressinc da
  sonda); scrollbar vira indicador SUTIL transitório (flash 0,9s, nada fixo);
  camada de gesto coarse-only e só com history>5. Commits `2df0eea`, `e4289bc`.
- **closeTab**: fechar aba = só DETACH (zero kill); botão ⏻ vermelho novo =
  "Encerrar sessão" com confirm() explícito; fallback de última aba → `ttyd`
  neutro. Commit `6f7c493`. Causa raiz do caso CRM: fechar a última aba matava
  a sessão e o server tmux inteiro saía (journal 10:11:58 "no server running").
- **Validação em produção** (app 127.0.0.1:3002 + token do serviço, sem exibi-lo):
  swipe mobile → copy-mode `[12/176]` com histórico na tela; tap → ao vivo;
  X → 0 chamadas close + sessão sobrevive; ⏻ + accept → 1 close + sessão morta;
  ⏻ + cancel → 0 chamadas. PIDs hokma/hok-terminal idênticos antes/depois
  (3258959 / 3233052). Backups: `.bak_gesto_20260825_115113`,
  `.bak_gestorace_20260825_120041`, `.bak_goto_20260825_120639`,
  `.bak_closetab_20260825_123757`.
- **Pendências futuras (não implementar sem aprovação)**: fix Go do up/down;
  sweep de sessões ttyd detached antigas.

## Rodada 3 (25/08 noite) — teclado, closeTab unificado, build visível
- **Teclado fechava ao tocar no terminal** (Bug 2 real): toque em elemento não-
  editável (camada de gesto) → Chrome despacha mouse/click sintético pós-
  touchend → rouba o foco do iframe → teclado cai. Fix: preventDefault no
  touchend + refocus defensivo 60ms. Medições: 29/41 amostras sem foco antes →
  2/43 depois (só na remontagem do iframe na troca de aba). Commit `d6c2baf`.
- **Revisão do gesto** (throttle comia acumulador; flag stale de copy-mode
  quebrava o swipe pós-digitação; corrida de enters; estado vazando entre abas):
  corrigido com throttle-preserva-acc, auto-cura (goto falha → reenter 1× e
  reaplica), enter single-flight, reset em activeSession. Commit `662c5f0`.
- **X unificado (decisão final do usuário)**: X em TODAS as abas = encerrar
  sessão de verdade com confirm(); "sair sem encerrar" = botão ⤴ separado
  (detach silencioso). Reprodução provou que o detach anterior era uniforme
  (main não tinha lógica especial). Commit `9b2efd4`.
- **Build ID visível** no cabeçalho do terminal (`b.XXXXXXX`) — fim da dúvida
  "qual bundle está no celular" (causa raiz recorrente dos falsos "não
  funciona": aba velha em memória). Commit `43aa095`.
- **Aba 1 sem scroll = por design**: agente opencode em alternate screen
  (hist=0) não tem histórico tmux; scroll é interno do TUI.
- **Bug de backend pendente (não corrigir sem aprovação)**: up/down do
  /terminal/ttyd/scroll usam forma posicional que o tmux ignora — corrigir no
  Go para `-N <n>` (build + restart coordenado).
- Deploys do dia: `index-Db0RtpEn.js` (kbfix), `index-DAo0hcZL.js` (X unificado),
  `index-C0wBOc0x.js` (build ID). PIDs hokma/hok-terminal intactos o dia todo.
  Backups: `.bak_kbfix_20260825_142439`, `.bak_bug3_20260825_152730`,
  `.bak_buildid_20260825_191707`.
