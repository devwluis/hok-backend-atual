# Adendo — RESPONSIVE DESKTOP/MOBILE LAYOUT + TESTES

**Origem:** opencode (frontend) | **Data/hora:** 14-09-2026

---

## Objetivo
Fazer o terminal responder corretamente em desktop (mouse+teclado físico) vs mobile (touch+teclado virtual), incluindo testes com Playwright.

## Comportamento por ambiente

### DESKTOP (mouse + teclado físico)
- `isMobile = false` quando: `pointer: fine` E `window.innerWidth >= 768`
- Teclas especiais (Ctrl/Esc/setas/Anexar) OCULTAS — desnecessárias com teclado físico
- Terminal ocupa altura total disponível (flex-1, sem cálculo visualViewport)
- Listener visualViewport NÃO registrado (evita execução desnecessária)
- Barra de teclas mobile NÃO visível (CSS `hidden` class)
- `toggleKeysBar()` bloqueada (early return se `!isMobile`)

### MOBILE (touch + teclado virtual)
- `isMobile = true` quando: `pointer: coarse` OU `window.innerWidth < 768`
- Toda lógica de barra de teclas especiais mantida
- Recálculo dinâmico via visualViewport ativo (140ms debounce)
- Teclas especiais VISÍVEIS
- `toggleKeysBar()` funcional

### BREAKPOINT: 768px
- 768px+ = desktop (teclado físico esperado)
- < 768px = mobile (touch esperado)
- Testado: 768px → desktop mode ✓ | 800px → desktop mode ✓ | 390px → mobile mode ✓ | 1024×768 landscape → desktop mode ✓

## Implementação (`src/components/screens/TerminalXtermScreen.tsx`)

### 1. Detecção isMobile (linha 103)
```typescript
const [isMobile, setIsMobile] = useState(() =>
  typeof window !== "undefined" &&
  (window.matchMedia("(pointer: coarse)").matches || window.innerWidth < 768)
);
// Resize listener atualiza isMobile quando janela muda de tamanho
```

### 2. recalcTerminal() (linha 141)
- Adicionado early return se `!isMobile`: limpa `area.style.height` (volta para flex-1)
- Dependency array: `[isMobile]`

### 3. Recalculation effect (linha 159)
- Condicional: `if (isMobile) recalcTerminal()` — não dispara em desktop

### 4. visualViewport listener (linha 190)
- Adicionado `if (!isMobile) return` no início
- Condicional: listener ativo apenas em mobile

### 5. toggleKeysBar() (linha 178)
- Early return: `if (!isMobile) return`

### 6. Force keys bar fechada (linha 209)
```typescript
useEffect(() => { if (!isMobile && keysExpanded) setKeysExpanded(false); }, [isMobile]);
```

### 7. Gesture overlay (linha 692)
- `coarse &&` → `isMobile &&` (include touch OR narrow viewport)

### 8. Keyboard bar (linha 743)
- `<div className={isMobile ? "" : "hidden"} data-testid="ov-bar-container">`
- Sempre renderizada no DOM, mas oculta via CSS `hidden` em desktop
- Evita flash/re-render ao alternar entre desktop/mobile via resize

## Testes (Playwright)

| Viewport | Toggle visível | Barra visível | Terminal visível | Erros |
|----------|---------------|---------------|-----------------|-------|
| Desktop 1280×800 | ✗ | ✗ | ✓ (627px) | 0 |
| Mobile 390×844 | ✓ | ✓ (expande) | ✓ (671px) | 0 |
| Tablet 800×1024 | ✗ | ✗ | ✓ (851px) | 0 |
| Breakpoint 768×1024 | ✗ | ✗ | ✓ (851px) | 0 |
| Landscape 1024×768 | ✗ | ✗ | ✓ (595px) | 0 |

**Resize live:** Desktop→Mobile keyboard bar aparece ✓ | Mobile→Desktop desaparece ✓

## Deploy
- Build: `index-F61EUahA.js` (920KB) + `index-BD3Aet9i.css` (141KB)
- Deployed em `/var/www/hok-os/`
- Smoke test: 0 page errors, terminal funcional em todos os viewports testados

## Git
- Frontend commit: `2dc472f` (xterm-v3, force-pushed)
- Commit anterior: `4449bfb` (ETAPA 3 layout)
