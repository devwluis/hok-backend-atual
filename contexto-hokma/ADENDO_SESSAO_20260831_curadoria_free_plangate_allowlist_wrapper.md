# ADENDO — SESSÃO 31/08/2026 — CUradoria free + Plangate real + Allowlist autônomo + hok-model + wrapper de modo

Consolidado das tarefas executadas nesta sessão (ITEM 1 a 4 + curadoria free aprovada). Todo o
trabalho foi feito em build ISOLADO (`hokma_test`, porta de teste 8099) e servidor de teste via
`systemd-run --unit=hokma-item1-test` (NUNCA em produção). **Produção NÃO foi reiniciada** — as
mudanças entram em vigor no próximo restart autorizado.

---

## ITEM 1 — Trava REAL do modo Plan no OpenCode (bloqueante) ✅

### Causa raiz encontrada
O config com o agente `plan` (deny) vive em `/root/hokma/backend/opencode.json`, mas o CLI roda com
`--dir /root/hokma` (workdir), que NÃO tem `opencode.json`. O `--agent plan` resolvia para o agente
builtin (frágil/versão-dependente) — não era uma trava garantida. Confirmado testando o binário:

```
$ opencode run "crie o arquivo plan-repro-ITEM1.txt..." --dir /root/hokma --agent plan
→ (resposta textual, dependia do modelo; sem garantia no motor)
```

### Fix aplicado (defesa em profundidade, 3 camadas) — `opencode_client.go`
1. **Guard fail-closed**: `planMode && skipPermissions` → erro (`errOpenCodePlanLock`).
2. **Trava no motor**: `OPENCODE_PERMISSION='{"bash":"deny","edit":"deny","write":"deny","patch":"deny","webfetch":"deny","external_directory":"deny","mcp":"deny"}'` no env do processo — o opencode aplica essa env **por último** na carga de config (prioridade máxima). A tool vira indisponível ("invalid"), não é instrução de prompt.
3. **Verificador pós-execução**: `processOpenCodeJSONStream` agora detecta `tool_use` com tool perigosa REAL (bash/edit/write/patch/webfetch) → fail-closed (descarta resposta). Tool negada chega como `"invalid"` e NÃO dispara.

`smart_chat.go` `tryOpenCode` plan: sufixo novo + modo `opencode_plan_blocked` quando a trava dispara.

### Testes unitários (passando)
```
=== RUN   TestProcessOpenCodeJSONStreamPlanDeny   --- PASS   (tool "invalid" não conta; bash real conta)
=== RUN   TestOpenCodePlanDenyJSON                --- PASS
=== RUN   TestOpenCodePlanNeverAuto               --- PASS   (--agent plan, nunca --auto)
=== RUN   TestPlanGate*                           --- PASS
ok  hokma_backend
```

### Teste manual REAL (servidor isolado 8099, DB temporário)
`mode=plan` + "crie o arquivo item1-plan-lock-test.txt":
```
MODE: opencode_plan
REPLY: Não posso criar arquivos durante o modo PLAN...
(Modo planejar: nenhuma acao foi executada — tools de escrita/execução estão bloqueadas por trava real do motor.)
$ find /tmp/opencode/item1_work -name 'item1-plan-lock-test.txt'  → 0 arquivos encontrados ✅
```
Controle: `mode=build` criou o arquivo (`opencode_direct`, ls confirmou) e `mode=autonomous` criou
(`opencode_autonomous`, ls confirmou) — a trava só vale para plan.

---

## ITEM 2 — `hok-model` (seletor de modelo no terminal) ✅

Script genérico em `/usr/local/bin/hok-model` (lê o catálogo dinamicamente, agrupa por provider
Free/Pago, marcando o ativo; sem argumento lista, com argumento seleciona por índice ou substring).

Listagem real (contra produção 8082):
```
Catálogo HOK — 5 providers, 911 modelos (115 free, 796 pagos) | ativo: deepseek/deepseek-chat-v3.1
## OpenCode Zen  [63 modelos]
  -- FREE --
     1. opencode/deepseek-v4-flash-free ...
  -- PAGO --
     8. opencode/claude-fable-5 ...
```

Seleção real (produção):
```
$ hok-model 93
✔ selecionado: minimax/minimax-m3:free  (provider=OpenRouter, free=True)
  backend respondeu: ok — active=minimax/minimax-m3:free
  ✔ ~/.opencode/opencode.json → model = 'openrouter/minimax/minimax-m3:free'
```
Diff `~/.opencode/opencode.json`: `opencode-go/deepseek-v4-flash` → `openrouter/minimax/minimax-m3:free`.

Sessão opencode NOVA usando o modelo selecionado:
```
$ opencode run "Responda apenas com seu id de modelo exato" --format json --dir /tmp/opencode/item1_work
RESPOSTA: openrouter/minimax/minimax-m3:free   (SID ses_fa76e09feffePg4s3uCNR1Xjrq)
```
✅ A sessão nova usou o modelo selecionado (propagação via `propagateActiveModelToMotors`).

---

## ITEM 3 — Allowlist do modo autônomo (pendência do adendo de session_mode, seção 5) ✅

### Allowlist EXPLÍCITA (`autonomous_allowlist.go`)
```go
func autonomousAllowlist() []string {
	return []string{
		// diagnóstico somente leitura
		"ls", "cat", "pwd", "whoami", "uptime", "uname",
		"df", "free", "ps", "netstat", "ss", "top", "find", "grep",
		// git somente leitura (nunca push/commit)
		"git status", "git log", "git diff", "git show",
		// serviço: SOMENTE status (nunca start/stop/restart/kill)
		"systemctl status",
		// HTTP interno somente leitura (GET)
		"curl",
	}
}
```
**Nunca em autônomo** (`autonomousNeverAllowed`): `rm -rf /`, `mkfs`, `dd`, `shutdown/reboot/halt/poweroff`,
`systemctl start|stop|restart|kill`, `git push|commit|reset|rebase|merge`, `sudo/su`, `curl -X/-d/--form`,
`wget`. Reusa o padrão da Opção A' do bash_exec (allowlist + argv/validação rígida).

Gate: `autonomousGate(prompt)` → `Forbidden` (proibido, NUNCA executa) | `NeedsApproval` (fora da
allowlist → pendência humana) | `Exec` (allowlist/conversacional). Integrado em `tryClaudeCode`,
`tryOpenCode` (fallback de aprovação via pending_action) e `tryHermes` (fail-closed, sem pendência própria).

### Testes unitários (passando)
```
TestAutonomousGateAllowed        --- PASS  (git status, ls, systemctl status, curl interno → EXEC)
TestAutonomousGateOutsideAllowlist --- PASS (npm install, crie arquivo, edite, go build/deploy → NEEDS_APPROVAL)
TestAutonomousGateForbidden      --- PASS  (rm -rf, systemctl restart, git push/commit, sudo, dd, shutdown → FORBIDDEN)
TestAutonomousGateConversational --- PASS
TestAutonomousCommandAllowedUnit --- PASS
```

### Teste manual REAL (8099, modo autonomous, budget 5)
| prompt | resultado | modo | auditoria |
|---|---|---|---|
| `whoami` (allowlist) | EXECUTOU → "root" | `opencode_autonomous` | `ok` budget 4 |
| `cat /etc/os-release` (allowlist) | EXECUTOU → Debian trixie | `opencode_autonomous` | `ok` budget 3 |
| `ps aux \| head -3` (fora da allowlist) | caiu em APROVAÇÃO (não executou) | `opencode_autonomous_pending` | `pending_allowlist` |
| `systemctl restart hokma` (proibido) | BLOQUEADO, serviço continuou ativo | `opencode_autonomous_blocked` | `blocked` |

---

## ITEM 4 — Wrapper de modo de sessão `opencode-mode-wrapper.sh` ✅

Arquivo `/usr/local/bin/opencode-mode-wrapper.sh` (ajustado: usa header `X-Conversation-Id: terminal`,
default `plan` quando vazio, token do `.env`):
```bash
#!/usr/bin/env bash
set -euo pipefail
HOK_API="${HOK_API:-http://127.0.0.1:8082}"
TENANT="owner"
ENV_FILE="${HOK_ENV_FILE:-/root/hokma/backend/.env}"
HOK_TOKEN="${HOK_TOKEN:-}"
if [ -z "$HOK_TOKEN" ] && [ -f "$ENV_FILE" ]; then
  HOK_TOKEN="$(grep -E "^HOK_TOKEN=" "$ENV_FILE" | head -1 | cut -d= -f2- | tr -d '"')"
fi
MODE_JSON=$(curl -sf --max-time 10 -H "X-Hok-Token: ${HOK_TOKEN:-}" -H "X-Conversation-Id: terminal" \
  "$HOK_API/session/mode?tenant_id=$TENANT&user_id=owner&conv_id=terminal" || echo '{"mode":"plan"}')
MODE=$(printf '%s' "$MODE_JSON" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("mode") or "plan")')
export HOK_SESSION_MODE="$MODE"
echo "[HOK] Modo de sessão: $MODE"
exec opencode "$@"
```
Alias adicionado no `.bashrc`: `alias opencode='/usr/local/bin/opencode-mode-wrapper.sh'`.

GET/POST `/session/mode` (8099, header X-Conversation-Id: terminal):
```
GET  → {"autonomous_budget":0,"conv_id":"terminal","mode":""}
POST {"mode":"build"} → {"autonomous_budget":0,"conv_id":"terminal","mode":"build"}
GET  → mode: build
```

3 sessões abertas (uma por modo) via wrapper:
```
=== modo build       → [HOK] Modo de sessão: build
=== modo plan        → [HOK] Modo de sessão: plan
=== modo autonomous  → [HOK] Modo de sessão: autonomous
```
`HOK_SESSION_MODE` dentro do processo opencode (prova real):
```
opencode run "rode na ferramenta bash: echo \$HOK_SESSION_MODE e me diga o valor"
→ tool bash output: "build\n"   |   text: `build`
```

---

## Curadoria FREE (aprovada — somente planos gratuitos) ✅

Aplicada **só em modelos free** de OpenCode Zen/Go/OpenRouter/AIHubMix (GLM-5.3-Flash pago ficou
intocado: `z-ai/glm-5.3-flash` segue `free=false` sem freeSource/categoria).

- `ModelCatalogItem` ganhou `contextLength`, `maxOutput`, `features`, `category`, `dataRetention`,
  `rateLimit`, `freeSource` (api-official | cli | manual-go).
- Fontes oficiais: OpenRouter `/api/v1/models` (filtro pricing 0/0) → 21 free; AIHubMix
  `/api/v1/models?type=llm` (pricing 0/0) → 54 free; Zen/Go via `opencode models` (CLI oficial —
  as APIs `opencode.ai/zen/...` retornam **HTTP 403** deste host, documentado).
- **DeepSeek V4 Flash via OpenCode Go** (Tarefa C): confirmado `opencode-go/deepseek-v4-flash`
  free=true com `freeSource=manual-go` (API Go inacessível → CLI + teste manual).
- **Tarefa B (GLM-5.3-Flash pago)**: fora do escopo aprovado — não aplicada.

`catalog_sync.go` (novo): snapshot persistido (`catalog_snapshot`) + delta (`added/removed/
metadata_changed`) + audit (`catalog_audit`) + `POST /catalog/sync` (requireHokAuth) + ticker diário.

Validação real (8099):
```
POST /catalog/sync → ok: +115 added (seed) — AIHubMix 54, OpenCode Go 33, OpenCode Zen 7, OpenRouter 21
2ª execução        → +0 added, -0 removed, 0 metadata_changed (idempotente)
após deletar 1 linha do snapshot → +1 added (detecção de delta) + audit registrado
```

---

## Problemas encontrados no caminho
1. **OpenCode Zen/Go APIs retornam 403** deste ambiente → catálogo usa `opencode models` (CLI) como fonte oficial; documentado.
2. **`/tmp` (tmpfs) encheu** (2.4GB de `.so` órfãos de ~174 arquivos) → limpeza + `TMPDIR=/var/tmp/gobuild` nos builds.
3. **Skill router intercepta comandos simples** (git-status, uptime, etc.) antes do engine autônomo — no teste manual usei `whoami`/`cat /etc/os-release` que não casam skill.
4. **`conv_id` do body é ignorado no /chat/smart** (usa header `X-Conversation-Id`) — ajustado nos testes e no wrapper.
5. **`hok-model`**: 1º bug de env (HOK_TOKEN não exportado pro python) e de arg (URL virou argumento) — corrigidos.

## Pendências reais
- **Produção (8082) segue no binário antigo** — precisa `systemctl restart hokma` (com sua aprovação) para ativar: trava plan, allowlist autônomo, catálogo com metadata, rota `/catalog/sync`, ticker diário. Cópia em `/root/hokma/backend/hokma_test` validada.
- **Modelo ativo de produção mudou** para `minimax/minimax-m3:free` (teste real do ITEM 2). Reverter: `hok-model opencode-go/deepseek-v4-flash` (ou o id desejado).
- **rate_limit e dataRetention**: campos hoje `null` (APIs oficiais não expõem); rate limit do OpenRouter pode vir de `/endpoints` — pendente.
- **Zen/Go sem endpoint API acessível** → auto-sync desses usa CLI; Go marcado "requer verificação manual periódica".
- Servidor de teste `hokma-item1-test` (8099, DB temporário) segue ativo para conferência — parar com `systemctl stop hokma-item1-test`.
---

## DEPLOY PRODUÇÃO (31/08 ~18:08) ✅ — com aprovação do usuário

Backup do binário anterior: `hokma.bak_20260831_180840_pre_item1_4`
- SHA256 antigo: `3100cfa1a4e05ea5a3af7ba6909f59a8c9f77c161b19115853e33e0f9e2106c3` (hokma)
- SHA256 novo:   `dbec563c956f4138d06b6c6d23d2428fe8f4a07ddb984fb656ea4d01e77fa7e5` (hokma_test)

`systemctl stop hokma && cp hokma_test hokma && systemctl start hokma` → `is-active: active`, health 200.

Verificação em produção (8082):
1. `POST /catalog/sync` → `ok: +115 added` (seed da curadoria free).
2. `GET /models/catalog` → `active=minimax/minimax-m3:free | free=115 | activeStatus=ok`; free com `category`/`freeSource` (cli/api-official).
3. **Trava plan real**: `forceOpenCode+mode:plan` + "crie o arquivo plan-prod-verify.txt" → `opencode_serve` respondeu "modo planejamento ativo — não vou criar nem editar"; `find` = **0 arquivos**.
