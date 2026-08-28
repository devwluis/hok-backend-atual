# CONTEXTO_TERMINAL — integração frontend + renovação de token (22/08/2026)

## O que foi implementado
- Componente `TerminalTTYDScreen.tsx` (criado na rodada anterior, commit 568ab92)
  REFINADO nesta sessão: renovação AUTOMÁTICA do token efêmero enquanto o
  usuário permanece na aba (novo token 60s antes de expirar; expires_in vindo
  do backend), com reload do iframe via key={url}; retry de rede a cada 10s.
- Fonte confirmada: /root/hokma-web/artifacts/hok-os (nginx :3002 serve
  /var/www/hok-os). /root/hokma/lovable-frontend NÃO existe.
- Padrão de auth: header X-Hok-Token com HOK_TOKEN de hokma.settings.v1.

## Commits
- 843220c "feat(terminal): renovação automática do token ttyd (60s antes de
  expirar) com reload do iframe e retry de rede em 10s" (frontend)
- (anterior) 568ab92 tela iframe; 2386685 rotas backend

## Build e deploy
- typecheck ✓ | build ✓ | hash do bundle MUDOU: index-tvlHxwkG.js → index-DQFyQrsQ.js
- rsync para /var/www/hok-os ✓ | produção servindo novo asset ✓ HTTP 200

## Smoke tests (produção)
- health ok | token sem auth 401 | com auth 200+URL
- proxy com token 200 | sem token 401

## Pendências / notas
1. DNS CNAME terminal → 7b0337b4-…cfargotunnel.com (Proxy ON) ainda pendente no
   dashboard Cloudflare — sem ele o iframe público não resolve. Fluxo local 100%.
2. Trade-off aceito: reload do iframe na renovação inicia nova sessão ttyd
   (sessão shell corrente não sobrevive ao refresh — pedido do dono).
3. Teste visual completo depende do item 1.
