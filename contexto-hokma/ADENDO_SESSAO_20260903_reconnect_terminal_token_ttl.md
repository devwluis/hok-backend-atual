# ADENDO — SESSÃO 03/09 (rodada 5) — Fix do "Press ⏎ to Reconnect" no terminal mobile + HOK_STATE.md operacional

Sessão dedicada ao overlay "Press ⏎ to Reconnect" frequente no terminal HOK no celular,
após o usuário reportar captura de tela com o erro e o overlay sobreposto.

## Contexto da captura (09:11, 02/09)
- "API Error: 400 opencode-go/deepseek-v4-flash is not a valid model ID" → já resolvido
  (era o `aihubmix/coding-glm-5.3-free`; filtro do catálogo 934→610 da rodada anterior).
  Confirmado: `opencode-go/deepseek-v4-flash` funciona.
- Overlay "Press ⏎ to Reconnect" → motivo desta rodada.

## Investigação do reconnect (causa raiz)
- Caminho real no celular: browser → Cloudflare Tunnel (`terminal.imoveischaves.com` →
  backend :8082 → ttyd :7681). Nginx NÃO está no caminho do domínio do terminal.
- ~210 "WS closed" do ttyd em 24h, todos coincidindo com períodos de uso ativo.
- Cloudflare: quedas QUIC por inatividade raras (10 em 48h, pico 21:46); "stream canceled
  by remote" = browser fechando (2x). Não são a causa principal.
- **Causa raiz:** TTL do token do terminal era **5min**. Pausa >5min (lendo tela/app em
  background) expirava o token do WS → o reconnect automático do ttyd falhava com 401 →
  overlay "Press ⏎ to Reconnect" até toque manual.

## Correções aplicadas
1. **Backend `terminal_routes.go`:** `terminalTokenTTL` 5min → **30min**.
   - Frontend renova silenciosamente a cada ~29min (expires_in agora 1800s).
   - O ttyd reconecta sozinho dentro da janela de validade — sem 401.
   - Validado em produção: `expires_in: 1800`.
2. **nginx `hokma-web`:** novo `location ^~ /terminal/` com `proxy_read_timeout 6h`
   (cobre acesso via `app.imoveischaves.com` — WS ocioso não corta mais em 300s).
   - Backups: `hokma-web.bak_*_ws_timeout`.

## Arquivos alterados
- Backend: `terminal_routes.go` (TTL token).
- nginx: `/etc/nginx/sites-enabled/hokma-web` (location terminal com timeout longo).
- Commit `04807e2` pushado (hok-backend-atual).

## Como validar
- Usar o terminal no celular com pausas >5min: o "Press ⏎ to Reconnect" deve sumir.
- Se ainda cair: investigar suspensão de WebSocket pelo Chrome em background (comportamento
  nativo do mobile), não do backend.

## Pendências
- (se persistir) testar suspensão de WS do Chrome Android em background.
- Itens abertos em PENDENCIAS.md inalterados.