# Adendo — Sessão 27/08 14:25 · Restart + smoke test do opencode-serve (Fase 3)

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md, ADENDO_SESSAO_20260827_deploy_fase3_producao.md.

---

## Contexto

Revisão da Fase 3 (prompt original de planejamento vs estado real): todos os
itens já estavam executados e em produção. Único item do checklist que faltava:
**restart do serviço + smoke test** (confirmar que o processo sobrevive a
restart sem loop e que a API responde igual antes). Autorizado por Washington
nesta sessão.

## Executado

### Restart (autorizado — serviço isolado nosso, sem impacto no Chat)
```
PID antes: 3603009
systemctl restart opencode-serve
PID depois: 3662490
Status: active (running) desde 14:24:25 UTC
NRestarts=0 (sem loop de restart)
```

### Smoke test via curl (127.0.0.1:4100, senha do .env — não exposta)

| Teste | Resultado |
|---|---|
| Auth sem token | 401 (ok) |
| Auth com token | 200 (ok) |
| `/api/health` | `{"healthy":true}` |
| Criar sessão `/session` | `ses_fbc63f4ceffe7OAFARemoZ1r7Z` |
| Mensagem `/session/{id}/message` | resposta `SMOKE-RESTART-OK` (role assistant, finish stop) |
| `/event` (SSE) | conectou e recebeu `server.connected` |

- Nenhum erro nem divergência vs comportamento validado antes do restart.
- `hokma.service` intacto (active, 8082 OK) — nada de produção do Chat tocado.

## Estado da Fase 3 (revisão completa)

| Item do prompt original | Status |
|---|---|
| Serviço systemd isolado (porta 4100, 127.0.0.1) | ✅ `opencode-serve.service`, `Restart=on-failure` (decisão do usuário — o prompt pedia `always`) |
| `OPENCODE_SERVER_PASSWORD` nova no .env | ✅ (gerada, nunca exposta) |
| Validação curl (auth, /doc, sessão, mensagem, SSE, health) | ✅ (agora inclui o restart+smoke) |
| Cliente Go `opencode_serve_client.go` | ✅ + `opencode_serve_persist.go` (sessão por conv_id) + `opencode_serve_flow.go` (tryOpenCodeServe na cascata) |
| Endpoint de teste isolado (`/opencode/serve/*`, gated por `OPENCODE_SERVE_TEST=1`) | ✅ validado na 8090 e depois deployado |
| Plug no fluxo real do Chat | ✅ Etapa 3 aprovada e deployada (engine `opencode_serve`) + fumaça ponta a ponta fechada |

Divergências do mapeamento registradas anteriormente (mantidas):
- Auth: Basic com usuário fixo `opencode` (não documentado no adendo de 26/08).
- `/summarize` assíncrono (retorna `true`; resumo vira mensagem `compaction` via
  SSE; `session.summary` só git stats).
- Modelo default do serve sem especificação: `thinkingmachines/inkling-small:free`.

## Pendências em aberto (decisões do usuário)
- Etapa 3b (replyPermission + SSE tool approval) — adiada.
- Decommission do `TerminalTTYDScreen` como canal principal — passo 4 do adendo.
- Reativação do xterm.js como terminal visível — em discussão.
- Bug do spin/exit do OpenTUI no terminal visível — decisão: conviver por ora
  (sem mitigação; retomar com opção c se virar prioridade).