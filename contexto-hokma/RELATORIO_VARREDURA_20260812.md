# Varredura HOKMA — Relatório de Bugs (2026-08-12)

Servidor: 179.197.64.133 (hostname: hokma, Debian)
Backend: /root/hokma/backend (Go 1.26, porta 8082, systemd hokma.service)
Frontend: /root/hokma-web/artifacts/hok-os (React/TS/Vite) → build em /var/www/hok-os
Infra: nginx :3002, traefik (docker) :80/:443, n8n, postgres, redis, portainer

## Status da análise
- go build OK, go vet OK, staticcheck OK (só style/unused), testes passam com env vars
- go test -race OK (testes não cobrem os caminhos concorrentes)
- Frontend: tsc typecheck OK

## CRÍTICOS — Segurança
1. Token master e senha vazados no bundle público
   - frontend/src/lib/default-settings.ts hardcoda HOK_TOKEN=5fae643bbee6b44c3da66a9099524bf4b6e53cd966aa373fba8764e517c85591
   - OwnerGate hash SHA-256: 5f6c0727ad559bbb67b55ef02cd79e47686e86e0a40696da7479983475f64887
   - Confirmado publicado em /var/www/hok-os/assets/index-r9IlVJmm.js
   - .env atual usa efb6543385f084bf4300b3eb4469df18336a824bc4d9185b204cd896d582302e (token antigo = 401)
2. OwnerGate 100% client-side (use-owner-auth.ts): bypass via localStorage.setItem("hokma.owner.v1","1"); hash SHA-256 não salgado quebrável
3. Backend exposto na internet (porta 8082 aberta, 0.0.0.0):
   - /introspect sem auth (enumera todos endpoints) — CONFIRMADO 200 externo
   - GET / e /status sem auth (vazam wifi SSID, IP, RAM, uptime) — routes.go:51
   - /auth/register e /auth/login sem rate limit (brute force; checkRateLimit existe em utils.go:16 mas nunca é usado)
4. CORS "*" global (utils.go:127) + getClientIP confia em X-Forwarded-For (utils.go:36)
5. handleSummarizeHistory (routes.go:835) sem auth e usa OR_KEY do servidor — NÃO registrada em main.go (por ora não exposta, risco latente)

## Bugs funcionais
6. alert("DEBUG: pendingAction recebido...") em ChatScreen.tsx:463 — confirmado no bundle de produção (index-r9IlVJmm.js)
7. handleFileWrite chama requireHokAuth 2x (fs_routes.go:128-130)
8. Testes falham sem env: main.go:50 log.Fatal no init() se HOK_TOKEN ausente
9. Fallback /api/chat quebrado: ChatScreen.tsx:433 usa /api/chat sem Server URL, backend não tem a rota (nginx proxia /api/ → 404)
10. Sem timeout em comandos aprovados: ExecuteApprovedCommand (pending_action.go:545), resolveTaskAgentPendingAction (405)
11. handleSmartChatWithFiles: defer file.Close() em loop (vaza FDs) + stub incompleto (smart_chat.go:292-361)
12. device_bridge.go: fila global sem isolamento de tenant; resultReady cresce sem limite
13. db.SetMaxOpenConns(1) (db.go:23) — SQLite serializado, gargalo
14. Blocklists de comando bypassáveis (rm -rf /x; .env não bloqueado em executeCommand utils.go:168)
15. /fs/exec "só local" inútil atrás do proxy (RemoteAddr = 127.0.0.1 via nginx)
16. JWT_SECRET cai para HOK_API_TOKEN se não definido (auth.go:41-47)

## Positivos
- SQL parametrizado (db.go), whitelist de tabelas no getSQLiteCount
- Assinatura Meta webhook (META_APP_SECRET)
- Guardrail determinístico de args n8n (validateArgsBeforePending)
- Smoke test + rollback no self-mod
- Sem secrets hardcoded em .go; sem race detector

## Google Drive
- NENHUM acesso a Google Drive no código. Integrações: Google Sheets (addImovel, CRM), Gemini API (env GEMINI_KEY), OpenRouter/DeepSeek/Groq/OpenAI/Cerebras/DeepHat (env).
- Não existem service accounts nem credentials.json no servidor.
- O diretório /root/.keys não existe.

## Prioridades de correção
1. Rotas sem auth na 8082 (introspect/status) + fechar porta ou exigir auth
2. Token/senha no bundle (trocar token, remover hash/hardcode)
3. alert() de debug no ChatScreen
4. Timeouts nos comandos aprovados
