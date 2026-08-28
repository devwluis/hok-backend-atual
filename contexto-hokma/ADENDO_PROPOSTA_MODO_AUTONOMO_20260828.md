# Adendo — Proposta do MODO AUTÔNOMO (investigação + desenho, sem implementação)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_MODO_COMPARTILHADO_PLAN_BUILD_AUTONOMO_24-08-2026.md (proposta original), commit 92dd160 (gates de plan).

---

## 1. Levantamento do estado atual (grep confirmado — não suposição)

- **"autonomous" no código**: aparece APENAS em `db.go:213` (CHECK do schema)
  e no comment de `opencode_serve_persist.go:7`. **NENHUM tryX trata o modo
  autonomous de forma diferente** — é 100% decorativo hoje.
- **session_mode NÃO é lido no fluxo**: o `mode` vem do **request**
  (`req.Mode`, enviado pelo frontend) — a leitura da tabela no
  `runSmartText`/`classifyEngine` (prevista no adendo 24/08) **não foi
  implementada**. A tabela é só escrita (getOrCreate/consulta de teste).
- **`req.Mode` lido em**: `RunAgentLoop` (n8n agent — o mode é passado), e os
  gates de plan em `tryClaudeCode`/`tryOpenCode`/`tryHermes` (smart_chat.go:
  471/505/540/555). `req.Mode == "plan"` é o único modo checado.
- **Fluxo de build (aprovação) atual**: `promptNeedsApproval` (destrutivos/
  escrita) → `setPendingAction` (claude_code/opencode) → `/actions/approve`
  executa com `--dangerously-skip-permissions`/`--auto`; read-only executa
  direto. O serve usa o watcher (once/card/reject) desde a Etapa B.
- **Flags de auto dos agentes**: claude `--permission-mode <auto|dontAsk|
  bypassPermissions|...>` (existe); opencode `--auto`; hermes `--yolo`
  (auto-aprova; em plan foi removido).

## 2. Definição do que "autônomo" significa (proposta)

**Autônomo = roda sem aprovação humana POR AÇÃO, dentro de limites; nunca
executa o que está na blocklist; para sozinho se algo der errado; registra
cada ação.**

### 2a. Budget (`autonomous_budget`)
- **Semântica**: nº de **unidades de ação** permitidas antes de exigir nova
  aprovação. Proposta: **1 unidade = 1 chamada ao agente** (cada turno de
  execução), contado no Hokma (simples e auditável). Refinamento futuro:
  contar tools dentro do stream (claude stream-json / opencode NDJSON).
- **Fluxo**: a cada chamada autônoma aprovada, decrementa; ao zerar → o modo
  volta a exigir aprovação (build) até o dono "recarregar" o budget (trocar de
  modo ou POST /session/mode com budget novo). Zerado ao trocar de modo.
- Valor default proposto: **5** (ajustável no POST /session/mode).

### 2b. Blocklist obrigatória (NUNCA executa, mesmo em autônomo — os 4 agentes)
Validada pelo **Hokma ANTES de chamar qualquer agente** (gate de prompt):
- `terminalExecBlocklist` (rm -rf /, mkfs, dd if=, shutdown, halt, chmod -r 777 /...)
- Sinais destrutivos (`destructiveSignals` + `writeSignals` do smart_chat):
  delete, sudo, systemctl, kill, git push (branch de produção), deploy
- Credenciais/tokens (leitura/escrita de keys, .env, auth.json)
- Arquivos congelados: `auth_routes.go`, `crm_*.go` (edição)
- `git push` para branches de produção; `git commit` permitido (local)
O que cai na blocklist em autônomo → **não é enviado** ao agente: retorna
aviso (mesmo padrão do `opencode_serve_blocked`) OU cai no pending se o
usuário quiser aprovar manualmente (proposta: aviso + opção de aprovação).

### 2c. Circuit breaker (interromper loop/comportamento anômalo)
- **Ações repetidas idênticas**: 3x o mesmo comando/prompt em sequência →
  para e avisa.
- **Erros consecutivos**: 3 falhas de execução seguidas → para.
- **Tempo**: o job já tem timeout (10 min async); o budget de tempo é o
  teto (proposta: o mesmo 10 min; configurável).
- **Manual**: mensagem "pare" no chat interrompe (o fluxo de aprovação já
  trata; no autônomo, uma flag de "interromper" por conv).

### 2d. Auditoria (cada ação, não só o resultado)
- **Nova tabela `autonomous_audit`** (consultável por conv):
  ```
  CREATE TABLE autonomous_audit (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    conv_id TEXT, tenant_id TEXT, user_id TEXT,
    agent TEXT,          -- claude_code | opencode | opencode_serve | hermes
    action TEXT,         -- prompt/comando resumido (primeiros ~200 chars)
    action_hash TEXT,    -- hash do prompt (para detectar repetição)
    budget_left INTEGER,
    status TEXT,         -- ok | error | blocked
    ts INTEGER DEFAULT (unixepoch())
  );
  ```
- Escrita a cada chamada autônoma (antes = tentativa, depois = resultado).
- A tabela `logs` existente continua para o geral; a `autonomous_audit` é a
  trilha específica do modo.

## 3. Implementação por agente (mesmo padrão hermético do plan)

| Agente | Gate autônomo proposto | Notas |
|---|---|---|
| **Claude Code** | `--permission-mode auto` (ou `dontAsk`) **+ blocklist Hokma validando o prompt ANTES** + budget/auditoria | Flag nativa existe (`auto`). Nenhum wrapper. O que a blocklist barra → não chega ao CLI. Testar qual choice auto-aprova sem pedir (auto vs dontAsk vs bypassPermissions) |
| **OpenCode CLI** | `--auto` + blocklist Hokma antes | Flag nativa (`--auto`). O agent "plan" (deny) já existe para o oposto — o autônomo usa o agent build + `--auto` com prompt filtrado |
| **OpenCode serve** | Agent build + **watcher responde `once`** para TODAS as permissions não-bloqueadas (camada do modo autônomo; a blocklist Hokma decide antes; o watcher mantém reject para o que escapar) | Mesmo princípio da camada 2 do plan, invertido: em autônomo → once (não card) |
| **Hermes** | Fluxo normal (`docker exec --yolo`) **+ blocklist Hokma antes** + budget/auditoria | Sem flag nativa. O autônomo DEVE executar — o isolamento do plan (read-only) não se aplica; o container efêmero com volume real rw seria o equivalente "hermético" (limita o hermes ao próprio /opt/data, sem docker.sock/host), mas o custo por chamada é maior. **Decisão em aberto**: usar o exec atual (rápido) ou o container efêmero com volume rw (mais isolado) |

## 4. Decisões em aberto para Washington (antes de codar)

1. **Leitura do session_mode**: implementar agora o `POST /session/mode` +
   leitura no runSmartText (os 3 botões do frontend) — o autônomo precisa do
   mode VINDO DA TABELA (não só do request). Confirmar escopo.
2. **Budget**: 1 unidade = 1 chamada ao agente (default 5) — ok? Ou contar
   tools no stream (mais preciso, mais trabalho)?
3. **Hermes**: fluxo atual (exec --yolo) com blocklist Hokma — ou container
   efêmero com volume rw (isolamento, custo ~1-2s/chamada)?
4. **Blocklist**: quando algo é barrado em autônomo — aviso direto OU
   oferecer aprovação manual (pending)?
5. **Circuit breaker**: os limiares propostos (3 repetidas / 3 erros / 10min)
   ok?

**Nada implementado — proposta para revisão.**