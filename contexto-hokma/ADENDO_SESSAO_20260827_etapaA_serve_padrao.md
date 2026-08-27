# Adendo — Sessão 27/08 15:00 · Etapa A: Chat Web responde via opencode serve por padrão

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md, ADENDO_SESSAO_20260827_restart_smoke_opencode_serve.md, ADENDO_BUG_20260827_terminal_active_nunca_limpo.md, adendo 2708 (bundle/tela preta).

---

## Objetivo (aprovado por Washington)

O Chat Web deixa de depender da ponte ttyd por padrão: responde via
`opencode serve` SEMPRE que o serviço estiver up — mesmo com a aba Terminal
aberta no app. O terminal visível (TerminalTTYDScreen + backend ttyd/tmux)
permanece funcional para acesso manual/emergencial. Nada do backend
ttyd/tmux foi removido.

## O que foi feito

- **A1 — ChatScreen.tsx**: `terminalSession: activeTerminalSession()` →
  `terminalSession: undefined` (o request do chat não declara mais terminal
  visível).
- **A2 — opencode_serve_flow.go**: **exceção §2.1 removida** (as duas
  checagens `req.TerminalSession` e `loadTerminalActive()` com has-session).
  O tryOpenCodeServe agora responde quando: gatilho (keyword/forceOpenCode) +
  senha configurada + health OK. Import `os/exec` removido.
- **A3 — smart_chat.go**: `tryTerminalExec` MANTIDO como fallback de
  resiliência (serve down / senha ausente → comportamento legado). Sem
  mudanças.
- **A4 — TerminalTTYDScreen.tsx**: sem mudanças nesta etapa (acesso manual
  intacto; o app continua registrando terminal_active para si).

## Validação isolada (8090 + serve teste 4110, OPENCODE_SERVE_TEST=1)

| Cenário | Resultado |
|---|---|
| terminal_active registrado (hok-terminal-t1 + sessão viva) + mensagem com keyword | ✅ **opencode_serve** respondeu (ANTES cairia na ponte) + session_mode criada |
| serve 4110 derrubado + mensagem com keyword | ✅ fallback legado (`terminal`/tryTerminalExec) — resiliência preservada |
| `go vet` + `tsc --noEmit` | ✅ limpos |

## Deploy em produção (padrão de sempre, backups feitos)

- Backend: backup (`hokma.bak_predep_etapaA_<ts>`, `memory.db.bak_predep_etapaA_<ts>`) → build → substituir → restart → hokma active, 8082 OK.
- Frontend: backup (`/var/www/hok-os.bak_etapaA_<ts>`) → `vite build`
  (PORT=3055, BASE_PATH=/) → copiar → nginx reload → **bundle
  `index-DHpagJ70.js`** (site 200).

### Smoke test em produção — PASS
- Com `terminal_active` = **hok-ttyd REGISTRADO** (terminal aberto/registrado
  no momento): mensagem com keyword → **`opencode_serve`** ("ETAPA-A-OK") +
  session_mode criada (`smoke_etapaA_1 → ses_fbc45038affemVMBw6wdsD9oMZ`).
  Antes da Etapa A teria caído na ponte.
- 0 panics/fatal no journal; hokma + opencode-serve ativos.

## Correção de registro (pedido de Washington)

- Adendo 2708, seção 2 (bundle do fix de tela preta): o registro apontava
  `index-vjXxFCLZ.js` — **desatualizado**. Bundle atual em produção:
  `index-Cy5XPIhw.js` (deploy do fix terminal_active, 27/08 12:38), e agora
  `index-DHpagJ70.js` (Etapa A). O fix de tela preta está incluído em todos;
  0 ocorrências de `iframeSrc` no fonte. Adendo atualizado no Drive.

## Commits locais (PUSH AGUARDANDO APROVAÇÃO)

- Backend `55d6c93`: opencode_serve_flow.go (A2), terminal_routes.go (fix
  terminal_active), adendos (2708 corrigido, bug terminal_active, restart+smoke).
- Frontend `2ff2694`: ChatScreen.tsx (A1), TerminalTTYDScreen.tsx (detach fix).

## Próximas etapas (não iniciadas — aguardando chamado)
- Etapa B (3b): replyPermission + aprovação de tool via SSE.
- Etapa C: bug OpenTUI (decisão atual: conviver; só mexer com aprovação).
- Etapa D: popup/reconnect (mesma regra).