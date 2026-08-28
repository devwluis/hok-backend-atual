# CONTEXTO_TERMINAL — migração para ttyd (22/08/2026)

## O que foi implementado
1. ttyd 1.7.7 instalado em /usr/local/bin/ttyd (release GitHub; apt não tem).
2. hok-terminal.service criado, HABILITADO e ATIVO: ttyd -i 127.0.0.1 -p 7681 -W bash
   (Restart=always/3s; loopback only — nunca exposto direto).
3. Backend terminal_routes.go (novo): tokens efêmeros 5min em tabela SQLite
   `terminal_tokens` via sqliteExecParams (GC automático); rotas:
   - POST /terminal/token (requireOwnerToken) → {"terminal_url": ".../?token=X"}
   - GET /terminal/token/validate?token= → 200/401
   - /terminal/ttyd → PROXY VALIDADOR reverso ao ttyd (WebSocket 101 incl.);
     token por query (entrada) e cookie hok_term_tok (SameSite=None+Secure p/
     iframe cross-origin). Assets estáticos do ttyd ficam públicos por natureza.
4. Cloudflare Tunnel: ingress terminal.imoveischaves.com → http://127.0.0.1:8082
   (BACKEND/proxy validador). DECISÃO DE SEGURANÇA: apontar direto pro :7681
   deixaria shell root público — o token seria cosmético. cloudflared reiniciado.
5. Frontend: nova tela TerminalTTYDScreen (iframe + fetch do token), mapeada no
   AppShell como "terminal". xterm in-app PRESERVADO em código (rollback = voltar
   o mapeamento).

## Commits
- backend: 2386685 "feat(terminal): rotas ttyd — token efêmero 5min em tabela terminal_tokens..."
- frontend: 568ab92 "feat(terminal): tela 'terminal' passa a usar ttyd real via iframe..."

## Smoke tests (todos passando)
- /health → {"status":"ok"}
- POST /terminal/token SEM auth → 401 | COM auth → 200 + URL
- GET validate token válido → 200 | inválido → 401
- Proxy raiz sem token → 401 | com token → 200 <title>ttyd - Terminal</title>
- WS upgrade sem token → 401 | com token → HTTP 101 Switching Protocols

## Estado final
- hok-terminal.service: enabled + active (loopback 127.0.0.1:7681)
- hokma.service: active, health ok, binário == hokma_test
- Frontend deployado (asset index-tvlHxwkG.js), backup pré-deploy:
  /var/www/hok-os.bak_ttyd_20260822_214843
- Backups código: terminal_routes.go.bak_*_ttydsqlite; config.yml.bak_*_ttyd

## Pendência CRÍTICA (ação manual no dashboard Cloudflare)
DNS ainda não existe para terminal.imoveischaves.com. Criar:
  DNS → CNAME @host "terminal" → "7b0337b4-586f-4f8b-82bf-fea9f4d8ab69.cfargotunnel.com" (Proxy ON)
Motivo: cert.pem de origem ausente impede `cloudflared tunnel route dns` local.
Enquanto isso, o fluxo é validável localmente em http://127.0.0.1:8082.

## Limitações conhecidas
- Token expira em 5min: sessões ttyd JÁ conectadas continuam vivas (validação
  só na entrada/handshake); reload da página pede novo token automaticamente.
- Assets do ttyd são públicos por natureza (JS/CSS genéricos) — a sessão
  interativa exige token.
