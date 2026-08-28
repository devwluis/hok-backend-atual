# CONTEXTO_TERMINAL — TESTE 2: barra minimizável estilo Termius (24/08/2026)

## Implementado (commit 2b4cf91, frontend)
- Estado MINIMIZADO (padrão): ícone compacto 40px ancorado ao teclado via
  visualViewport (fix anterior mantido).
- Toque no ícone → EXPANDE barra completa: sticky Ctrl/Alt, setas, Home/End/
  PgUp/PgDn/Ins/Del, Esc/Tab/Space/⌫/⏎, combos ^C ^D ^W ^R ^L ^S ^Z.
- Botão "..." alterna GRUPO EXTRA: F1-F12 + símbolos completos.
- Botão recolher volta ao minimizado; preferência persistida em
  hokma.terminal.keysbar.v1 ("min"/"expanded").

## Validação E2E (headless Chromium real)
| Verificação | Resultado |
|---|---|
| Ícone minimizado presente, barra completa ausente | ✓ |
| Sem sobreposição com Dock (parcial aceitável sem teclado) | ✓ |
| Expande: teclas nav + "..." + recolher | ✓ |
| Grupo extra F-keys+símbolos visível após "..." | ✓ |
| Recolher volta ao minimizado | ✓ |
| Persistência localStorage entre reloads | ✓ |

## Estado final
- Aba Terminal = ttyd definitivo + teclado minimizável (padrão minimizado)
- Commits: frontend 2b4cf91 · Backups: TerminalTTYDScreen.tsx.bak_*_minimize_*
- Rollback: git revert 2b4cf91 + rebuild + rsync

## Pendências
1. Adendos no Drive PENDENTES de reenvio até a credencial Google Drive
   account 2 ser reconectada no n8n (401 desde 22/08 05:43): 054341, 062208,
   073611 e este arquivo — todos salvos localmente em backend/contexto-hokma/.
