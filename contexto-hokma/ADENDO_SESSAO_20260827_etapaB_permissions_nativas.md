# Adendo — Sessão 27/08 18:45 · Etapa B: permissions nativas do opencode serve (implementada, inerte em produção por decisão)

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md, ADENDO_SESSAO_20260827_etapaA_serve_padrao.md.

---

## Objetivo (aprovado por Washington)

Etapa B do roteiro da Fase 3: aprovação de tool via mecanismo NATIVO do opencode
serve (permission.asked via SSE + POST /session/{id}/permissions/{id}), com
decisão automática (sem UI ainda): once para baixo risco, reject para blocklist
e fail-safe para o resto. Fallback legado (pending_action) intacto para serve
fora do ar.

## Implementado (backend, commit 23296e1)

1. **Watcher SSE por sessão** (`ensureOpenCodeServeWatcher` em
   opencode_serve_flow.go): consome `/event`, filtra `permission.asked`,
   decide e responde via `POST /session/{id}/permissions/{id}` (payload
   `{"response": "once"|"always"|"reject"}` — schema confirmado no /doc).
   Reconexão automática.
2. **Decisão automática** (`decideOpenCodeServePermission`): comandos da
   blocklist (terminalExecBlocklist) → `reject`; prefixos seguros de
   leitura/echo (`echo/ls/cat/pwd/whoami/date/printf/head/tail/grep/find/
   wc/hostname/uptime/df/free/ps/env/which/type/true/false/uname/id/getent`)
   → `once`; **qualquer outra → `reject` fail-safe** (sem card de aprovação
   ainda).
3. **Caminho async para mensagens com tools** (`openCodeServeNeedsTools` =
   needsRealTools + containsTerminalKeyword): sendMessage em goroutine com
   timeout gerenciado de 240s; mensagens simples seguem síncronas.
4. **Gate legado removido do caminho serve** (pending_action continua intacto
   nos caminhos legados — tryTerminalExec/tryOpenCode).

## Bugs encontrados e corrigidos durante implementação/validação

| Bug | Correção |
|---|---|
| `prompt_async`/`noReply=true` NÃO inicia o processamento no binário 1.18.23 (0 eventos; o bloqueante inicia) | async usa sendMessage em goroutine + timeout, não prompt_async |
| SSE do client morria a cada ~320s (http.Client.Timeout global cortava o stream longo) | eventStream usa client próprio SEM timeout (ctx controla a vida) |
| `metadata` de permission.asked pode conter arrays (external_directory: directories/patterns) → unmarshal em map[string]string falhava EM SILÊNCIO (permission nunca respondida) | `map[string]interface{}` + extração do `command` |
| `openCodeServePart.State` era string mas o state da tool é objeto → decode quebrava | `json.RawMessage` |
| Modelos podem terminar SEM texto após reject | aviso padrão de bloqueio (em vez de cascata) |

## Validação isolada (serve teste 4111 com config bash:ask/edit:ask + backend 8090) — 7/7 PASS

| Cenário | Resultado |
|---|---|
| Mensagem simples (síncrona) | opencode_serve |
| `echo` → permission | once automático → resposta |
| `ls` → permission | once → resposta |
| `mkdir` → permission external_directory | reject automático → aviso, dir NÃO criado |
| `rm -rf /` | bloqueado no backend (opencode_serve_blocked) |
| Sessão após reject (mesma conv) | continua funcional (não trava) |
| go vet + testes existentes | limpos |

## Smoke test em produção (deploy aprovado) — RESULTADO REAL

| # | Teste | Resultado |
|---|---|---|
| 1 | Mensagem simples | ✅ opencode_serve |
| 2 | `echo` | ✅ respondeu — por **allow** (config produção), não por once |
| 3 | `mkdir -p /tmp/hok-smoke-b` | ⚠️ **EXECUTOU** (dir criado) — reject NÃO exercitado |
| 4 | Sessão segue funcional | ✅ |
| — | AUDIT permission no journal | **VAZIO** (nenhum permission.asked em produção) |

### Explicação (config efetiva)
O serve de produção (cwd /root/hokma/backend) usa a config GLOBAL
`~/.config/opencode/opencode.json` com **bash: allow / edit: allow** → nenhuma
permission é pedida → o watcher fica **inerte**. O backend/opencode.json só
restringe o agent custom my-agent, não o build usado pelo serve.

### Decisão de Washington (opção B)
**Manter bash/edit em allow** por enquanto: o fail-safe reject sem UI de
aprovação quebraria o fluxo produtivo do chat (edição/execução real). A Etapa B
fica registrada como **implementada e validada, inerte em produção**, e será
**ativada quando o card de aprovação do usuário (UI) existir** — que destrava a
opção A (config ask) sem quebrar o fluxo.

## Commit/push
- Backend: `23296e1` → push `d90089e..23296e1` em hok-backend-atual (aprovado).

## Próxima etapa (plano curto — UI do card de aprovação)
Ver relatório da sessão (plano curto entregue a Washington).