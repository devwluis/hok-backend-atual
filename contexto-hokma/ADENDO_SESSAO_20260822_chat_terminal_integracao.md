# ADENDO — SESSÃO 22/08/2026 — INTEGRAÇÃO CHAT ↔ TERMINAL (`tryTerminalExec` NA CASCATA) [IMPLEMENTADO E VALIDADO LOCALMENTE]

Sessão após `ADENDO_SESSAO_20260822_refactor_runsmarttext.md` (cascata `runSmartTextCascade`, commit `3bed50a`). Backend em `/root/hokma/backend`.

## 1. O QUE FOI IMPLEMENTADO
Comandos do chat admin agora executam na sessão PTY ativa do terminal web, com output capturado e devolvido limpo no chat.

### Arquivos (+247/−22)
| Arquivo | Mudança |
|---|---|
| `smart_chat.go` | **+7**: inserção `res = tryTerminalExec(msg, userID)` na cascata — após n8n, ANTES do skill router (comando explícito não pode ser sequestrado pelo fuzzy match). Nenhum branch existente alterado |
| `terminal_session.go` | **+28**: mecanismo de *taps* — campo `taps map[chan []byte]struct{}`, fan-out best-effort no `broadcast()` (dentro do mesmo lock; chunk copiado; tap cheio descarta), `addTap()/removeTap()` |
| `terminal_exec.go` | Reescrita completa do rascunho (~190 linhas): `extractTerminalCommand`, `tryTerminalExec`, `cleanCapturedOutput`, blocklist, auditoria |

### Gatilho (superfície apertada de propósito)
- Explícito: `/terminal <cmd>` ou `/term <cmd>`;
- Linguagem natural: SOMENTE iniciando com verbo (`rode/roda/execute/executa/executar/run`) → comando extraído via `extractBashCommand` + limpeza de sufixos de cortesia ("pra mim", "por favor"...);
- Heurísticas largas de `containsTerminalKeyword` ("comando", "status do"...) NÃO executam nada sozinhas — sem comando extraível → nil → cascata segue para claude_code/chat. Teste T5 prova: *"como crio um comando alias?"* não executa.

### Segurança em camadas
1. Alcance arquitetural: só `/chat/smart` + `requireHokAuth` chega à cascata (leads/WhatsApp usam outros fluxos);
2. Sessão-alvo SEMPRE a do próprio token admin: `findActiveSession(terminalUserKey(HOK_TOKEN))` — chat nunca digita em PTY de terceiro;
3. Blocklist case-insensitive: `rm -rf /`, `rm -fr /`, `mkfs`, `dd if=`, `> /dev/sd`, shutdown, reboot, halt, poweroff, init 0/6, fork bomb, `chmod -r 777 /` → `mode="terminal_exec_blocked"` + `[AUDIT] … BLOQUEADO`;
4. Auditoria de TODO comando: `[AUDIT] terminal_exec user/session/cmd/ts`.
Sem sessão ativa → mensagem clara `"⚠️ Nenhuma sessão de terminal ativa no momento…"` (não silencioso).

## 2. MECANISMO DE CAPTURA (e os 3 bugs descobertos no smoke)
Fluxo: `addTap()` → escreve `cmd + "\necho ___HOK_CMD_DONE_<nanotime>___\n"` (marcador em linha própria, SEM `&&` — roda mesmo com exit≠0) → loop select drena o tap até o marcador aparecer / timeout 15s → `cleanCapturedOutput` → removeTap (defer).

Bugs reais encontrados ao comparar com bytes capturados por probe (TTY real):
1. **Eco da line discipline**: o TTY ecoa o input inteiro instantaneamente — o 1º chunk já continha o texto do marcador, encerrando a captura em 7ms. Fix: completo quando `strings.Count(raw, marker) >= 2` (1ª = eco, 2ª = execução; nanotime impossibilita colisão);
2. **Break prematuro no clean**: o eco da 2ª linha (`echo <marker>`) disparava o corte antes do output real. Fix: só a linha standalone (`== marker`) encerra; variantes com "echo "/prompt são puladas;
3. **Redesenho do readline** vazava como ruído → descartado enquanto não houver primeiro output real; prompts PS1 filtrados via regex `^\S+@\S+:\S+[#$]$`.

Timeout: 15s → output parcial + nota `_[comando ainda em execução, output parcial]_`. ANSI/OSC/DCS strippados antes de responder. Resposta com `mode="terminal_exec"`, `engine="terminal"` (bloqueios: `terminal_exec_blocked`).

## 3. VALIDAÇÃO LOCAL (5 cenários, PTY real, teste temporário removido após)
| Cenário | Resultado |
|---|---|
| `/terminal echo teste123` c/ sessão | `reply="teste123"` limpo + `[AUDIT]` ✓ |
| Sem sessão ativa | `"⚠️ Nenhuma sessão de terminal ativa no momento…"` + AUDIT ✓ |
| `/terminal sleep 20` | Timeout em 15.02s + nota parcial ✓ |
| `/terminal rm -rf /` | `terminal_exec_blocked` ⛔ + AUDIT ✓ |
| NL sem verbo c/ "comando" | nil → cascata segue ✓ |

`go build -o hokma_test .` OK · `go vet .` OK · suíte completa OK.
Backups: `*.bak_termexec2_20260822_084856`.

## 4. ESTADO
- Commit deste adendo + integração: pushado na main do `hok-backend-atual`;
- **Produção NÃO deployada** — serviço segue no binário `2272211b…` (commit `3bed50a`). Deploy aguardando autorização explícita (mudança toca produção real de chat).
- Pós-deploy sugerido: abrir Terminal web (cria a sessão), mandar `/terminal ls -la` no chat e conferir output limpo + `[AUDIT]` no journalctl; testar `/terminal sleep 20` p/ parcial.

**Data/Hora:** 22/08/2026 ~09:10 UTC
**Status:** Integração completa implementada e validada localmente (5/5 cenários); commitado/pushado; deploy pendente de autorização.