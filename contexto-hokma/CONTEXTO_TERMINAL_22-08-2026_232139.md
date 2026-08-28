# CONTEXTO_TERMINAL — Etapa 2: Caminho B aprovado e validado (22/08/2026)

## Decisão
Caminho B aprovado (avaliação Etapa 1 no adendo anterior): retomada do
terminal custom xterm.js+PTY Go, que já contava com persistência tmux
(7d6ee03), flapping corrigido (21732d5), gate TUI tmux-aware (76b70cf),
teclas especiais mobile (38984dd) e copiar/colar.

## Mudanças da Etapa 2 (commit 4930844, frontend)
1. AppShell: tela "terminal" remapeada de TerminalTTYDScreen → TerminalScreen
   (xterm in-app). ttyd permanece ativo como fallback instantâneo
   (hok-terminal.service segue rodando; reversão = voltar o mapeamento).
2. TerminalScreen: auto-scroll ENDURECIDO — suprimido durante TUI ativa
   (tuiActiveRef): frames de TUI com posicionamento absoluto não forçam mais
   scroll pro fundo (suspeita principal da oscilação "sobe e desce").
3. Instrumentação temporária de medição aplicada, usada nos testes e REMOVIDA
   antes do commit final (0 refs).

## Testes executados (headless Chromium + journalctl paralelo)
- C1 — bash c/ histórico, PARADO NO FUNDO 60s: 60 amostras, viewportY
  constante (0 movimentos, 0 reversões) ✅
- C2 — rolar pra cima (~15 linhas) +20s: posição rolada MANTIDA, nunca puxada
  de volta ✅
- C3 — troca de aba Chat(15s)↔Terminal(12s): reconexão única e limpa
  (closeCode=1006 → reattach; ZERO eventos duplicados no mesmo segundo);
  viewport estável pós-retorno ✅
- TUI: opencode real aberto dentro da sessão tmux (herdado entre conexões,
  provando a persistência); durante TUI: ZERO scrollToBottom forçados.
- Observações de ambiente: chromium headless exigiu --disable-dev-shm-usage
  (crash de renderer era do ambiente de teste, não do produto).

## Estado final
- Frontend deployado: asset index-6gghB3C0.js em /var/www/hok-os (HTTP 200)
- ttyd/hok-terminal.service: ATIVO como fallback (remapeamento de 1 linha reverte)
- hokma.service: inalterado nesta etapa (sem restart necessário)
- Commits: 4930844 (frontend). Backups:
  TerminalScreen.tsx.bak_etapa2_20260822_230042,
  AppShell.tsx.bak_etapa2_20260822_230042

## Rollback
    cd /root/hokma-web/artifacts/hok-os
    git revert 4930844
    cp src/components/screens/TerminalScreen.tsx.bak_etapa2_20260822_230042 src/components/screens/TerminalScreen.tsx
    cp src/components/shell/AppShell.tsx.bak_etapa2_20260822_230042 src/components/shell/AppShell.tsx
    npm run build && rsync -a --delete dist/public/ /var/www/hok-os/

## Pendências
- Monitorar uso real por alguns dias (critério zero tolerância do dono).
- Scroll oscilante: se reaparecer, retomar investigação com os contadores
  __hokScroll (padrão de instrumentação desta sessão).
