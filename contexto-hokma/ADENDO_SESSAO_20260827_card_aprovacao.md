# Adendo — Sessão 27/08 19:15 · Card de aprovação (Etapa B, parte 2) implementado + deploy

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_SESSAO_20260827_etapaB_permissions_nativas.md (Etapa B parte 1), plano detalhado do card (revisado e aprovado por Washington).

---

## Objetivo

Card de aprovação do usuário para permissions sensíveis do opencode serve
(destrava a opção A — config ask — sem quebrar o fluxo). Implementado,
validado isolado (7/7) e deployado no backend. **Config ask em produção NÃO
ativada** — decisão separada, aguardando aprovação explícita após o backend
rodar estável com o card.

## Implementado (backend)

1. **Decisão tri-state** (`decideOpenCodeServePermission`): blocklist → `reject`
   direto; prefixos low-risk → `once` direto; **resto → CARD**.
2. **Card** (`openCodeServeAskUser`): cria `pending_action`
   `"opencode_serve_perm"` (descrição `permission: comando`) + canal que faz o
   async devolver "Confirma? (responda sim/nao)" NA HORA (execução em
   background até a decisão).
3. **TTL do card**: `openCodeServeCardTTL = 120 * time.Second` — **constante
   ÚNICA** (ajustável em um lugar). Sem resposta → `reject` automático.
4. **Aprovação recente** (`serveApproved`, janela 90s): permissions
   SUBSEQUENTES da mesma execução (ex.: mkdir pede external_directory e depois
   bash) → `once` automático após o usuário aprovar a primeira.
5. **Resolver** (`resolveOpenCodeServePermPendingAction`): "sim" → `once` +
   devolve o RESULTADO real da execução (polling do histórico); "não" → `reject`.
6. **Abort no timeout** do async (`POST /session/{id}/abort`) — a sessão não
   fica busy com tool pendente.
7. `openCodeServeSessionOwner` — mapeia session_id → conv/tenant/user via
   session_mode (necessário para o card).

## Bugs corrigidos durante a validação

| Bug | Correção |
|---|---|
| TTL rejeitava a permission JÁ APROVADA quando outra pendência surgia na mesma conversa (2 permissions encadeadas) | TTL checa pelo **actionID específico** (`cur.ID == pa.ID`) |
| 2ª permission encadeada (external_directory → bash) nunca era aprovada | **recent-approve** (90s após aprovação → once automático) |
| `info.summary` do GET /message é OBJETO → decode falhava → waitResult sempre dava timeout | `Summary` → `json.RawMessage` |
| `prompt_async`/noReply não inicia processamento (1.18.23) — já documentado | async = sendMessage em goroutine + timeout |
| SSE do client morria a cada 320s | eventStream com client sem timeout (ctx controla) |
| metadata com arrays (external_directory) | map[string]interface{} + extração do command |
| State da part tool é objeto | json.RawMessage |

## Validação isolada (serve 4111 com ask + backend 8090) — 7/7 PASS

| Cenário | Resultado |
|---|---|
| Mensagem simples (síncrona) | opencode_serve |
| `echo` → permission | once automático |
| `ls` → permission | once automático |
| `mkdir` → CARD → **aprovar** | executou + resultado real ("Criado com sucesso... 755") |
| `mkdir` → CARD → **rejeitar** | "Permissão negada", dir não criado |
| **TTL** (120s sem resposta) | reject automático, dir não criado, sessão não presa (processa novas mensagens) |
| `rm -rf /` | bloqueado no backend |

go vet + testes existentes limpos.

## Deploy em produção (aprovado)

- Backups: `hokma.bak_predep_card_20260827_191527`, `.env`, `memory.db`.
- Build → substituir → restart → hokma active, 8082 OK, opencode-serve active.
- **Config produção segue `bash: allow / edit: allow`** (Etapa B inerte até a
  ativação da opção A — decisão separada).

### Smoke test em produção — 4/4 PASS
| Teste | Resultado |
|---|---|
| Mensagem simples | opencode_serve (SMOKE-CARD-1) |
| Comando (allow → direto) | opencode_serve (SMOKE-CARD-2) |
| Panics no journal | 0 |
| Binário com o card | sim (opencode_serve_perm presente) |

## ITEM DE ACOMPANHAMENTO — Sessão "zumbi" pós-TTL

- Comportamento observado na validação: numa sessão onde o TTL do card
  rejeitou uma permission, as MENSAGENS SEGUINTES da mesma sessão às vezes
  retornam resposta vazia (o modelo/servidor responde step-finish sem texto),
  enquanto sessão nova funciona normal.
- Causa: estado da sessão após reject (comportamento do modelo/servidor, não
  do código Hokma — o fluxo do card funciona).
- **Mitigação conhecida**: recriar a sessão da conversa (nova session_id).
- **Ação quando perceber no uso real**: se uma conversa passar a responder
  vazio após um card rejeitado por TTL, recriar a sessão (remover a linha em
  session_mode da conversa ou reiniciar a conversa). Não exige código agora —
  acompanhamento.

## Próximos passos (aguardando decisão)
- Commit/push desta rodada (aguardando aprovação explícita do smoke).
- Ativação da config `ask` em produção (opção A) — separada, só com aprovação
  explícita após o backend estável com o card.
- Frontend: o card já tem botões Aprovar/Rejeitar (fluxo existente); ajuste de
  apresentação da descrição quando a opção A ativar.