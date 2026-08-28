# Adendo — Terminal persistente do HOK OS (pty desacoplado do WebSocket) — 2026-08-20

Sessão completa sobre o Terminal web (xterm.js + WebSocket + PTY bash no
backend hokma). Abrange a investigação e correção de três sintomas
encadeados e a implementação do terminal persistente estilo tmux/screen.

## 1. Sintomas e contexto

1. **Quedas de conexão "aleatórias"** do Terminal — OFFLINE exigindo
   reconexão manual, pior ao trocar de aba (Chat/N8N/Config).
2. **Lixo no prompt após encerrar uma TUI** (opencode/claude):
   `35;36;23M0;36;23M0;36;23m` → `bash: 35: command not found` — respostas
   de terminal emulador órfãs.
3. **Sessão resetando ao trocar de aba** com TUI rodando — "sessão PTY real
   iniciada" de novo + prompt limpo, como se um pty novo tivesse sido criado.
4. Pedido final: terminal persistente — a sessão pty sobrevive ao fechamento
   do navegador/refresh/suspensão do Android, com scrollback recuperável.

## 2. Causas raiz (todas confirmadas por reprodução)

- **Quedas/reconexões**: o WebSocket era o dono do pty — QUALQUER queda
  (suspensão da aba pelo Chrome Android, rede, refresh) matava o bash e o
  processo filho. Caminho real do WS: `wss://api.imoveischaves.com/terminal/ws`
  → Cloudflare → cloudflared → backend:8082 (`app.*` via nginx:3002 retorna
  400 — sem headers de Upgrade). Testado: Cloudflare não derruba WS idle
  (heartbeat 25s mantém vivo); o gatilho é a suspensão do navegador mobile.
- **Lixo CPR**: TUIs enviam `ESC[6n` (Cursor Position Report); o xterm.js
  responde `ESC[<linha>;<col>R` via onData → input no pty. Se a TUI encerra
  antes de consumir, a resposta fica órfã no buffer e o **readline do bash
  (modo raw) a lê na hora** → executa fragmentos como comandos. O guard
  inicial só filtrava quando o bash já era o foreground (lacuna TOCTOU quando
  a resposta chegava durante o exit da TUI).
- **Regressão de sessão (troca de aba)**: `connect()` no remount sempre fazia
  `teardown()` — derrubava o socket vivo que o TerminalProvider mantinha,
  matando o bash. `ensureConnected()` resolveu (só conecta se não há socket
  OPEN/CONNECTING).

## 3. Correções implementadas (commits)

| Commit | Escopo | Resumo |
|---|---|---|
| `dc63009` | backend | Heartbeat no WebSocket: PING 25s, PONG renova deadline 90s, encerra clientes mortos sem órfãos. |
| `e6a6bb5` | frontend | TerminalProvider global (socket sobrevive à troca de aba), guard de identidade, auto-reconnect com backoff, replay de output. |
| `0af1c43` | backend | Guard do CPR: descarta respostas CPR/DSR/DA órfãs quando o bash é o foreground do pty. |
| `4cfd019` | backend | Fecha a lacuna TOCTOU (hold 100ms + recheck do pgrp) e corrige race de escrita do WS (mutex ping×output). |
| `cc4db88` | frontend | `ensureConnected()` no mount — sessão preservada ao trocar de aba (sem derrubar o socket vivo). |
| `07b87c3` | backend | **Terminal persistente** (ver abaixo). |
| `27fc0f3` | frontend | Reattach via `session_id` no localStorage + tratamento das mensagens de controle. |

## 4. Terminal persistente (tmux/screen-like)

**Arquitetura**: o pty/bash agora roda como processo do backend, desacoplado
de qualquer WebSocket. A conexão WS é apenas um "viewer" da sessão — pode
cair que o processo continua vivo (comandos, diretório, scrollback e
processos filhos sobrevivem). A sessão só morre por:

- `exit`/Ctrl+D do usuário — detecção via `cmd.Wait()` (não só EOF do pty,
  pois processos em background que seguram o slave fd impediriam o EOF);
- timeout de inatividade longo (TTL 24h, configurável);
- restart do serviço `hokma` (sessões vivem em memória).

**Estrutura de dados** (`terminal_session.go`, escolha: memória do processo):

- `TerminalSession`: `session_id` (aleatório), `userKey` (derivado do token),
  pty master, `*exec.Cmd` (bash), `bashPgrp`, ring buffer de scrollback
  (512KB/sessão), viewers (conexões WS espelhadas), `lastUsed`, `closed`.
- `terminalSessionRegistry`: mapa `sessionID → *TerminalSession` + `byUser`
  (uma sessão por token). Múltiplos viewers (abas/dispositivos com o mesmo
  token) **espelham a mesma sessão** (estilo `tmux attach`).
- Por que memória e não SQLite: um pty não sobrevive a restart do serviço de
  qualquer forma — persistir em disco não traria estado real do processo.

**Protocolo no WS** (mensagens de controle antes do stream ao vivo):

- `{"type":"session","session_id":"<id>","created":bool}` — frontend salva o
  id no localStorage (`hokma.terminal.session.v1`) e só mostra o banner
  "sessão PTY real iniciada" quando `created:true` (sessão nova); em reattach
  (`created:false`) não mostra.
- `{"type":"scrollback","data":"<base64>"}` — replay do buffer persistente
  antes do stream ao vivo (decodificado para UTF-8 no frontend).
- `{"type":"ready"}` — fim da fase de attach; a partir daqui é texto cru.

**Frontend** (`use-terminal.tsx` + `TerminalScreen.tsx`):

- Envia `?session_id=` salvo no reconnect/refresh para reattach.
- No mount decide replay: socket vivo (troca de aba) → histórico local +
  output recente; conexão nova → espera o scrollback do servidor (não
  duplica).
- Aviso sutil "Sessão anterior expirada — nova sessão iniciada" quando a
  sessão antiga não existe mais.

**Guard do CPR preservado**: o `writeTerminalInput` (drop quando bash é o
foreground + hold 100ms com recheck do pgrp quando há TUI) opera no pty da
sessão — validado que continua descartando órfãos na nova arquitetura.

## 5. Validação

- `go build` + `go vet` + `go test ./...` 100% verdes (incl. `terminal_session_test.go`:
  ring buffer ordem/caps e registro reattach/exit; `terminal_ws_test.go`: stripper CPR).
- Frontend `tsc --noEmit` + `vite build` OK.
- **Smoke isolado (porta 18094)**: sessão criada → `cd /tmp` + `sleep 300 &` →
  WS derrubado → reattach ao mesmo `session_id` (`created:false`) → scrollback
  entregue e **`sleep 300` VIVO** → `exit` fecha a sessão → nova conexão cria
  sessão nova (`created:true`).
- **Pós-deploy produção (8082)**: mesmo fluxo validado (reattach mesma sessão,
  processo sobreviveu, exit fechou).
- Guard do CPR re-testado na sessão (órfão descartado).

## 6. Deploy

- Backend: `hokma.bak_persist_20260820_*` → stop → swap → start (PID 2721014);
  backup do source `terminal_ws.go.bak_persist_20260820_143238`.
- Frontend: `/var/www/hok-os.bak_persist_20260820_*` → swap do dist.
- Commits pushados: backend `07b87c3`, frontend `27fc0f3` (+ os demais da tabela).

## 7. Como reproduzir o teste manual

Abrir o Terminal → rodar algo de longa duração (`opencode`, `tail -f`,
`sleep 300`) → **fechar o navegador de verdade** → reabrir
`app.imoveischaves.com` → o Terminal deve **reconectar na MESMA sessão**
(mesmo diretório, histórico e processo rodando). Trocar de aba continua
funcionando.

## 8. Limitações conhecidas

- Restart do serviço `hokma` ou TTL de 24h sem nenhuma reconexão → sessão
  perdida (nova criada, com aviso).
- Múltiplos dispositivos com o mesmo token espelham a mesma sessão (por
  design).
- `app.imoveischaves.com` (via nginx:3002) ainda não faz upgrade WebSocket
  (400 — falta `proxy_set_header Upgrade/Connection`); o terminal usa
  `api.imoveischaves.com` (direto ao backend:8082). Pendência opcional:
  adicionar headers de Upgrade no nginx.
- Guard do CPR: o hold de 100ms pode não cobrir saídas de TUI mais lentas que
  100ms após a última resposta — residual, mitigado pelo terminal persistente
  (sessão não é mais morta no meio de uma resposta).