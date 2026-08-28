# CONTEXTO_TERMINAL — Lotes 1-3 UX mobile implementados (23/08/2026)

## Execução (3 lotes aprovados, commits isolados)
| Lote | Commit | Conteúdo |
|---|---|---|
| 1 | 63d6112 | Teclado estendido deslizável: Home/End/PgUp/PgDn/Ins/Del, Ctrl+W/R/L/S/Z, F1-F12, símbolos completos — linha extra na barra mobile |
| 2 | 0c5bf76 | Paletas Termius-like (#011627) e High Contrast (#000/#fff) + seletor cíclico na barra + zoom fontSize ± persistido |
| 3 | 76d3395 | Gestos: botão Space vira trackpad de setas quando segurado (long-press 500ms); arrasto vertical envia ↑/↓ contínuo (throttle 24px/tecla, máx 6) |

## Build/smoke por lote
- L1: typecheck+build ✓; bundle contém todas as sequências (verificado por grep no asset); deploy ✓
- L2: typecheck ✓ (corrigida duplicação de declaração gerada pelo patch); build ✓
- L3: typecheck ✓; build ✓; E2E digitação normal intacta
- Deploy final: index-_8hbAId5.js em produção, HTTP 200

## Notas técnicas
- Teclas estendidas visíveis junto da barra especial (com teclado aberto);
  navegação horizontal nativa por overflow-scroll entre os grupos.
- Zoom: fontSize 8..22px, persistido em hokma.terminal.fontSize.v1.
- Temas: ciclo HOK Dark → Termius-like → High Contrast (+ pré-existentes),
  persistido em hokma.terminal.theme.v1, sincronizado cross-tab via evento.
- Trackpad de setas: flag de módulo compartilhada barra(pai)↔corpo(filho)
  (arrowsTrackpad), interceptando touchmove antes do scroll custom.
- Space+swipe do teclado virtual: inviável via web (documentado) — substituído
  pelo botão-espaço trackpad próprio.

## Pendências / recomendações
1. Validação visual manual no celular real (barra aparece com teclado aberto).
2. Monitorar oscilação de scroll em uso real (critério zero tolerância mantido;
   fallback ttyd instantâneo via remapeamento).
3. Opcional futuro: painel de histórico visual; pré-carga skills como snippets.
