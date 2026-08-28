# CONTEXTO_TERMINAL — Pré-checagem estabilidade + Avaliação Etapa 1 UX mobile (22-23/08/2026)

## Pré-checagem obrigatória (scroll oscilante pós-Etapa 2)
- Deploy referência: 4930844 (22/08 23:21)
- journalctl desde então: apenas eventos do E2E automatizado (até 23:18);
  nenhum flapping/duplicado; nenhum uso real registrado ainda.
- Instrumento __hokScroll era client-side temporário (removido antes do commit;
  padrão documentado para remedição futura).
- Dono informado: "Ainda não testei".
- VEREDITO: sem sinais de instabilidade → prosseguiu para avaliação Etapa 1.

## Avaliação por item (impacto × esforço)
✅ JÁ IMPLEMENTADO (zero esforço):
- Múltiplas abas/sessões tmux hok-<sid> independentes (FASE 6) — exatamente o
  mecanismo sugerido.
- Snippets/comandos rápidos editáveis e persistidos (FASE 5). Opcional: pré-
  carga das skills HOK como base inicial.
- Scrollbar customizada arrastável (FASE 2). Addon oficial desnecessário.

P1 — Teclado estendido 3 seções deslizáveis (alto impacto / esforço médio ~1-2h)
- Existe: barra fixa Ctrl/Alt(sticky+swipe)/Esc/Tab/Ctrl+C/Ctrl+D/setas.
- Faltam: Home/End/PgUp/PgDn/Insert/Delete, F1-F12, símbolos completos,
  Ctrl+W/R/L/S/Z, pager horizontal 4 colunas por grupo.
- Sequências: ESC[1~/4~/5~/6~/2~/3~, ESCOP..ESC[24~, \x17/\x12/\x0C/\x13/\x1A.
- Risco baixo (aditivo).

P2 — Visual (impacto médio-alto / esforço baixo ~45min cada)
- Temas: infra pronta (TERMINAL_THEMES + persistência + sync cross-tab).
  Faltam paletas "Termius-like" e "High Contrast" + seletor rápido na barra.
- Zoom: botões ±/reset fontSize + persistência (pinch = extensão posterior,
  conflito de gesto com swipe-scroll).

P3 — Gestos de navegação (médio/médio ~1-2h)
- Long-press+arrastar vertical → setas contínuas throttled: viável (touch
  handlers existentes).
- Space+swipe → setas: INVIAVEL via web (teclado virtual não expõe a tecla
  física); alternativa futura: botão-espaço próprio modo trackpad.

P5 — Histórico unificado visual (médio-baixo/médio): ADIAR (seta ↑ cobre 90%).

❌ Fora de escopo confirmado: ditado IA, split-view, shake.

## Plano proposto Etapa 2 (aguardando aprovação)
Lote 1: teclado estendido. Lote 2: temas + zoom. Lote 3: gestos.
Cada lote: build isolado + smoke + commit isolado + adendo.

## Nota de estabilidade
Critério zero tolerância mantido: qualquer oscilação perceptível nos testes
ou em uso → reversão instantânea para ttyd (hok-terminal.service segue ativo).
