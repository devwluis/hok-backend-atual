# CONTEXTO_TERMINAL — ttyd definitivo na aba Terminal (23/08/2026)

## Decisão final do dono
Abandono definitivo do terminal custom (xterm.js+PTY Go): scroll oscilante
persistiu MESMO com os 3 lotes revertidos → bug mais profundo na base do
Caminho B. ttyd assume a aba "terminal" em produção.

## Mudança (commit 484fab8, frontend)
AppShell: SCREENS.terminal → TerminalTTYDScreen (iframe ttyd + token efêmero).
TerminalScreen.tsx e todo o código xterm in-app PERMANECEM versionados
(rollback = voltar o mapeamento). ttyd/hok-terminal.service segue ativo.

## Smoke E2E executado (headless Chromium + interceptação de rota)
PARTE 1 — integração no app:
- iframe montado com src contendo token emitido ✓
- request ao hostname dedicado atendido pelo proxy validador ✓
- iframe PERSISTE após troca de aba Chat↔Terminal ✓

PARTE 2 — estabilidade visual no ttyd real:
- C1 parado 60s: 12 snapshots, 1 hash único → ZERO movimento ✅
- C2 wheel-up +20s: rolagem funcional (hash muda) e posição estável ✅
- C3 opencode TUI real 60s parado: 12 snapshots, apenas 3 variações legítimas
  (spinner/status), SEM alternância A/B/A/B de oscilação ✅

## Estado final em produção
- Aba Terminal = iframe ttyd (token efêmero 5min, renovado automaticamente)
- hok-terminal.service ativo (loopback :7681); Cloudflare ingress apontando
  para o backend (proxy validador)
- xterm in-app: código preservado, não montado
- Pendência mantida: DNS CNAME terminal no dashboard Cloudflare (acesso público)

## Rollback (se um dia o custom voltar)
    cd /root/hokma-web/artifacts/hok-os
    git revert 484fab8   # volta SCREENS.terminal para o xterm in-app
    npm run build && rsync -a --delete dist/public/ /var/www/hok-os/
