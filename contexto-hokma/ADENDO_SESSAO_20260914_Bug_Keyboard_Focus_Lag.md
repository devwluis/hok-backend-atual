# Adendo — BUG C — Atraso/travamento ao abrir teclado virtual

**Origem:** opencode (frontend) | **Data/hora:** 14-09-2026

---

## Backup criado no início como ponto de rollback

- **Arquivo:** `/root/backups/hok_os_frontend_pre_scroll_color_fix_20260914.tar.gz` (19 MB)
- **Commit rollback:** `2dc472f` (xterm-v3)

---

## BUG C — Atraso/travamento ao abrir teclado virtual (mobile)

### Causa raiz

`onTouchEnd` no gesture overlay chamava `.focus()` no input do terminal em **cada toque**, inclusive durante gestos de scroll com movimentos pequenos:

1. **Perda de scroll nativo:** `onTouchMove` usava threshold per-interação (`Math.abs(dy) < 10`). Movimentos pequenos consecutivos (< 10px cada) **nunca** marcavam `g.moved = true`, mesmo com deslocamento cumulativo grande. Resultado: `onTouchEnd` via `!g.moved` → `.focus()` era chamado DURANTE o scroll.
2. **Competição de recursos:** `.focus()` disparava ao mesmo tempo que `visualViewport` resize (abertura do teclado) → `recalcTerminal()` → `terminal.refresh()` + `fitAddon.fit()` → travamento aparente.
3. **Sem debounce:** Toques rápidos múltiplos geravam múltiplos `.focus()` concorrentes.

### O que foi corrigido

| Mudança | Detalhe |
|---------|---------|
| **Tracking cumulativo** (`gestureAccRef`) | Acumula TODOS os movimentos verticais, mesmo os pequenos. `g.moved = true` quando acumulado ≥ 15px |
| **Duração do toque** (`gestureT0Ref`) | Só `.focus()` se toque durou < 300ms (tap genuíno) |
| **Debounce 100ms no `.focus()`** | Evita competição com layout/resize durante abertura do teclado |
| **Cleanup no unmount** | Timer de debounce é limpo quando componente desmonta |
| **Limiar 15px cumulativo** | Distingue tap real (< 15px) de swipe (> 15px) mesmo com movimentos pequenos |

### Lógica de decisão em `onTouchEnd`

```
if (!g.moved && duration < 300ms) {
  // TAP genuíno → debounce .focus() 100ms
} else {
  // SWIPE ou LONG-PRESS → preventDefault
}
```

### Fluxo corrigido ao abrir teclado

1. Usuário toca terminal → `touchstart` (reset accumulators, timestamp)
2. Dedo se move 2-3px → `touchmove` (acumula, < 15px → `g.moved = false`)
3. Dedo levanta → `touchend` (`!g.moved && < 300ms` → tap → debounce .focus 100ms)
4. 100ms depois → `.focus()` → teclado virtual abre SEM competir com scroll
5. Teclado abre → `visualViewport` resize → `recalcTerminal()` → ajusta altura

### Arquivo modificado
- `src/components/screens/TerminalXtermScreen.tsx`
  - Linha ~189: cleanup do focusDebounce timer no unmount
  - Linha ~491-529: gesture handlers com tracking cumulativo + debounce

## Restrições mantidas
- PTY, tmux, WebSocket, termApi: **inalterados**
- BUG A (scrollback), BUG B (perda de cor), tecla travada, responsivo: **mantidos**

## Git
- Frontend commit: `859d5e3` (xterm-v3, force-pushed)

---

## BUG C — Detalhe adicional: barra de teclas ancorada ao teclado (sem delay)

### Problema (parte 1 — posicionamento debounced)
A barra de teclas especiais (Ctrl, Esc, shift+tab, etc.) não acompanha o teclado virtual do Android em tempo real — há um atraso visível e um gap temporário entre o teclado e a barra ao abrir/fechar, diferente do comportamento do Termius onde a barra fica colada acima do teclado instantaneamente.

### Causa raiz (parte 1)
O posicionamento `bottom` da barra de teclas usava `kbInsetSettled` (estado debounced de 140ms) em vez de `kbInset` (tempo real). Como `kbInsetSettled` só é atualizado após o timer de 140ms expirar, a barra de teclas ficava "atrasada" em relação ao teclado:

- Teclado abre → `kbInset` atualiza imediatamente → mas barra usa `kbInsetSettled` (aindo antigo) → **gap visível por 140ms**
- `transition-[bottom] duration-150` no botão minimizado piorava: barra animava lentamente → **gap adicional por 150ms**
- Total: até ~290ms de delay + gap entre teclado e barra

### Correção (parte 1 — commit `0b3eceb`)

| Elemento | Antes | Depois |
|----------|-------|--------|
| Minimized bar `bottom` | `kbInsetSettled > 0 ? kbInsetSettled + 24 : DOCK_CLEAR_PX` | `kbInset > 0 ? kbInset + 24 : DOCK_CLEAR_PX` |
| Expanded bar `bottom` | `aboveDock(kbInsetSettled)` | `aboveDock(kbInset)` |
| Minimized bar transition | `transition-[bottom] duration-150` | removido |

### Problema (parte 2 — position absolute vs viewport)
Mesmo com `kbInset` (tempo real), a barra tinha `position: absolute`, o que a tornava relativa ao container pai (`main[relative]` via `motion.div[absolute]`). Quando o teclado abre no Android WebView, `100dvh` diminui → `main` (flex-1) diminui → container da barra diminui → referência de `bottom` da barra sobe junto com o container → **a barra não acompanha o teclado corretamente, gap persistente**.

Cadeia de dependências:
```
teclado abre → dvh diminui → main.diminui → motion.div.diminui → bar.containing_block.diminui
                                                                    ↑
                                              bar.bottom relativa a isto ❌
```

### Correção (parte 2 — commit `9d6e3f9`)

| Elemento | Antes | Depois |
|----------|-------|--------|
| Minimized bar position | `absolute` | `fixed` |
| Expanded bar position | `absolute` | `fixed` |
| Bar `bottom` reference | pai (main/motion.div) | viewport (independente de container) |

Com `position: fixed`, o `bottom` da barra é sempre relativo à viewport:
- Barra bottom edge = viewport bottom - (kbInset + 24)
- Teclado top edge = viewport bottom - kbInset
- Gap = 24px ✅ (sempre, sem delay, sem gap)

A barra agora é **completamente independente** de qualquer resize de container ou debounce. O `recalcTerminal()` (debounced 140ms) continua existente apenas para ajustar a altura do terminal (heavy computation com `fitAddon.fit()`), sem afetar a barra.

### Separação de responsabilidades final
- **Posicionamento da barra** (`kbInset` + `position: fixed`) → tempo real, sem debounce, sem dependência de container → barra segue o teclado instantaneamente
- **Recálculo de altura do terminal** (`recalcTerminal()` → `fitAddon.fit()`) → continua debounced 140ms → evita recálculo pesado em tempo real

### Correção (parte 3 — commit `8ada4bf`)

**Referência técnica**: [MDN Visual Viewport API](https://developer.mozilla.org/en-US/docs/Web/API/Visual_Viewport_API) — `window.visualViewport.addEventListener('resize', handler)` com cálculo `kbdLift = Math.max(0, window.innerHeight - visualViewport.height - visualViewport.offsetTop)` aplicado diretamente via `requestAnimationFrame`.

| Elemento | Antes | Depois |
|----------|-------|--------|
| Posicionamento | `setState(kbInset)` → React re-render → DOM | `updateBar(inset)` → rAF → DOM direto |
| Guarda de frame | N/A | `pendingBarUpdate` flag — 1 rAF/frame máx |
| State `kbInset` | `useState(0)` (React overhead) | Removido (direct DOM apenas) |
| Cleanup | Separado (focusDebounceRef perdido) | Consolidado no return do effect |

Fluxo:
```
teclado abre → visualViewport 'resize' → onVV()
  → updateBar(inset): pending=true → rAF → pending=false
    → barMinimizedRef.style.bottom = kbInset+24px (imediato)
    → barRef.style.bottom = aboveDock(kbInset) (imediato)
  → setTimeout(140ms)
    → setKbInsetSettled → recalcTerminal() (separado)
```

Barra **completamente independente** de React, container resize, ou debounce. `recalcTerminal()` (140ms) apenas para altura do terminal.

### Referências técnicas
- [MDN Visual Viewport API](https://developer.mozilla.org/en-US/docs/Web/API/Visual_Viewport_API)
- xterm.js pattern (PR codeg #647): `kbdLift = Math.max(0, innerHeight - visualViewport.height - visualViewport.offsetTop)`
- Bram.us: rAF com guard "pendingUpdate"

### Arquivo modificado
- `src/components/screens/TerminalXtermScreen.tsx`
  - Linha ~82: `const [kbInset, setKbInset]` → `const barMinimizedRef = useRef<HTMLDivElement | null>(null)`
  - Linha ~193: `updateBar(inset)` com rAF + pending guard
  - Linha ~209: `updateBar(inset)` no lugar de `setKbInset(inset)`
  - Linha ~220: cleanup consolidado (kbSettleTimer + focusDebounceRef)
  - Linha ~775: minimized bar `ref={barMinimizedRef}` `data-testid="ov-bar-minimized"` `bottom: DOCK_CLEAR_PX`
  - Linha ~782: expanded bar `bottom: DOCK_CLEAR_PX` (default, rAF atualiza)

### Restrições mantidas
- PTY, tmux, WebSocket, termApi: **inalterados**
- BUG A (scrollback), BUG B (perda de cor), BUG C focus lag, responsivo: **mantidos**

## Git
- Frontend commit: `8ada4bf` (xterm-v3, force-pushed)
