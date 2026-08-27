# Adendo — Sessão 27/08 11:15 · Fase 3 Etapa 3 inicial (opencode serve no fluxo do chat) validada

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_SESSAO_20260827_fase3_passo2_persistencia.md, ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md.

---

## Decisões aprovadas (usuário, 27/08)
1. **Gatilho**: manter exceção do bridge ttyd para terminal visível — não rotear
   tudo cegamente para opencode serve.
2. **Summarize automático**: NÃO ativar por limiar de tokens nesta etapa —
   função implementada, trigger desligado (`OPENCODE_SERVE_AUTO_SUMMARIZE=1`
   para ativar depois de validar em produção).
3. **Frontend**: NÃO parar de enviar terminalSession — mínimo absoluto no
   ChatScreen.tsx (nenhuma mudança feita).
4. **Modelo**: modelo ativo do Hokma no sendMessage (padrão dos outros engines).

## O que foi feito (Etapa 3 inicial — sem replyPermission/SSE de tool, fica para 3b)

### opencode_serve_flow.go (novo)
- `tryOpenCodeServe` — engine da cascata, na posição do tryTerminalExec:
  - Gatilho: `containsTerminalKeyword(msg)` OU `forceOpenCode`
  - Exceção §2.1: terminal REGISTRADO pelo frontend (request `terminalSession`
    via `resolveTmuxSession`, ou tabela `terminal_active` via `loadTerminalActive`)
    → deixa a ponte visível cuidar. **Correção de implementação**: NÃO usar
    `registeredActiveTTYD()` — o fallback dela (`tmux ls`) pega qualquer sessão
    tmux do servidor e sequestrava o serve (bug encontrado no primeiro teste:
    resposta veio de `terminal_exec_busy` porque a sessão real do usuário
    existia).
  - Fail closed sem `OPENCODE_SERVE_PASSWORD`; health check com cache de 15s
  - Blocklist de segurança (mesmo princípio do tryTerminalExec)
  - `getOrCreateOpenCodeServeSession` (persistência por conv_id, Etapa 2)
  - Modelo ativo do Hokma no `sendMessage` + `system` com persona
    (`smartChatSystemPrompt`) e instrução de modo plan
  - Aprovação: prompts destrutivos → `setPendingAction(..., "opencode_serve", ...)`
    → `opencode_serve_pending` (mesmo fluxo do tryOpenCode)
  - Falha (serve fora do ar / resposta vazia / erro) → retorna nil → cascata
    cai para `tryTerminalExec` legado (comportamento anterior preservado)
- `maybeOpenCodeServeSummarize` — implementada, DESLIGADA por default
- `resolveOpenCodeServePendingAction` — execução da ação aprovada na sessão
  persistente da conversa

### smart_chat.go (backup .bak_20260827_105229_fase3)
- `tryOpenCodeServe` inserido na cascata ANTES do `tryTerminalExec`

### pending_action.go (backup .bak_20260827_105229_fase3)
- `case "opencode_serve"` no `resolvePendingAction` → resolver novo

### opencode_serve_client.go (backup .bak_20260827_105229_fase3)
- `openCodeServeMessageOpts.System` + envio do campo `system` no body do
  `/session/{id}/message` (o spec aceita; faltava no struct)

## Validação (isolado: porta 8090, DB de teste, OPENCODE_SERVE_TEST=1)

| # | Cenário | Resultado |
|---|---|---|
| 1 | Nova conversa (convT1), keyword terminal | `opencode_serve` — pwd correto (`/tmp/opencode/fase3-test`) |
| 2 | Reabrir convT1 (contexto) | mesma sessão, lembrou o comando anterior (contexto nativo) |
| 3 | convT2 nova | sessão própria, isolada (3 sessões no banco) |
| 4 | Blocklist `rm -rf /` | `opencode_serve_blocked` |
| 5 | Destrutivo com keyword → aprovação | `opencode_serve_pending` → "sim" → arquivo REALMENTE removido |
| 5b | Destrutivo SEM keyword | `opencode` (CLI) — comportamento pré-existente, corretamente preservado |
| 6 | Serve derrubado (pkill) | fallback `terminal` (tryTerminalExec legado) |
| 7 | Serve religado | volta a `opencode_serve`, mesma convT5 → nova sessão persistida |
| — | go vet + testes existentes | limpos / ok (4.1s) |
| — | produção | hokma.service ativo 8082, intocada |

Notas do teste 6: o `pkill -x opencode` derrubou o serve de teste (4100) —
o usuário estava com uma sessão `opencode --continue` própria no terminal
(pts/0), NÃO afetada. O serve de teste foi religado (4100, senha de teste).

## Pendências / próximos passos
- **Proposta de deploy** (aguardando aprovação explícita separada — não deployar
  sem isso): .env com OPENCODE_SERVE_URL/PASSWORD em produção, systemd unit do
  `opencode serve` (porta interna, Restart=always), deploy do binário, testes
  de fumaça em produção.
- Etapa 3b (futura): replyPermission + SSE de tool approval.
- Commit da Etapa 3 sugerido (aguardando confirmação): opencode_serve_flow.go,
  smart_chat.go, pending_action.go, opencode_serve_client.go + este adendo.
- drive_creds.env: refresh token expirado (explicado em sessão; NÃO alterado).