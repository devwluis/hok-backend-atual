# CONTEXTO_TERMINAL — TESTE 2 validado: temas de cor (23-24/08/2026)

## Implementado (isolado sobre base ttyd estável)
- Backend (6943251): POST /terminal/ttyd/theme — aplica paleta À SESSÃO ttyd
  VIVA via sequências OSC 10/11/4 (bg/fg/cursor/ANSI 0-15) injetadas por
  tmux send-keys -l; aceita {osc} pronto ou paleta estruturada.
- Frontend (28416ca + 65bd5d3): paletas HOK Dark/Termius-like/High Contrast
  em TERMINAL_THEMES; botão Palette no header do ttyd screen cicla temas,
  aplica via rota acima e persiste em hokma.terminal.theme.v1.

## Validação E2E (headless Chromium)
| Cenário | Resultado |
|---|---|
| Barra overlay teclas | ✓ presente |
| ↑ navega histórico / ^C interrompe | ✓ |
| Tema cicla + persiste | ✓ (highcontrast→dark→solarized) |
| C-A opencode PARADO 60s | 12 snapshots = 1 hash único — ZERO oscilação ✅ |
| C-B scroll manual 20s | 1/10 únicos, sem travar ✅ |
| C-C troca aba Chat↔Terminal | iframe/barra/LIVE persistem ✅ |

## Estado final
- Aba Terminal: ttyd + overlay teclas + temas cicláveis (OSC na sessão viva)
- Commits: backend 6943251 · frontend 65bd5d3 (+28416ca paletas)
- Backups: SettingsScreen.tsx.bak_teste2tema_*, TerminalScreen.tsx.bak_teste2tema_*,
  terminal_routes.go.bak_*_teste2tema

## Rollback
    cd /root/hokma-web/artifacts/hok-os && git revert 28416ca 65bd5d3
    npm run build && rsync -a --delete dist/public/ /var/www/hok-os/
    cd /root/hokma/backend && git revert 6943251 && go build -o hokma . && systemctl restart hokma

## Pendências
1. Upload deste adendo ao Drive PENDENTE: credencial Google Drive account 2
   expirada (401 desde 22/08 05:43). Reconectar em Credentials no n8n.
2. Próximo teste isolado (aguardando aprovação): zoom fontSize ±.
