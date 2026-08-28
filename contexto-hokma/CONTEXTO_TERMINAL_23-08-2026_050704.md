# CONTEXTO_TERMINAL — Estabilidade confirmada pós-rollback + causa ambiental encontrada (23/08/2026)

## Rollback dos lotes 1-3 (concluído antes desta rodada)
Reverts b1cba13/f0e7a6c/37f3431 (frontend main). Produção servindo asset
index-6gghB3C0.js — hash IDÊNTICO ao da Etapa 2 estável.
Suspeita "gate enfraquecido pelos lotes": DESCARTADA por diff — use-terminal.tsx
vs pré-lotes contém apenas o fix anti-flapping 21732d5 intacto.

## Validação de estabilidade do estado revertido (automação, hoje)
Ferramenta: Playwright + leitura de translateY do .xterm-rows @1Hz.
- FASE USO ATIVO ~45s (digitação + wheel cima/baixo ×4): concluída sem crash
- OBSERVAÇÃO 90s PARADO: 95 amostras, viewportY CONSTANTE, 0 movimentos,
  0 reversões de direção, amplitude 0px
→ ✅ ZERO OSCILAÇÃO no estado revertido.

## 🔍 CAUSA AMBIENTAL ENCONTRADA (provável agravante histórico)
/tmp (tmpfs 3.9GB) estava 100% CHEIO:
- 433 arquivos /tmp/.<hash>-00000000.so de ~14MB cada = 3.6GB
- Assinatura do runtime Bun (opencode): extrai libs nativas a cada execução
  e NUNCA limpa. Cada abertura de opencode no terminal = +14MB.
- Efeito: Chromium crashava ("No space left on device" no font service) e
  todo o sistema degradava — incluindo possivelmente a instabilidade
  histórica percebida no terminal.
Ação: limpados (nenhum processo os usava). /tmp agora 12% usado.

## Experimento controle (descarta bug geral do app/headless)
- App na tela CHAT (sem xterm montado): CRASHOU também (~30-60s) quando /tmp
  estava cheio → crashes E2E eram ambientais, não do produto.

## Estado final em produção
- Frontend: Caminho B revertido (asset index-6gghB3C0.js), HTTP 200
- ttyd fallback: hok-terminal.service ativo (:7681 loopback)
- Backend: inalterado nesta rodada; routes.go versionado (66a67ec)

## Pendências / recomendações
1. Dono valida manualmente em dispositivo real (opencode/scroll/troca aba).
2. MITIGAR vazamento Bun .so: cron de limpeza `/tmp/.*.so` com idade > 1 dia
   OU reportar upstream opencode. Sem isso, enche novamente (~28 execuções
   de opencode ≈ 400MB).
3. Lotes 1-3 prontos para re-aplicação futura: git revert dos reverts
   (b1cba13/f0e7a6c/37f3431) após causa raiz do scroll fechada.
