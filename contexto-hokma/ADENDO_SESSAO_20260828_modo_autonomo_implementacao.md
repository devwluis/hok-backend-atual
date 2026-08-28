# Adendo — Sessão 28/08 · Implementação do MODO AUTÔNOMO (gates + session_mode + auditoria)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_PROPOSTA_MODO_AUTONOMO_20260828.md (proposta aprovada).

---

## Contexto

Implementação do modo autônomo seguindo as 5 decisões fechadas por Washington
(28/08): (1) POST /session/mode + leitura da tabela no fluxo; (2) budget =
nº de chamadas ao agente (default 5); (3) Hermes autônomo = container
efêmero com volume real rw (sem --rm); (4) blocklist em autônomo = aviso
direto, sem pendência automática; (5) circuit breaker 3 repetidas / 3 erros
/ 10 min.

## Implementação

- **session_mode.go** (novo): `POST /session/mode` (upsert com CHECK +
  budget, set_by='web') e `GET /session/mode`; `sessionModeLoad/Set`;
  `normalizeAutonomousBudget` (default 5 em autonomous, 0 nos demais modos).
  Rota registrada em main.go. Trocar o modo reseta o circuit breaker.
- **autonomous.go** (novo): auditoria `autonomous_audit` (cada chamada:
  agente, ação truncada a 200, hash sha256, budget_left, status), budget
  (decremento por chamada), circuit breaker in-memory por conversa
  (3 ações idênticas / 3 erros consecutivos / 10 min), `autonomousAllow`
  (valida e decrementa; blocklist é do chamador).
- **smart_chat.go**: handleSmartChat lê o session_mode como fonte do modo
  quando o request não traz mode (compat com o frontend atual); gates
  autônomos em tryClaudeCode (claude_code_autonomous*), tryOpenCode
  (opencode_autonomous*) e tryHermes (hermes_autonomous* — assinatura
  ganhou conv/tenant/user) — todos: blocklist → aviso direto; budget/cb →
  blocked; execução → ok com budget restante no reply; erro → error.
- **claude_code_client.go**: `--permission-mode auto` (modo nativo) no
  autônomo, sob claudeAgentUser (defesa em profundidade); sudo segue
  proibido; nunca --dangerously-skip-permissions.
- **opencode_client.go**: `callOpenCodeAutonomous` (--auto, fallback
  modelB, sessão preservada).
- **hermes_client.go**: `callHermesWithMode(model, prompt, mode)` agora por
  string ("plan"/"autonomous"/normal); `hermesAutonomousArgs` — container
  efêmero com volume real rw e SEM --read-only/--rm (decisão 3), -z (yolo)
  para executar, montagens ro para config + rw para escrita.
- **opencode_serve_flow.go**: modo autônomo por sessão
  (serveAutonomousMode); watcher camada 2: em autônomo, TODA permission
  não-bloqueada → `once` (blocklist do serve segue `reject`); a blocklist
  Hokma do prompt barra antes.
- **db.go**: CREATE TABLE autonomous_audit.
- **Testes** (autonomous_test.go, 8): args claude auto, args hermes
  autônomo (sem --rm/--read-only, mount rw, -z), circuit breaker (repetição,
  erros, tempo), normalizeAutonomousBudget, blocklist de prompts, modo
  serve. Assinaturas atualizadas em plan_gate_test.go, claude_bare_fix_test.go,
  leak_debug_test.go.

## Validação

- **Go**: build + vet limpos; suíte completa PASS (33s).
- **E2E isolado (porta 8099)**: 
  - POST /session/mode {mode:autonomous, budget:3} → ok; GET lê o estado.
  - Hermes autônomo com `rm -rf /` → `hermes_autonomous_blocked` (aviso
    direto, SEM pendência — decisão 4).
  - Hermes autônomo executando (listar /opt/data) → respondeu com o
    conteúdo real do volume; container efêmero ficou para inspeção
    (Exited 0, sem --rm — decisão 3).
  - Budget: 3 → 2 → 1 (decremento por chamada).
  - Circuit breaker: 3ª ação idêntica → `hermes_autonomous_blocked`
    ("mesma ação repetida 3x").
  - Budget esgotado → `hermes_autonomous_blocked` ("budget esgotado
    (0/5) — recarregue via POST /session/mode").
  - Auditoria (autonomous_audit): blocked → ok(2) → blocked_cb → ok →
    blocked_budget, com budget_left e hash por linha. 
- **Banco de produção**: linhas de teste removidas (conv_auto_test).

## Pendências

- **NÃO deployado/restart/push** — aguardando aprovação de Washington para
  o deploy em produção (padrão das sessões: backup → substituir → restart).
- Smoke real em produção (após deploy): session/mode + hermes autônomo
  executando + blocklist + budget + auditoria.
- Frontend: os 3 botões do ChatScreen chamando POST /session/mode (próxima
  etapa).