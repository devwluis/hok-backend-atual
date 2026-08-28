# Adendo — Sessão 27/08 22:30 · FASE MAIOR CONCLUÍDA: jobs em background + retomada (itens 2+3)

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_PLANO_FASE_MAIOR_JOBS_BACKGROUND.md (plano registrado), investigação dos 4 bugs (sessão 27/08).

---

## Status final

**Fase maior (jobs em background + retomada) CONCLUÍDA e VALIDADA em produção com uso real.** Fecha o ciclo dos 4 bugs investigados (item 1: status OK; item 4: label dos engines; itens 2+3: este adendo).

## O que foi implementado

**Backend (commit 9b726c4, hok-backend-atual):**
- `chat_jobs.go` (novo): `chatJob` em memória; `startChatJob` roda em goroutine com `context.Background()` + timeout 10min (sobrevive à desconexão); `GET /chat/job` (`?id=` e `?conv_id=`); `persistChatJobMessages` grava a troca em `conv_messages`; `pending_action` da conversa no retorno do job (card reaparece na retomada).
- `smart_chat.go`: `req.Async` → cria o job e retorna `{job_id, mode:"job_running"}` na hora; `runSmartText` com ctx próprio; fluxo síncrono mantido como compat.
- `types.go`: campo `Async`.

**Frontend (commits ebef6b3 + eaa0a00, hok-frontend-atual):**
- Envio `async:true` → polling 2s em `GET /chat/job` até `done` (AbortController).
- `sendWatchdog` de 180s removido — o loading segue o status do job (tarefas longas mantêm a bolha).
- Retomada no mount: `GET /chat/job?conv_id=` → running: retoma polling; done: adiciona a resposta automaticamente + `persist` no conversationsStore.

## OS 3 BUGS DO CENÁRIO SEM SERVER URL (causa da diferença smoke vs uso real)

O smoke testava o backend direto (127.0.0.1:8082); o app real roda no próprio domínio sem "Server URL" configurado — 3 pontos causavam a discrepância:

1. **Guarda `!loadingRef.current` no useEffect de retomada** — ao voltar para a aba, o ChatScreen remonta com `loadingRef=false` → a retomada nunca disparava. Corrigido: guarda removida (retomada roda sempre no mount).
2. **`endpointPath = "/api/chat"` sem Server URL** — o `/api/chat` cai no `handleRoot` (fluxo de chat ANTIGO, síncrono, SEM async/jobs) → o `async:true` era ignorado → nenhum job era criado → a retomada não encontrava nada. Corrigido: `endpointPath = "/chat/smart"` SEMPRE (o nginx do próprio domínio proxyia `/chat/*` para o backend).
3. **Header de auth sem Server URL** — enviava `Authorization: Bearer <HOK_TOKEN>` (a chave não é JWT → 401 no backend). Corrigido: `X-Hok-Token` em todos os casos.

+ Retomada com `baseUrl = serverUrl || window.location.origin` (o app sem Server URL usa o próprio domínio).

## Validação

- **Isolado**: cenário real replicado via https/nginx (caminho do navegador sem Server URL): envio → job → done → retomada → resposta + persistência. Card via job (pending_action + aprovação executou).
- **Produção (smoke 5/5)**: envio via `https://app.imoveischaves.com/chat/smart` + `X-Hok-Token` → job → done → retomada → `conv_messages`. Bundle `index-On-Jneb1.js`.
- **Uso real confirmado por Washington**: enviou, trocou de aba, voltou e a resposta apareceu automaticamente. ✅

## Estado da Etapa B (relacionada)
O card de aprovação (Etapa B) segue funcionando no fluxo job (pending_action no retorno). Config `ask` em produção permanece pendente de decisão (opção B mantida).

## Próximas pendências (registradas em outros adendos)
- Item de acompanhamento: sessão "zumbi" pós-TTL (recriar sessão).
- Bug OpenTUI do terminal visível (conviver por ora; retomar com opção c se virar prioridade).
- Popup/reconnect do terminal visível (mesma regra).
- drive_creds.env (refresh token; renovação pendente de autorização).