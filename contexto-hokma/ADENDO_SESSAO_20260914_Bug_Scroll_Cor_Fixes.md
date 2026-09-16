# Adendo — BUG A (scrollback) e BUG B (perda de cor) — Correções

**Origem:** opencode (frontend) | **Data/hora:** 14-09-2026

---

## Backup criado no início como ponto de rollback

- **Arquivo:** `/root/backups/hok_os_frontend_pre_scroll_color_fix_20260914.tar.gz` (19 MB)
- **Commit rollback:** `2dc472f` (branch `xterm-v3`)

---

## BUG A — Sem rolagem no histórico do terminal

### Causa raiz
O overlay de gesto (`data-testid="term-gesture"`) em TerminalXtermScreen.tsx tinha um **deadlock de pointer-events**:
- `pointerEvents: gestureActive ? "auto" : "none"` — começa como `"none"`
- Para virar `"auto"`, precisa receber um toque (`onTouchStart` → `gestureActive = true`)
- Mas toque nunca chega porque `pointerEvents` é `"none"`
- Resultado: **nenhum toque é capturado**, nenhum gesto de scroll funciona, nem mesmo scroll nativo

Além disso:
- `touch:none` (Tailwind `touch-none`) → `touch-action: none` → navegador não faz pan vertical
- Toque sem movimento (tap) não focava o input do terminal

### O que foi corrigido
1. **`pointerEvents: "auto"` sempre** (quando `isMobile`) — remove deadlock, overlay recebe toques
2. **`touch:pan-y`** (Tailwind + inline style) — permite scroll vertical nativo para movimentos pequenos
3. **`onTouchEnd` com foco no input** — tap no terminal foca o campo de input (além de scrollToBottom)
4. **`scrollLines()` para movimentos grandes** — mantido (gesto de swipe rola histórico)

### Arquivo modificado
- `src/components/screens/TerminalXtermScreen.tsx`
  - Linha ~700: gesture overlay CSS/style
  - Linha ~509: `onTouchEnd` — foco no input ao tap

---

## BUG B — Perda de cor ao trocar de aba e voltar

### Causa raiz
Quando abas internas do Terminal trocam via `display: none/block` (TerminalXtermScreen.tsx, efeito "Troca de aba"):
1. `display: none` no container → canvas do xterm.js **perde contexto de renderização** (comportamento conhecido do navegador)
2. Ao voltar (`display: block`), **nenhum `terminal.refresh()` é chamado**
3. Buffer de texto persiste, mas camada de cor não é redesenhada
4. Um clique/interação força reflow → cores retornam

### O que foi corrigido
1. **`terminal.refresh()` no efeito de troca de aba** — após `display: "block"`, força redesenho completo com cores ANSI
2. **`terminal.refresh()` no ResizeObserver** — quando o terminal é redimensionado (teclado abre/fecha, janela muda), também força redesenho

### Arquivo modificado
- `src/components/screens/TerminalXtermScreen.tsx`
  - Linha ~365: ResizeObserver — adicionado `entry.terminal.refresh()`
  - Linha ~389: Efeito de troca de aba — adicionado `entry.terminal.refresh()`

---

## Restrições mantidas
- PTY, tmux, WebSocket, termApi: **inalterados**
- Restyle Termius, status-line fix, tecla travada fix, responsivo desktop×mobile: **mantidos**

## Testes
- Build: ✓ passou
- Smoke test: 0 page errors
- BUG A: gesture overlay funcional em mobile (pointerEvents auto, touch pan-y)
- BUG B: terminal.refresh() chamado ao reativar aba

## Git
- Frontend commit: `25ff713` (xterm-v3, force-pushed)
