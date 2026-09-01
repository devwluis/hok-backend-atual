# ADENDO — SESSÃO 01/09/2026 — Badge de fallback de IA em produção + diagnóstico do terminal ttyd (histórico duplicado / scroll travado)

Consolidado da sessão de 01/09 (continuação): Correção B (transparência do fallback de IA) aplicada
e em produção; investigação completa do painel de terminal ttyd (TerminalTTYDScreen) com os 2 bugs
diagnosticados (nada aplicado ainda — aguardando decisão).

---

## 1. Correção B — Badge âmbar de fallback de IA: aplicada e em produção ✅

(Precedente: Correção A — ModelB trocado para minimax/minimax-m3:free — já registrada no adendo
`ADENDO_SESSAO_20260901_modelb_free_fallback_transparencia.md`.)

### Implementado (Opção 1 aprovada — bloco discreto)
`ChatScreen.tsx`:
- Tipo `Msg.meta` ganhou `actualModel?: string`.
- `applyResult` guarda o `model_used` REAL do backend em `actualModelRef` + `meta.actualModel`.
- Renderização: badge âmbar de 2 linhas acima do rodapé quando `actualModel !== meta.model`:
  ```
  ⚠️ respondido por: [modelo real]
  (o modelo selecionado não respondeu agora)
  ```
- `final` preserva `actualModel`.

### Build/deploy/commit
- Build: `index-Dc176elv.js` (559.34 kB) — contém o badge (`respondido por`×1, `actualModel`×2).
- Deploy: rsync para `/var/www/hok-os/` + diag restaurados; bundle servido hash-idêntico.
- Commit: `ca93513` — `fix(frontend): badge âmbar avisa quando fallback de IA respondeu com modelo
  diferente do selecionado` (+27/-4). **Sem push.**

---

## 2. Diagnóstico do terminal ttyd (TerminalTTYDScreen.tsx) — NADA aplicado ainda

### BUG 1 — Histórico duplicado (causa raiz confirmada)
- Frontend não duplica: botão Histórico chama `openHistory()` 1x (sem StrictMode), modal renderiza
  `histText` 1x. Backend lê arquivo cru (não duplica).
- **Causa**: `/var/log/hok-term/hok-ttyd.log` tem **1515/2558 snapshots duplicados** — **4 helpers
  `tmux-capture.sh hok-ttyd`** (PIDs 452301, 454633, 463530, 478430) gravando `>>` no **mesmo
  arquivo**. `handleTerminalTTYDLogStart` (terminal_routes.go:1448) só mata o PID do pid file, **não
  mata órfãos** → race.
- **Correção mínima proposta**: matar TODOS os `tmux-capture.sh <sess>` antes de iniciar
  (`pgrep -f` + kill). Baixo risco.

### BUG 2 — Scroll travado (causa raiz confirmada: limitação TUI + inconsistência mouse)
- Estado real: `opencode | alternate_on=1 | history=0 | mouse_any=1 | tmux mouse=OFF | history-limit 2000`.
- **`history=0`**: em TUI (opencode/alternate screen) o tmux não acumula scrollback — documentado em
  terminal_routes.go:618 ("quem rola lá é o próprio app"). Copy-mode não tem o que buscar.
- **`tmux mouse OFF`**: inconsistente com o comentário do código (:968 assume mouse on para a roda
  física no desktop) → a roda física não é forwardada ao app.
- Gesto mobile vai por wheel SGR (tuiWheelMode, mouse_any=1), não por copy-mode.
- **Correções conservadoras**: (a) `tmux set -g mouse on` para alinhar desktop (risco médio); (b)
  **documentar como esperado (TUI)** — recomendada. Não recomendo reescrever copy-mode (alto risco).

### MELHORIA — Botão "Copiar tudo" no Histórico: **já existe** (nada a fazer)
- `term-history-copy` (:1804-1809), `copyHistory` (:665-672) com `writeClipboard` (:164,
  clipboard + fallback execCommand) + `flashToast`. Já no bundle servido.

---

## 3. Commits pendentes (sem push)
- `6705ee3` — temp(frontend): esconde modelos pagos do picker (SHOW_PAID_MODELS)
- `c309948` — fix(frontend): recupera resposta ao voltar de aba oculta
- `ca93513` — fix(frontend): badge âmbar de fallback de IA

## Arquivos/estado
- Deploy: `/var/www/hok-os` → `index-Dc176elv.js` (badge âmbar ativo).
- Backend: `globals.go` ModelB = minimax/minimax-m3:free (Correção A, código pronto, **sem deploy**).
- `TerminalTTYDScreen.tsx`: investigado, nada alterado.
- Backups do frontend: `ChatScreen.tsx.bak_20260901_111443_model_badge`; `/var/www/hok-os.bak_*_pre_model_badge`.

## Pendências
- **BUG 1 terminal**: aplicar correção (matar helpers órfãos) — aguardando aprovação.
- **BUG 2 terminal**: decidir `tmux mouse on` vs documentar como TUI.
- **Deploy Correção A** (ModelB free): build hokma_test pronto, aguardando restart+deploy aprovado.
- **Lacuna de rastreio**: adicionar `id`/`usage` da generation OpenRouter (diagnóstico futuro).
- Cascatas próprias com `google/gemini-2.5-flash` (agent_loop.go, autopatch_loop.go) — limpeza futura.
- Push pendente (3 commits frontend + Correção A backend).