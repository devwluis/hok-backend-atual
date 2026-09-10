# ADENDO — Fix: deepseek-native sem fallback silencioso (itens 1, 2 e 5)

**Data:** 2026-09-10
**Escopo:** itens 1, 2 e 5 da auditoria de roteamento nativo. Itens 3 (`tryOpenCodeServe`) e 4 (`RunAgentLoop`) **ficaram FORA do escopo** desta sessão.

---

## Identificadores e backups

| Item | Valor |
|---|---|
| Checkpoint commit (pré-fix) | `e09f6eb7a04b69897c42675ae2bd64eb55632511` |
| Commit do fix | `c8b5608f32ed4b13253e5c5a091189fdefaf8972` |
| Backups físicos (código) | `/root/backups/pre_native_fix_20260910_075004/` (hermes_client.go, opencode_client.go, routes.go, agent_loop_groq.go, agent_orchestrator.go, frontend_loop.go) |
| Backup do binário produção | `/root/backups/hokma_bin_pre_native_fix_20260910_075004` |
| Build deployado (hash) | `787299ce81506324597970161a604a02` |

---

## Baseline "antes" (estado saudável)

```
systemctl status hokma → Active: active (running) since 07:35:38
curl -sI https://app.imoveischaves.com → HTTP/2 200
health :8082/health → 200
modelo ativo: deepseek-native/deepseek-flash
```

## Validação "depois" (pós-deploy)

```
systemctl status hokma → Active: active (running) since 07:54:19 (novo PID 2952425)
curl -sI https://app.imoveischaves.com → HTTP/2 200
health :8082/health → 200
```

---

## O que foi mudado

### Item 1 — hermes_client.go
- Novo `errNativeUnsupportedByHermes`.
- `callHermes()`: se modelo nativo → retorna erro claro **antes** de montar o `docker exec hermes-gateway`; **sem** fallback para `hermesModelB` (OpenRouter).
- `callHermesWithMode()`: mesma checagem (defesa em profundidade p/ chamadores diretos, ex.: tryHermes autônomo).

### Item 2 — opencode_client.go
- `callOpenCode()`, `callOpenCodeApproved()`, `callOpenCodeAutonomous()`: capturam o modelo ativo; se nativo e a chamada falhar, **propagam o erro** em vez de cair no `ModelB` (OpenRouter).

### Item 5 — pontos de entrada não-chat
- `agent_loop_groq.go` (`/agent-loop/tools`): guard nativo antes de `RunAgentLoop` → mensagem clara (não cai no OpenRouter).
- `routes.go` (`handleRoot`): se `routeModel` falhar com modelo nativo → erro claro, **sem** o fallback `callOR(...)`.
- `frontend_loop.go` (`/frontend-loop`): se `req.Model` for nativo → HTTP 400 com mensagem clara.
- `agent_orchestrator.go` (`runEngineToolExec`): se ativo nativo e engine `hermes`/`opencode` → mensagem clara (o engine `claude` segue, usa `/anthropic`).
- `smart_chat.go`: constante `nativeEngineUnsupportedMsg` (usada nos guards).

**NÃO alterado:** guard do chat web (`runSmartTextCascade`/`nativeModelSelection`), `tryOpenCodeServe` (item 3), `RunAgentLoop` (item 4).

---

## Resultado dos testes

### Antes (comportamento do bug)
- `docker exec hermes-gateway hermes ... -m deepseek-native/deepseek-flash --provider openrouter` → **`HTTP 401: User not found.`** (gateway Hermes só fala OpenRouter).
- `/agent-loop/tools` e `/frontend-loop` com nativo → tentavam OpenRouter/ModelB silenciosamente.

### Depois (validado)
| Teste | Resultado |
|---|---|
| `callHermes("teste")` com ativo nativo (unit test isolado) | retorna `errNativeUnsupportedByHermes`, **sem** docker/OpenRouter ✅ |
| `callHermesWithMode(nativo)` (unit test isolado) | idem ✅ |
| `POST /agent-loop/tools` (prod, ativo nativo) | `"Modelo nativo deepseek-native/* nao e suportado por este motor/caminho; use o engine chat ou opencode."` ✅ |
| `POST /frontend-loop` (prod, model nativo) | HTTP 400 mensagem clara ✅ |
| Regression: `/chat/smart` forceHermes + nativo | `engine: chat`, `model_used: deepseek-native/deepseek-flash` (guard intacto) ✅ |
| `go build` / `go vet` / `go test ./...` | todos OK ✅ |

> Observação: o item 2 (opencode) só age em caso de FALHA da chamada nativa — como o provider `deepseek-native` funciona no opencode, o fallback não é exercitado no caminho de sucesso; a mudança garante que, se falhar, não vaza para OpenRouter.

---

## Rollback (se necessário)
```bash
# opção 1 — git
cd /root/hokma/backend && git reset --hard e09f6eb7a04b69897c42675ae2bd64eb55632511
# opção 2 — binário
cp /root/backups/hokma_bin_pre_native_fix_20260910_075004 /root/hokma/backend/hokma
systemctl restart hokma
```

---

## Fora do escopo (reafirmado)
- **Item 3 — `tryOpenCodeServe`** (`opencode_serve_flow.go:88-90`): continua reescrevendo nativo→ModelB. **Não tocado.**
- **Item 4 — `RunAgentLoop`** (`agent_loop_groq.go:713-719`): continua usando ModelB para não-free. **Não tocado** (apenas guard no handler de entrada).

## Commit / Push
- `c8b5608` em `main` (hok-backend-atual), push OK. Inclui o checkpoint `e09f6eb` (commit `git add -A` de segurança pré-fix).
