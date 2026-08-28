# Adendo — Sessão 28/08 04:20 · Pendências 2 e 3: OpenTUI (conviver) + popup/reconnect (opção b)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_INCIDENTE_20260827_terminal_popup_reconnect_trava_teclado.md, Adendo Investigacao 20260827 (popup Select model / overlay reconnect), ADENDO_SESSAO_20260828_pendencia1_sessao_zumbi.md.

---

## Pendência 2 — OpenTUI (spin CPU + exit 0 no terminal visível) — CONFIRMADO "CONVIVER"

Washington confirmou: o bug upstream do opencode (issues #2115, #38932, #33399)
permanece com a decisão de CONVIVER — **nenhuma ação nesta rodada**. O Chat Web
não depende mais dessa ponte desde a Fase 3; o terminal visível é uso
manual/emergencial. Nada feito (registro apenas).

## Pendência 3 — Popup/reconnect do terminal visível — OPÇÃO (b) IMPLEMENTADA

Causa raiz (já diagnosticada): overlay "Press ↵ to Reconnect" interno ao iframe
cross-origin do ttyd (inalcançável pelo React); recovery do pai só dispara em
online / visibilitychange >10s / botão manual; troca de aba rápida (<10s) não
dispara. Opção (b) escolhida: **poll do estado da sessão pelo backend**.

### Backend — `GET /terminal/status?session=<nome>` (terminal_routes.go)
- Leve: `tmux has-session` + `display-message -p '#{pane_pid}'` (pane respondendo)
- Retorna `{session, status: "up"|"down", pane_alive}` — token efêmero obrigatório (401 sem)
- Rota registrada no init das rotas ttyd

### Frontend — `TerminalTTYDScreen.tsx`
- **Poll a cada 12s** enquanto a aba Terminal está montada
- `status === "down"` → **`startRecovery()`** (mesmo fluxo existente: probeHealth
  + remontagem do iframe) — sem depender de visibilitychange
- tsc 0 erros

### Validação
- **Isolado (8090)**: sessão viva → up; sessão morta → down; sem token → 401.
- **Produção (deploy aprovado, smoke 5/5)**:
  | Teste | Resultado |
  |---|---|
  | Sessão real (hok-ttyd) | `status: "up"` |
  | Sessão inexistente | `status: "down"` |
  | Sem token | 401 |
  | Bundle novo (`index-6YJx81lb.js`) com o poll | presente |
  | Panics / serviços | 0 panics; hokma + opencode-serve ativos |
- Backups: `hokma.bak_predep_status_*`, `.env`, `memory.db`, `/var/www/hok-os.bak_predep_status_*`.

### Limitação honesta
O poll detecta **pane/sessão tmux morta** (ex.: tmux server caiu — ocorrido em
27/08; sessão encerrada). O caso exato do incidente (WS do ttyd caiu COM a
sessão viva — ttyd mostra "Press ↵ to Reconnect") NÃO é detectável pelo backend
(conexão WS direta ttyd↔navegador, cross-origin). Benefício real: qualquer
queda que leve à morte da sessão/pane (desfecho comum do bug) é detectada e o
recovery dispara automaticamente — inclusive com o app em foreground, que hoje
é cego.

## Status
Pendências 1-3 do roadmap: 1 concluída (adendo anterior), 2 confirmada
(conviver), 3 concluída (opção b em produção). Pendência 4 (drive_creds.env —
renovação do refresh token via OAuth) aguarda o passo a passo + autorização de
Washington (nada feito).