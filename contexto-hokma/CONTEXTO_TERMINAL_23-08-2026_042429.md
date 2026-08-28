# CONTEXTO_TERMINAL — Rollback lotes 1-3 + investigação (23/08/2026)

## Gatilho
Scroll oscilante reapareceu após deploy dos lotes 1-3 (UX mobile). Critério
zero tolerância acionado → rollback imediato.

## Rollback executado ✅
Reverts no frontend (hok-frontend-atual main):
- b1cba13 Revert L3 gestos (Space trackpad/setas contínuas)
- f0e7a6c Revert L2 temas Termius/HighContrast + zoom
- 37f3431 Revert L1 teclado estendido
Deploy: asset voltou a index-6gghB3C0.js (hash IDÊNTICO ao da Etapa 2 estável
— prova de reversão fiel). Estado atual = Caminho B original (tmux+teclas
básicas+copiar/colar), sem os lotes.

## Investigação da suspeita "gate enfraquecido pelos lotes"
DESCARTADA por diff: use-terminal.tsx vs pré-lotes mostra APENAS o fix
anti-flapping (21732d5) intacto — lotes 1-3 não tocaram conexões/gates.
Gate TUI backend também intocado (76b70cf).

## Achado adicional importante
routes.go com a delegação por Host (terminal.* → proxy validador) estava EM
PRODUÇÃO sem versionamento — commitado agora: 66a67ec.

## Crashes nos testes E2E = limitação do AMBIENTE (não do produto)
Experimento controlado (headless Chromium neste host):
1. ttyd direto (:7681, xterm.js próprio + stream binário) + carga 75s → SOBREVIVEU
2. App HOK OS na tela CHAT (sem xterm montado) → CRASHOU (~30-60s)
Conclusão: o renderer headless deste host derruba o APP BASE independente da
tela — inviabiliza validação visual automatizada (os 3 cenários da Etapa 2).
Validação definitiva requer dispositivo/navegador real do dono.

## Estado final em produção
- Frontend: Caminho B original (pré-lotes), asset index-6gghB3C0.js
- hokma.service: inalterado nesta rodada
- hok-terminal.service (ttyd fallback): ativo

## Próximos passos sugeridos
1. Dono valida manualmente em dispositivo real: opencode 60s / scroll manual
   20s / troca de aba (critério zero tolerância).
2. Se oscilar MESMO sem os lotes → bug pré-existente mais profundo; investigar
   com instrumentação __hokScroll (padrão desta sessão) em dispositivo real.
3. Lotes 1-3 ficam prontos nos commits revertidos; re-aplicáveis via
   git revert dos reverts quando a causa raiz for fechada.
