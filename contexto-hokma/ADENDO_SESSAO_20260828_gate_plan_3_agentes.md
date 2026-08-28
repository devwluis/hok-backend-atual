# Adendo — Sessão 28/08 14:00 · GATE REAL DE MODO PLAN para os 3 agentes (Claude Code, OpenCode, Hermes)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_20260828_modo_sessao_compartilhado_analise.md (achado: plan decorativo nos 3 agentes), proposta do Modo de Sessão Compartilhado.

---

## Objetivo

Corrigir o achado crítico: o modo "plan" era decorativo nos 3 agentes (executavam
mesmo assim). Implementado gate REAL de plan em cada um + verificação
pós-execução no Hermes + CHECK constraint na session_mode + testes
automatizados anti-regressão. **Nenhum deploy/restart/push** — aguardando
revisão de Washington.

## Alterações (arquivos + diff resumido)

| Arquivo | Mudança |
|---|---|
| `db.go` | `CREATE TABLE session_mode` agora com `CHECK (mode IN ('plan','build','autonomous'))` (bancos novos) |
| **migration SQL** (memory.db, via sqlite) | Tabela recriada com CHECK preservando 18 linhas; modo inválido rejeitado (testado) |
| `opencode.json` (projeto) | Novo **agent `plan`**: `permission` = bash/edit/external_directory/webfetch **deny** (nada executa) |
| `claude_code_client.go` | `claudeCLIArgs(+planMode)` → em plan: **`--permission-mode plan`** (modo NATIVO do claude — descreve, não executa; nunca combina com skip-permissions). Novos `callClaudeCodePlan`, `runClaudeCodeCLI(+planMode)`, `runClaudeCodeWithModel(+planMode)` |
| `opencode_client.go` | `opencodeCLIArgs(+planMode)` → em plan: **`--agent plan`** (permissões deny) e nunca `--auto`. Novos `callOpenCodePlan`, `runOpenCodeCLI(+planMode)` |
| `smart_chat.go` | `tryClaudeCode` → `callClaudeCodePlan`; `tryOpenCode` → `callOpenCodePlan`; `tryHermes` → em plan: `callHermesWithMode(planMode=true)` (sem `--yolo`), mode `hermes_plan` + verificador pós-execução |
| `opencode_serve_flow.go` | `tryOpenCodeServe` em plan: `opts.Agent="plan"` (camada 1) + marca a sessão em plan; **watcher: em modo plan TODA permission.asked → reject** (camada 2) |
| `hermes_client.go` | `callHermesArgs`/`callHermesWithMode` (sem `--yolo` em plan); **`hermesVerifyOutput`** — verificação pós-execução: alegação de criação com caminho inexistente no disco → aviso anexado (pega alucinação) |
| `plan_gate_test.go` (novo) | 5 testes: ClaudeArgs (plan nativo, sem skip), OpenCodeArgs (--agent plan, sem --auto), HermesArgs (sem --yolo), ServeDecision (plan mode set/clear), HermesVerifyOutput (alucinação detectada) |
| `claude_bare_fix_test.go` / `leak_debug_test.go` | assinaturas de `claudeCLIArgs` atualizadas |

## Resultado dos testes automatizados

`go test -run "TestPlanGate|TestHermesVerify"` → **5/5 PASS** (e toda a suíte
existente ok: 27.7s). Build `go build -o hokma_test .` e `go vet` limpos.

## Resultado dos testes E2E controlados (isolado, porta 8099 — NUNCA produção)

| Agente | Teste | Resultado | Plan respeitado? |
|---|---|---|---|
| **Claude Code** | `forceClaudeCode + mode:plan` + "crie o arquivo" | reply "restrições de segurança no modo de planejamento... não permite criar" — **nenhum arquivo criado** | ✅ **SIM** (--permission-mode plan) |
| **OpenCode serve** | `forceOpenCode + mode:plan` | modelo disse "vou criar" mas **nada foi criado** (agent plan deny + watcher reject — AUDIT `reject (modo plan)`) | ✅ **SIM** |
| **OpenCode CLI** | `--agent plan` direto | **parou e perguntou** (sem ferramentas) — nada criado | ✅ **SIM** |
| **Hermes** | `forceHermes + mode:plan` | mode `hermes_plan`, sem `--yolo`; **parcial**: o hermes tentou escrever em /tmp (protegido pelo sistema DELE) mas criou 1 arquivo em `/opt/data` (home do container — fora do host) numa rodada; na 2ª rodada nada foi escrito (verifier do hermes confirmou). Verificador pós-execução anexa aviso para caminhos do host inexistentes | ⚠️ **PARCIAL** (executa no ambiente dele; o gate não impede — mitigado pelo verificador + modo + isolação do container) |

## Achados/observações

1. **Claude e OpenCode: gates NATIVOS e eficazes** (o claude tem `--permission-mode
   plan`; o opencode aceita agent com permissões deny).
2. **Hermes: o mais fraco** — o hermes executa no próprio container (fora do
   host); sem `--yolo` ele ainda executa algumas ferramentas. O verificador
   pós-execução pega alegações de arquivos do HOST; as do container não são
   verificáveis pelo host. Para um plan 100% no hermes, seria preciso um modo
   nativo do hermes (não existe flag — documentado).
3. O modo `plan` continua com o texto de aviso no reply (compat), mas agora o
   GATE é de verdade no código (antes da chamada de execução).

## Pendências

- Deploy/restart/push: **aguardando aprovação de Washington** (backups:
  `*.bak_<ts>_plangate` + `memory.db.bak_pre_plangate_*`).
- O `session_mode` (chave tenant:user:conv + CHECK) está pronto para o Modo de
  Sessão Compartilhado (implementação do POST /session/mode + leitura no
  runSmartText — próxima etapa, separada).