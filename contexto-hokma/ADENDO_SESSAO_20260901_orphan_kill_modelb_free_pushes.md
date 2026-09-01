# ADENDO — SESSÃO 01/09/2026 — BUG 1 terminal (órfãos) corrigido + ModelB free deploy + pushes

Execução dos itens pendentes do adendo
`ADENDO_SESSAO_20260901_badge_fallback_terminal_ttyd_diagnostico.md`: correção do histórico
duplicado do terminal (BUG 1), documentação do BUG 2 (TUI), deploy da Correção A (ModelB free) e
push de todos os commits pendentes. **Tudo com evidência real.**

---

## ITEM 1 — BUG 1 (histórico duplicado): CORRIGIDO e validado ✅

### Causa (já diagnosticada)
4 helpers `tmux-capture.sh hok-ttyd` órfãos gravando `>>` no mesmo arquivo
(`/var/log/hok-term/hok-ttyd.log`) → 1515/2558 snapshots duplicados. O `handleTerminalTTYDLogStart`
só matava o PID do pid file.

### Correção aplicada (`terminal_routes.go`)
- Novo `killHelperOrphans(sess)`: `pgrep -f "tmux-capture.sh <sess>"` + `SIGTERM` em **todos** os
  processos (ignora `os.Getpid()`), com `sleep 200ms` antes do novo spawn.
- Substituiu o bloco de "already running" (que não matava órfãos).

### Confirmado com evidência real
- **1 helper vivo** por sessão (antes: 4-5). `ps aux | grep tmux-capture.sh` → só `pid 488694`.
- **Snapshots novos com timestamps distintos** (12:18:22→12:18:32, sem duplicação); delta 4/7s
  consistente com interval 2s de UM helper.
- **Últimos 20 snapshots: 0 duplicados** (antes 1515 duplicados no histórico).

---

## ITEM 2 — BUG 2 (scroll travado): documentado como esperado de TUI ✅ (sem mudança de código)

- **`tmux set -g mouse on` NÃO foi aplicado** — apenas proposto (mostrado o comando + riscos).
- Documentado no código (`handleTerminalTTYDScroll`): em TUI (opencode/claude fullscreen) o tmux
  reporta `history=0` por design (alternate screen) → scroll é interno do app; o botão "Histórico"
  é o workaround oficial.

### Confirmado com evidência real
- `tmux display -t hok-ttyd` → `opencode | alternate_on=1 | history=0 | mouse_any=1 | mouse off`.

---

## ITEM 3 — Deploy da Correção A (ModelB free): VALIDADO ✅

- `globals.go`: `ModelB = "minimax/minimax-m3:free"` (era `google/gemini-2.5-flash` PAGO).
- Backup binário: `hokma.bak_20260901_121438_pre_orphan_modelb`.
- Restart único (com o fix do BUG 1 no mesmo binário). Health 200.

### Confirmado com evidência real
- `GET /models/catalog` → `modelB: minimax/minimax-m3:free` (antes gemini pago).
- Log do pool: `HOK/Fallback-minimax/minimax-m3:free` — **0 ocorrências** de
  `google/gemini-2.5-flash respondeu` pós-restart.
- Teste real (modelo `z-ai/glm-5.2:free` falhando 429): pool tentou ModelB novo (429 também) →
  seguiu para Gemini/Flash-Lite (free). Sem custo de ModelB pago.

---

## ITEM 4 — Push dos commits pendentes: CONCLUÍDO ✅

### Backend → `origin/hok-backend-atual` (`33629b2..f83d12f`)
- `f83d12f` — `fix(catalog+terminal): ModelB free (minimax-m3) + mata helpers órfãos do histórico duplicado`
  (globals.go + terminal_routes.go + opencode_client_test.go só com a linha do ModelB — o plangate
  de outra tarefa foi ISOLADO e restaurado no working tree).

### Frontend → `origin/main` (`63a825b..ca93513`)
- `6705ee3` — temp(frontend): esconde modelos pagos do picker
- `c309948` — fix(frontend): recupera resposta ao voltar de aba oculta
- `ca93513` — fix(frontend): badge âmbar de fallback de IA

### Confirmado com evidência real
- `git log origin/hok-backend-atual -1` → `f83d12f`; 0 commits à frente.
- `git log origin/main -1` → `ca93513`; 0 commits à frente.
- Permissão de escrita confirmada nos 2 repos (blob probe OK) antes do push.

### Segurança
- Credencial `~/.git-credentials` removida após o push (perm 600 temporária).
- Token NÃO rastreado em nenhum arquivo (grep em contexto-hokma/ e src/ → vazio).
- Usuário orientado a rodar `clear` no ttyd (token visível no scrollback).

---

## NÃO implementados (aguardando decisão)
- **BUG 2**: `tmux set -g mouse on` (proposto, não aplicado — risco de mudar comportamento global).
- **Lacuna de rastreio**: adicionar `id`/`usage` da generation OpenRouter na resposta
  (`APIResponse`/`ChatResponse` não capturam) — para isolar custo por chamada no futuro.
- Cascatas próprias com `google/gemini-2.5-flash` (agent_loop.go, autopatch_loop.go) — limpeza futura.

## Arquivos/estado
- Backups: `terminal_routes.go.bak_20260901_121340_orphan_kill`, `globals.go.bak_*_modelb_free`,
  `opencode_client_test.go.bak_*_modelb_free`, `hokma.bak_20260901_121438_pre_orphan_modelb`.
- Working tree backend: `opencode_client.go`, `smart_chat.go`, `autonomous_allowlist.go`,
  `opencode_client_test.go` (plangate) seguem modificados (outra tarefa, não commitados).
- Working tree frontend: `TerminalTTYDScreen.tsx` modificado (outra tarefa).