# COMMIT — Segurança SaaS da varredura 12/08: rotas fechadas na 8082, OwnerGate server-side, bundle limpo

Data: 2026-08-14 13:40 · Por: Washington + opencode (sessão remota via SSH)

## O que foi feito (retomada do item pendente do master context)

### Backend — commit `446b10c` (hok-backend-atual), validado em produção
- `/introspect` agora exige `requireHokAuth` (antes enumerava todos os endpoints sem auth — confirmado 200 externo na varredura).
- `GET /` (handleRoot → handleStats) exige auth — vazava wifi SSID, IP, RAM, uptime. POST / já exigia.
- `handleSummarizeHistory` (routes.go:835, latente, usa OR_KEY do servidor) ganhou guard de auth.
- Rate limit 10 req/min/IP em `/auth/register`, `/auth/login` e novo `/auth/owner-check` (`checkRateLimit` existia em utils.go:16 e nunca era usado).
- Novo `POST /auth/owner-check`: valida a senha contra admins/owners do DB (bcrypt, `users` table) e devolve JWT role owner. Sem rate limit bypassável; 429 testado.
- Teste isolado (porta 8090, DB de teste): 401 sem token / 200 com token em introspect e GET /; owner-check 401 senha errada, 200 senha certa, 429 após 10 tentativas. Produção revalidado após restart.

### Frontend — commits `8586120` + `19aad29` (hok-frontend-atual), bundle deployado
- `default-settings.ts`: HOK_TOKEN hardcoded (5fae64...) REMOVIDO — agora default vazio; o usuário informa no Settings.
- `use-owner-auth.ts`: hash SHA-256 da senha do owner (5f6c07...) REMOVIDO do bundle. OwnerGate agora valida a senha no servidor via `POST {serverUrl}/auth/owner-check` e guarda o JWT. O bypass via `localStorage.setItem("hokma.owner.v1","1")` não funciona mais.
- Bundle novo `index-EEaAXSj4.js` verificado: 0 ocorrências do token e do hash. Deploy em /var/www/hok-os (backup feito).
- O `alert("DEBUG: pendingAction...")` já havia sido removido em sessão anterior (0 ocorrências no bundle).

## Validação em produção
- `systemctl is-active hokma` OK; `/health` 200; `/introspect` e `GET /` → 401 externo sem token, 200 com token.
- `POST https://app.imoveischaves.com/auth/owner-check` → 401 com senha errada (rota acessível externamente, atrás do proxy).

## Observações / pendências
- **O OwnerGate agora pede a senha de um admin do DB** (ex.: washington@hok.ai, admin@hokma.dev, devwluis@gmail.com) — não é mais o hash hardcoded. Testar login real no navegador.
- Usuários de teste da varredura (teste-idor-*, test_hash@hokma.dev, test2@hok.ai, ton@hokma.io) continuam no DB — candidatos a limpeza futura.
- CORS "*" global mantido (item 4 da varredura): a API exige token nas rotas sensíveis; restringir origem pode quebrar clientes legítimos — decisão deliberada, reavaliar se virar produto público.
- Ainda pendente da varredura: timeouts em comandos aprovados (ExecuteApprovedCommand, resolveTaskAgentPendingAction), X-Forwarded-For confiável (getClientIP), JWT_SECRET fallback para HOK_API_TOKEN, e o teste E2E de criação de workflow via chat (gasta créditos LLM).
