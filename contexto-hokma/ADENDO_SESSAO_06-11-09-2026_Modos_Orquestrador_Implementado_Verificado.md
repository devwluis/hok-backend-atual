# Adendo — Sessão 11/09/2026 · Modos do Orquestrador — IMPLEMENTADO E VERIFICADO

**Origem:** opencode (terminal) | **Data/hora:** 11-09-2026 06:24 (-03) | **Subagente:** Especialista N8N

> **Atualiza/substitui o status** do plano de 10 itens documentado em
> `ADENDO_SESSAO_20260912_Cache_Audit_Terminal_Modos_Orquestrador.md` (§3).
> Aquele adendo (mtime 09-10 07:08) é **anterior** aos commits de implementação
> do orquestrador (09-10, após 11:05). Ou seja: o plano **já foi implementado**;
> este adendo confirma qual é o estado real e acrescenta **verificação funcional**.

---

## 1. Estado real dos 10 itens — TODOS IMPLEMENTADOS

| # | Item | Status | Evidência (código atual) |
|---|---|---|---|
| 1 | `Mode` em `OrchestratorRequest` | ✅ | `agent_orchestrator.go:61` |
| 2 | Passar `req.Mode` em `tryOrchestrator` | ✅ | `smart_chat.go:559` (`Mode: req.Mode`) |
| 3 | `describeRunEngineAction()` | ✅ | `agent_orchestrator.go:1100` |
| 4 | Gate de modo no dispatch (run_engine + mutantes) | ✅ | `agent_orchestrator.go:680-721` (run_engine) e `723-762` (mutantes via `isMutantTool`/`describeMutantAction` — `pending_action.go:97,245`) |
| 5 | Budget check (autonomous_total) | ✅ | `agent_orchestrator.go:494-498` + `autonomousAllow` (`autonomous.go:250-267`) |
| 6 | `runSubagent` com mode + gate | ✅ | `agent_orchestrator.go:782` (assinatura) + gate `800-827` |
| 7 | Case `run_engine` no resolver de pending_action | ✅ | `pending_action.go:438-439` + `resolveRunEnginePendingAction:489` |
| 8 | Migração DB `autonomous→build` | ✅ | `db.go:309-312` (+ migração do CHECK/colunas) |
| 9 | Remover botão "Autônomo" (ModeSelector) | ✅ | `ModeSelector.tsx:10-15` — só `plan｜build｜autonomous_total` |
| 10 | `isAutonomousLike` em autonomous.go | ✅ | `autonomous.go:122` |

**Commits que implementaram** (09-10, posteriores ao adendo do plano):
`7f2d6a0` (detect pending_action do subagente + autonomous userID),
`60ebb1c` (sequential mutations in build mode — task expansion, tool restriction, state persistence),
`dadab5c` (deepseek-native via DS_URL), `8c297c1` (ApprovalResult no resume + schema plan).

---

## 2. Correção de diagnóstico do adendo anterior

O adendo anterior afirmava que `agentTools()` "só tem tools de leitura (sem edit/write)".
**Incorreto**: `agentTools()` (`agent_loop_groq.go:60`) inclui tools de **escrita/mutação**
(`n8n_create/update/activate/delete/execute/test_workflow`, `add_imovel`) e `bash_exec`.
Por isso o **gate de modo (item 4)** é essencial — e é exatamente o que foi implementado.

Complementarmente, nesta mesma sessão foi deployado o **default seguro** em
`agentAllowedTools()` (commit `46ad889`): subagentes sem `tools` explícito no DB
passam a receber apenas o catálogo read-only (`safeDefaultTools`), em vez do catálogo
completo. **Sem conflito** com os 10 itens: as tools do *orquestrador* vêm de
`agentTools()` (não de `agentAllowedTools`), e o gate do item 4 opera no dispatch.

---

## 3. Verificação funcional real (via `/agents/orchestrate`, Especialista N8N `ag_718147165`)

Tarefa usada nos 3 testes (mesma ação mutante, exige `run_engine`):
> "rode `echo hok-verificacao-modos` no terminal do backend; use a tool run_engine (engine opencode)"

Modelo: `nvidia/nemotron-3-super-120b-a12b:free`.

### Teste 1 — `mode=plan` (conv=`modetest_plan`)
- **Request:** `{"task":"...run_engine...","agent_id":"ag_718147165","mode":"plan"}`
- **Resultado:** `status ok`, `steps 1`, subagent Especialista N8N ~23.9s, sem erro.
- **Reply (trecho):** "…Todas as tentativas de usar a tool `run_engine` … retornaram:
  `Modo planejar: execução via engines não permitida.` …"
- **Comportamento:** ✅ **BLOQUEOU** — nenhum side-effect executado.

### Teste 2 — `mode=build` (conv=`modetest_build`)
- **Request:** `{"task":"...run_engine...","agent_id":"ag_718147165","mode":"build"}`
- **Resultado:** `status ok`, `steps 1`, tracing `kind=pending_action`.
- **Reply:** `Executar tarefa no engine opencode: echo hok-verificacao-modos` + `Confirma? (responda sim/nao)`
- **pending_action REAL criado** (tabela `pending_actions`):
  - key `owner:anonymous:modetest_build` | tool `run_engine` | ts `2026-09-11T06:23:15`
- **Comportamento:** ✅ **NÃO executou direto** — aguarda confirmação. **Deixado pendente** (não aprovado), conforme instrução.

### Teste 3 — `mode=autonomous_total` (conv=`modetest_auto`, budget=3)
- **Setup:** `POST /session/mode {"mode":"autonomous_total","autonomous_budget":3}`
- **Request:** `{"task":"...run_engine...","agent_id":"ag_718147165","mode":"autonomous_total"}`
- **Resultado:** `status ok`, `steps 1`, subagent ~59.1s, sem erro.
- **Reply:** "O comando foi executado com sucesso no terminal do backend via engine **opencode**:
  `hok-verificacao-modos`"
- **Budget:** **3 → 2** (decrementado).
- **Auditoria** (`autonomous_audit` id=29): `agent=orchestrator_subagent`, `status=ok`, `budget_left=2`,
  `action={"engine":"opencode","task":"Run the command: echo hok-verificacao-modos"}`
- **Comportamento:** ✅ passou pelo `autonomousAllow` (budget/circuit breaker) **antes** de executar; executou direto e consumiu 1 do budget.

---

## 4. Conclusão

Os 3 modos do Orquestrador funcionam **como documentado**:
- **Planejar** → `run_engine` e mutantes **bloqueados** (sem execução).
- **Construir** → `run_engine` e mutantes passam por **pending_action** (confirmação sim/não) + estado salvo para retomada.
- **Autônomo Total** → passa por **`autonomousAllow`** (budget + circuit breaker) e executa direto; budget decrementado e auditado; auto-rollback disponível.

**Plano dos modos: implementado e verificado.** Nenhuma implementação nova foi necessária
nesta etapa — apenas verificação e atualização de status.

---

## 5. Observações / pendências

- O `pending_action` do teste 2 (`owner:anonymous:modetest_build`) segue **pendente de propósito**
  (evidência de que o gate cria a ação sem executar). Pode ser rejeitado/limpo quando quiser.
- Continua pendente o commit/push de eventuais mudanças novas (nenhuma nova além do `46ad889`,
  já publicado em `main` — `f3dc91a..46ad889`, sincronizado).
