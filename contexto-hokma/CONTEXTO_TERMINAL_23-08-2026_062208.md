# CONTEXTO_TERMINAL — TESTE 1 validado: teclado estendido sobre ttyd (23-24/08/2026)

## Contexto
Rodada 1 de N da reintrodução gradual de features sobre a base estável ttyd
(decisão pós-rollback dos lotes). Interrupção por queda de bateria durante os
testes; verificação de integridade confirmou commits/deploy íntegros antes da
validação final.

## Implementado
- Backend (3fdacb5): POST /terminal/ttyd/key — injeção de teclas na sessão
  tmux hok-ttyd via tmux send-keys (whitelist de nomes; texto literal com -l;
  limite 64 chars; protegido pelo token efêmero).
- Frontend (55e6871): TerminalTTYDScreen v2 — barra overlay FORA do iframe
  (cross-origin impede injeção direta), teclas → fetch /terminal/ttyd/key.
  Sticky Ctrl/Alt combinam com setas/nav ("C-Left", "M-Up"); símbolos literais.

## Validação E2E (headless Chromium real)
| Teste | Resultado |
|---|---|
| Barra overlay presente | ✓ |
| ↑ navega histórico do shell | ✓ (tela muda) |
| ^C interrompe processo | ✓ (tela muda) |
| Símbolo \| digitado | ✓ |
| C-A opencode PARADO 60s | 12 snapshots = 1 hash único — ZERO oscilação ✅ |
| C-B scroll manual 20s | responde à rolagem sem travar ✅ |
| C-C troca de aba Chat↔Terminal | iframe+overlay persistem, LIVE ✅ |

## Estado final
- ttyd definitivo na aba Terminal (mantido); teclado estendido ATIVO como
  overlay fora do iframe.
- Commits: backend 3fdacb5 · frontend 55e6871 (inclui correção de testid).
- Backups: terminal_routes.go.bak_*_integcheck,
  TerminalTTYDScreen.tsx.bak_*_integcheck.

## Rollback
    cd /root/hokma-web/artifacts/hok-os && git revert 55e6871
    npm run build && rsync -a --delete dist/public/ /var/www/hok-os/
    cd /root/hokma/backend && git revert 3fdacb5 && go build -o hokma . && systemctl restart hokma

## Pendências
1. Upload deste adendo ao Drive PENDENTE: credencial Google Drive account 2
   expirada no n8n ("needs to be reconnected" — 401 desde 05:43 de 22/08).
   Reconectar em Credentials no n8n e reenviar o arquivo local.
2. Próximo teste isolado (aguardando aprovação): temas de cor.
