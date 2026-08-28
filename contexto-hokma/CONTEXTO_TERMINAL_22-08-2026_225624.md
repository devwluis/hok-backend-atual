# CONTEXTO_TERMINAL — Avaliação Etapa 1: ttyd+tmux vs terminal custom (22/08/2026)

## Objetivo
Decidir entre (A) manter ttyd adicionando tmux, ou (B) retomar o terminal
custom xterm.js+PTY Go aplicando as lições de estabilidade. Avaliação ANTES
de qualquer código, conforme pedido do dono.

## Comparativo (fatos verificados nesta semana)
| Capacidade | A: ttyd+tmux | B: custom (estado atual do repo) |
|---|---|---|
| Persistência | 1 linha no unit | JÁ FEITA (7d6ee03: PTY dentro de tmux hok-<sid>, adoção de órfãs, kill-session no close) |
| Teclas especiais mobile | Exige fork do cliente ttyd (--index) ou postMessage não suportado | PRONTO (38984dd: SwipeKey/sticky/swipe) |
| Copiar/colar mobile | Nativo fraco em iframe cross-origin; long-press impossível sem fork | PRONTO (long-press + menu + copiar-tudo) |
| Flapping 15-20s (trava) | N/A | CORRIGIDO (21732d5: guard idempotente; E2E 1 conexão/0 quedas) |
| Duplicação | N/A (replay=screen) | CORRIGIDO (748bdf4/0045dec, smoke 1× após 2 reattachs) |
| Scroll oscilante | N/A | Investigação pausada; backend descartado; mitigação planejada (auto-scroll só no fundo) |
| Chat→terminal | Não existe canal | Gate tmux-aware ativo (76b70cf); desabilitável em 1 linha |
| Segurança | Proxy validador (mantém) | HOK_TOKEN na conexão; efêmero = melhoria futura |

## Recomendação: CAMINHO B
1. Motivo original da troca (instabilidade/flapping/persistência) já endereçado
   nos commits 7d6ee03/76b70cf/21732d5/748bdf4.
2. A exigiria fork permanente do cliente ttyd para features que o B já tem.
3. B está ~95% pronto: remapear tela "terminal" no AppShell (1 linha); ttyd
   permanece instalado como fallback.
4. Risco residual: scroll oscilante sem causa raiz fechada — plano de
   instrumentação curta + endurecimento do auto-scroll na Etapa 2, com
   rollback instantâneo (remapear para ttyd).

## Plano proposto para Etapa 2 (após aprovação)
1. Remapear SCREENS.terminal → TerminalScreen (xterm in-app).
2. Instrumentação temporária de medição de scroll (removida antes do commit final).
3. Endurecer auto-scroll (margem de fundo + suprimir durante TUI ativa).
4. Testes: opencode parado 60s; scroll manual 20s sem ser puxado; troca de aba;
   journalctl limpo (sem flapping).
5. ttyd/hok-terminal.service: manter coexistindo como fallback ou desativar
   (decisão do dono).

## Bugs pendentes chat→terminal
Permanecem fora de uso. Gate atual (76b70cf) já impede digitação fora de shell;
desabilitação total da chamada é opcional e isolada.
