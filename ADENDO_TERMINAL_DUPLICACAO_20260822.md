# Adendo — Bug 2: duplicação de conteúdo no terminal web (replay/reattach) — 2026-08-22

Sessão completa sobre o segundo bug pendente do Terminal web (xterm.js +
WebSocket + PTY bash no backend hokma): o MESMO bloco de texto (relatório de
status tipo "Serviço/Health/Hash deployado/Journal boot") aparecia repetido
3× seguidas, idêntico, sem o usuário rodar o comando mais de uma vez.
Independente do Bug 1 (gate de foreground, commits 351ce4e + 77adf74) —
caminhos opostos do PTY: aquele era INPUT, este é OUTPUT.

## 1. Investigação (evidência real, não suposição)

Três greps dirigidos nas hipóteses levantadas:

1. **Competição pelo io.Reader do PTY** (`PTY|pty.Master|getReader|getWriter`):
   DESCARTADA. `readerLoop` é o ÚNICO leitor do pty; zero ocorrências reais
   além de comentários. O tap do tryTerminalExec é fan-out ADITIVO no
   broadcast (recebe cópia do chunk; nunca lê o pty nem escreve em viewers).
2. **Re-envio do scrollback** (`scrollback|ringBuffer|replay`): CONFIRMADO como
   mecanismo — ring buffer ~512KB guarda tudo desde o início da sessão e o
   attach() reenvia o Snapshot INTEIRO a cada reconexão, sem offset.
   Frontend aplicava o replay com APPEND sobre o xterm existente.
3. **Listener duplicado no frontend** (`ws.onmessage|addEventListener`):
   DESCARTADA. `ws.onmessage` é atribuição (sobrescreve); `outputListeners`
   é Set com unsub no cleanup. Nota: o grep original perdeu
   `TerminalScreen.tsx` por case-sensitivity (`*terminal*`).

## 2. Causas raiz

- **Via 1 (dominante, frontend)**: o scrollback do servidor é "autoritativo"
  (contém TUDO desde o início da sessão), mas o frontend reaplicava-o com
  `term.write()` SEM limpar o xterm (`handleScrollback` limpava só o buffer
  local `s.recent`). Cada reconexão (Android suspende o socket em background;
  reconexão automática via scheduleReconnect/visibilitychange) empilhava
  +1 cópia completa do histórico. Matemática do vídeo: 1 execução real +
  2 reconexões = bloco repetido 3×.
- **Via 2 (backend, race attach×broadcast)**: attach() registrava o viewer em
  `s.viewers` ANTES de tirar o Snapshot(). Chunk lido nessa janela ia ao vivo
  E entrava no snapshot → entregue 2×. Janela de microssegundos, rara sozinha.
- **Agravante (remount)**: ao alternar Chat↔Terminal com conn live, o mount
  escrevia DUAS fontes sobrepostas: saved.history (snapshot visual do
  localStorage) + getRecentOutput (chunks brutos acumulados, nunca consumidos
  destrutivamente). Cada ciclo empilhava cópia adicional.

## 3. Correções implementadas (commits)

| Repo | Commit | Resumo |
|---|---|---|
| backend (hok-backend-atual, main) | 748bdf4 | viewer em `backlog` durante o replay: registro+Snapshot no MESMO lock crítico; chunks da janela vão pra fila `pend` e são entregues 1× em ordem após o ready (`finishBacklog`). Nem duplica, nem perde. |
| frontend (hok-frontend-atual, main) | 0045dec | `subscribeReset` → `term.reset()` (+limpeza do log de leitura) ANTES de reaplicar o scrollback; `takeRecentOutput` destrutivo; fonte única no remount (`recent` OU `history`). |

Arquivo novo no backend: `terminal_dup_fix_test.go` — smoke E2E
(TestTerminalReplayReattachSemDuplicacao: PTY/bash/WS reais, 1 comando digitado
1×, reattach DURANTE stream concorrente; marcador == eco+execução exatamente,
cada linha da bomba == 1×) + teste determinístico da mecânica
(TestBroadcastBacklogFilaOrdemUnica). 10/10 PASS.

## 4. Validação e deploy

- go vet ✓ | suite completa ✓ | build isolado ✓ | tsc + vite build frontend ✓.
- Deploy backend 22/08 14:26 UTC: md5 produção == hokma_test (fdbe2c8c…),
  health {"status":"ok"}. Backup prévio: /var/www/hok-os.bak_bug2fix_20260822_143715.
- Frontend: build PORT=3000 BASE_PATH=/ + rsync dist/public/ → /var/www/hok-os/
  (assets 14:37 UTC).
- Limpeza: models_routes.go (+21, dead code validateModelsAvailable de sessão
  noturna anterior, sem rota registrada) descartado conforme decisão do dono;
  patch preservado em /tmp/models_routes.patch.bak_20260822_131618.

## 5. Lição registrada

Replay autoritativo só é seguro se o cliente RESETAR antes de reaplicar.
Qualquer "snapshot completo + append incremental" exige offset/seq ou reset —
senão cada reconexão vira duplicação cumulativa.
