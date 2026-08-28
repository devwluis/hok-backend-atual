# Adendo — Sessão 28/08 03:55 · Pendência 1: sessão "zumbi" pós-TTL — detecção automática (concluída)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_20260827_fase_maior_concluida.md (pendências), ADENDO_SESSAO_20260827_card_aprovacao.md (sessão zumbi, item de acompanhamento).

---

## Objetivo (pendência 1 do roadmap)

Automatizar a detecção da sessão zumbi: quando o TTL do card (120s) rejeita uma
permission e a sessão passa a responder VAZIO (step-finish sem texto), recriar a
sessão automaticamente em vez de esperar o usuário perceber.

## Implementado (backend, backups .bak_..._zumbi)

1. **`opencode_serve_flow.go`**:
   - Contador `serveZombie` por sessionID: incrementa em resposta vazia SEM
     tool; zera em resposta válida/card.
   - Limiar: `openCodeServeZombieThreshold = 2` (constante única, ajustável).
   - Ao atingir o limiar: **recria a sessão** — `clearOpenCodeServeSessionID`
     (DELETE em session_mode) + `getOrCreateOpenCodeServeSession` (sessão nova
     persistida) + **reenvio automático da mensagem 1x** na sessão nova.
   - Se o reenvio também vier vazio → erro claro ("sessão recriada, mas o
     modelo ainda responde vazio — reenvie a mensagem").
   - Logs `[AUDIT] ... sessão ZUMBI detectada — recriando` e
     `sessão recriada X → Y`.
2. **`opencode_serve_persist.go`**: `clearOpenCodeServeSessionID` (DELETE do
   mapeamento da conversa — a próxima mensagem cria sessão nova).
3. **Fallback manual documentado** (forçar antes do limiar):
   ```
   sqlite3 /root/hokma/backend/memory.db "DELETE FROM session_mode WHERE conv_id='<ID>';"
   ```
4. **Testes**: `opencode_serve_zombie_test.go` — `TestZombieCounter`
   (1→2→3, zera em resposta válida) e `TestZombieIsolation` (por sessão).

## Nota operacional

`smoke_test.go` (resíduo com sintaxe inválida, package "backend", criado
27/08 20:24) QUEBRAVA o `go test` — preservado como
`smoke_test.go.bak_20260828_0345_residuo` (sem remoção).

## Validação

- **Isolado (8090 + serve 4111)**: testes unitários PASS; mecanismo de
  recriação (clear → nova session_id → resposta "RECRIADA-OK"); fluxo real
  pós-TTL é NÃO-DETERMINÍSTICO (o modelo às vezes responde vazio, às vezes
  normal — a detecção dispara com 2 vazias consecutivas).
- **Produção (deploy aprovado, smoke 3/3)**:
  | Teste | Resultado |
  |---|---|
  | Envio async normal | job → done "SMOKE-ZUMBI" |
  | Mecanismo de recriação (DELETE → mensagem) | **nova session_id** + resposta real |
  | Panics / binário | 0 panics; detecção ZUMBI no binário |

## Status

Pendência 1 CONCLUÍDA e em produção. Próxima: pendência 2 (bug OpenTUI —
confirmar decisão de conviver, sem ação) quando chamado por Washington.