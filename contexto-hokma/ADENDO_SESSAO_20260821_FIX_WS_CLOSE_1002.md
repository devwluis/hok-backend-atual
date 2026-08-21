# ADENDO — SESSÃO 21/08/2026 — TERMINAL: FIX WS CLOSE 1002 (INVALID UTF-8 EM TEXT FRAME) — RECONEXÕES EM LOOP

Sessão após o `ADENDO_SESSAO_20260821_3fixes_terminal.md`. Backend em `/root/hokma/backend` (repo `devwluis/hok-backend-atual`), frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082), nginx :3002.

## 1. SINTOMA (evidência — journalctl -u hokma)
```
Aug 21 15:48:12 hokma hokma[2841819]: [term-ws] desconexao user=3873e8e8daac4ca8 session=a6c47b47f0c0fa18ec1f9fb6 reattach=true closeCode=1002 reason="Invalid UTF-8 in text frame" bashPgrp=2842044 procAlive=true
```
Erro repetindo em loop a cada poucos segundos (15:48:12, 15:48:18, 15:48:20, 15:48:21, 15:48:23...) — frontend piscando "Online/Offline" por reconexão constante.

## 2. CAUSA RAIZ
Não é rede nem memória. O `broadcast()` de `terminal_session.go` enviava o output **cru do PTY** com `websocket.TextMessage` (opcode 0x1). O protocolo WebSocket exige que frames TEXT sejam UTF-8 puro; o PTY pode emitir bytes que não formam UTF-8 válido (escape OSC `rgb:6e6e/e7e7/b7b7...`, bytes crus de TUI do opencode, `cat` de arquivo binário) → o browser é OBRIGADO a fechar a conexão com **1002** → loop de reconexão.

## 3. FIX APLICADO (mudança cirúrgica, 3 arquivos)

### 3.1 Backend — `terminal_session.go`, função `broadcast()`
```diff
- v.conn.WriteMessage(websocket.TextMessage, chunk)
+ v.conn.WriteMessage(websocket.BinaryMessage, chunk)
```
Stream do PTY passa a trafegar como frame BINÁRIO (opcode 0x2) — aceita qualquer byte. Frames de controle (session/scrollback/ready/session_error) continuam TEXT (o frontend os parseia como JSON). Nenhuma lógica de negócio, autenticação ou sessão foi alterada.

### 3.2 Frontend — `use-terminal.tsx` (obrigatório em par com o backend)
- `ws.binaryType = "arraybuffer"` na criação do socket (garante `ev.data` como ArrayBuffer, não Blob);
- `onmessage` agora aceita frames binários: `new TextDecoder("utf-8").decode(new Uint8Array(ev.data))` — bytes inválidos de UTF-8 viram U+FFFD, que o xterm.js renderiza sem quebrar, e o browser NÃO fecha mais com 1002. Sem essa mudança o terminal ficaria mudo (o código antigo descartava `ev.data` não-string).

### 3.3 Teste de regressão novo — `terminal_binary_fix_test.go`
Abre WS real (httptest) + sessão pty, faz `broadcast` de um chunk com bytes inválidos (`0xFF`, `0xFE`) + escape OSC e exige frame BINÁRIO íntegro no client. Passa.

## 4. VALIDAÇÃO (tudo verde, ambiente LOCAL)
- Backups conforme workflow: `terminal_session.go.bak_wsfix_20260821_174423`, `use-terminal.tsx.bak_wsfix_20260821_174423` (e `hokma.bak_wsfix` quando o binário de produção for trocado);
- `go build -o hokma_test .` OK — binário isolado, NÃO sobrescreveu o de produção;
- `go vet .` OK; `go test ./...` → `ok hokma_backend` (suíte completa, ~34s; obs: testes exigem `HOK_TOKEN` no ambiente — usado valor descartável, `.env` real não foi lido);
- Frontend: `npm run typecheck` OK + `vite build` OK (com `PORT`/`BASE_PATH`, exigidos pelo config);
- Teste E2E do fix: stream com bytes inválidos chegou íntegro como frame binário (38 bytes).

## 5. ESTADO / PRÓXIMOS PASSOS
- **NÃO** houve restart/deploy em produção — aguardando autorização explícita do usuário.
- Deploy pendente: 1) backend `systemctl stop hokma` → `cp hokma_test hokma` → `systemctl start hokma` (backup `hokma.bak_wsfix_<ts>`); 2) frontend deploy do build (`dist/public`) com backup `/var/www/hok-os.bak_wsfix_<ts>`; 3) smoke manual: abrir terminal, `ls --color=always`, abrir o opencode, conferir journalctl SEM novos `closeCode=1002`.
- Observação: diff pré-existente não commitado em `models_routes.go` (`validateModelsAvailable`) NÃO é desta sessão — deixado intacto.

**Data/Hora:** 21/08/2026
**Status:** Causa raiz identificada, fix implementado e validado localmente; deploy aguardando confirmação do usuário.