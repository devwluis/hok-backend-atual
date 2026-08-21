# ADENDO — SESSÃO 21/08/2026 — TERMINAL: BARRA DE TECLAS COMO "INPUT ACCESSORY VIEW" (ESTILO TERMIUS)

Sessão após o `ADENDO_SESSOES_20260820_tema_teclas_seg_modelo_roteamento.md`. Frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), backend em `/root/hokma/backend` (repo `devwluis/hok-backend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082), nginx :3002.

## 1. PROBLEMA (commit `38984dd` → revisado)
A barra de teclas especiais (Ctrl/Alt sticky, Esc, Tab, Ctrl+C/D, setas) existia ancorada
acima do teclado virtual via `visualViewport`, mas o handler do teclado TAMBÉM
redimensionava o terminal (altura dinâmica `kbHeight`) e disparava resize do pty
(`scheduleResizeSend` → `pty.Setsize`/SIGWINCH) a cada abertura/fechamento do teclado.

**Resultado nos testes:** diálogos da TUI do OpenCode desalinhados/cortados, sem scroll —
a sincronização do tamanho do pty com o teclado do Android não funcionou bem.

## 2. NOVA ABORDAGEM — "INPUT ACCESSORY VIEW" (comportamento Termius)
Em vez de sincronizar o resize do terminal com o teclado, a barra passou a ser uma
**accessory view fixa e estática** acima do teclado virtual:

1. **Barra `position: fixed`** ancorada na borda inferior da janela com
   `bottom = kbInset + 8` (kbInset = `window.innerHeight - visualViewport.height` =
   altura do teclado). Como é `fixed`, não participa do layout → o terminal **não
   reflowa nem reposiciona**.
2. **Removida a sincronização do resize do pty com o teclado:** eliminados o `kbHeight`
   (encolhia o container) e o `scheduleResizeSend` no handler do visualViewport. O
   resize do pty agora só dispara em mudança real de tamanho (rotação /
   redimensionamento da janela) via ResizeObserver.
3. **Barra some/desce junto com o teclado:** `showKeysBar = kbInset > 0` — existe
   enquanto o teclado virtual está aberto, independente de estado de foco (evita
   flicker ao tocar nos botões, pois o toque rouba o foco da textarea). Teclado
   fechado → barra desliza para fora (`bottom: -72`).
4. Removido o state `focused` e os listeners focus/blur da textarea (não mais
   necessários).

**Consequência esperada:** com o teclado aberto, a parte inferior do terminal fica
atrás do teclado (a última linha/cursor pode ficar coberta) — inerente a não
redimensionar o terminal. Aceito pelo usuário em favor da estabilidade da TUI.

## 3. VALIDAÇÃO
- `npm run typecheck` OK; `npm run build` OK (bundle `index-CzcdUpPy.js`).
- Teste funcional isolado via Playwright (Chromium, 390×844, mobile emulado):
  - Teclado fechado (vv real): barra `position: fixed`, `bottom: -72px`, invisível.
  - Teclado aberto (vv simulado = 400px): barra `fixed`, `bottom: 452px`
    (= kbInset 444 + 8), visível, grudada 8px acima do topo do teclado.
  - Altura do xterm **idêntica (496px)** nos dois cenários → terminal não redimensiona.
- Teste real do usuário no celular: **aprovado** ("teclado funcionou redondo").

## 4. DEPLOY
- Backup: `/var/www/hok-os.bak_accessory_20260821`.
- Build → `dist/public` → copiado para `/var/www/hok-os/` (nginx :3002).
- nginx servindo 200 (página + bundle novo).

## 5. COMMITS / ESTADO
- Frontend `devwluis/hok-frontend-atual` @ main: **`7c90b41`** (pushado).
- Backend: sem mudança de código nesta sessão.

## 6. PRÓXIMO PASSO (solicitado pelo usuário)
- **Corrigir a rolagem da "barra de mensagem" do terminal**: falta rolar para ver todo
  o contexto/conteúdo descrito no terminal (verificar o comportamento da área de
  output/scroll da tela do Terminal no mobile).

**Data/Hora:** 21/08/2026
**Status:** Deploy validado em produção pelo usuário; commit/push concluídos; pendente a correção da rolagem do conteúdo do terminal.
