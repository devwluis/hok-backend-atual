# Adendo — Sessão 11/09/2026 · Fix do default perigoso em `agentAllowedTools()` + IP do `n8n_expert.md`

**Origem:** opencode (terminal) | **Data/hora:** 11-09-2026 06:02 (-03) | **Subagente:** Especialista N8N

---

## 1. Fix do default perigoso em `agentAllowedTools()`

### Problema (antes)

`agent_orchestrator.go` — `agentAllowedTools(a)` retornava o **catálogo COMPLETO** de `agentTools()` sempre que o agente não tinha `tools` explícito no DB:

```go
if a == nil || len(a.Tools) == 0 {
    return base   // ← perigoso: inclui n8n_create/update/activate/delete/execute/test_workflow,
}                 //   add_imovel e bash_exec
```

Qualquer subagente futuro criado sem popular `tools` herdaria acesso de **escrita/mutação no n8n e execução arbitrária** sem intenção.

### Verificação prévia (obrigatória)

```sql
SELECT id, name, kind, active, tools FROM hok_agents;
```
| id | name | kind | active | tools |
|---|---|---|---|---|
| ag_718147165 | Especialista N8N | subagent | 1 | `["n8n_list_workflows","n8n_diagnose_workflow","n8n_get_workflow_detail","n8n_get_execution_errors","run_engine"]` |

- **Total de agentes: 1** · **Agentes com `tools` vazio: 0**
- ✅ **Nenhum subagente dependia do default antigo.** Mudança segura; o único agente existente (tools explícito) não sofre regressão.

### Correção (depois)

Novo catálogo **read-only** como default:

```go
var safeDefaultTools = map[string]bool{
    "read_file": true, "env_diagnose_config": true,
    "n8n_list_workflows": true, "n8n_diagnose_workflow": true,
    "n8n_get_workflow_detail": true, "n8n_get_execution_errors": true,
    "n8n_expert_lookup": true,
}

func agentAllowedTools(a *HOKAgent) []toolDef {
    base := agentTools()
    allow := safeDefaultTools                    // default SEGURO
    if a != nil && len(a.Tools) > 0 {
        allow = map[string]bool{}
        for _, t := range a.Tools { allow[t] = true }
    }
    ...filtro...
}
```

Removidas do default: `n8n_create_workflow`, `n8n_update_workflow`, `n8n_activate_workflow`, `n8n_delete_workflow`, `n8n_execute_workflow`, `n8n_test_workflow`, `add_imovel`, `bash_exec`.
`run_engine` continua sendo anexada à parte pelo `runSubagent` (não alterado).

---

## 2. Fix do IP no `n8n_expert.md`

### Antes
```
- N8N em Docker precisa acessar o backend Go via `172.17.0.1:8082` (gateway
  do bridge network), não via `localhost`.
```

### Depois
```
- N8N em Docker precisa acessar o backend Go via `172.16.0.1:8082` (gateway
  do bridge network), não via `localhost`. OBS: `172.17.0.1` NÃO funciona
  neste host — confirmado empiricamente em produção em 11/09/2026 (workflow
  "Monitor Disk and Self-Heal", execução 30324, rodou com sucesso nesse IP).
```

**Carga do arquivo:** `os.ReadFile` a cada chamada (`agent_loop_groq.go:1015`, usado também em `agent_loop_groq.go:738` e `agent_orchestrator.go:890`). **Sem cache → sem restart, sem rebuild.** Vale já na próxima chamada.

---

## 3. Deploy completo (item 1)

| Etapa | Resultado |
|---|---|
| `go build -o hokma_new .` | ✅ hash `f757144ba64c8e3e560fbd6fe42a3ce8` |
| `go vet ./...` | ✅ exit 0 |
| `go test ./...` | ✅ `ok hokma_backend 23.472s` (com `HOK_TOKEN` no env, como o systemd) |
| **Hash produção ANTES** | `be5d05f6f13042d9b982e6caa5f634dc` |
| Backup do binário | `/tmp/hokma.bak_20260911_055026` (md5 `be5d05f6...`) |
| Deploy | stop → `mv hokma_new hokma` → start |
| **Hash produção DEPOIS** | `f757144ba64c8e3e560fbd6fe42a3ce8` |
| Health check | `GET /health` → **HTTP 200** `{"status":"ok"}` |
| Service | `active` |

### Query pós-restart (`hok_agents`)
```
ag_718147165 | Especialista N8N | subagent | active=1 |
["n8n_list_workflows","n8n_diagnose_workflow","n8n_get_workflow_detail","n8n_get_execution_errors","run_engine"]
```
✅ Idêntico ao antes — **sem regressão**.

### Teste funcional real — Especialista N8N (leitura)
```
POST /agents/orchestrate
{"task":"Liste os workflows existentes no n8n. Somente leitura, não altere nada.",
 "agent_id":"ag_718147165","model":"nvidia/nemotron-3-super-120b-a12b:free",
 "max_steps":6,"mode":"build"}
```
Resposta: `status ok`, `steps 1`, `subagents 1` (Especialista N8N, ~27.5s, sem erro).
Output: tabela com os **12 workflows reais** do n8n (via `n8n_list_workflows`). Run persistido: `run_665342180`.

**Backups de código:** `agent_orchestrator.go.bak_20260911_032436_safedefault` · `n8n_expert.md.bak_20260911_032436`.

---

## 4. Pendência

O repo segue com **mudanças não commitadas**:
- `agent_orchestrator.go` (safeDefaultTools)
- `ai.go` (allowlist de pagos: `allowedPaidModels`)
- `models_catalog.go` (filtro de fantasmas Zen via CLI)
- `knowledge/n8n_expert.md` (IP 172.16.0.1)

**Commit/push ainda pendente** — aguardando confirmação.

---

## 5. Achado lateral (webhook de adendos)

O Code node do workflow "HOK OS — Contexto Claude Terminal" (`enSxay8DwbAurLcj`) **descarta o campo `folderId`**:
```js
items[0].json = { fileName, fileContent: conteudo };  // folderId não é propagado
```
Logo, o nó Google Drive cai sempre no default `16zPoX8HrHOHCZgKezWwNmOEQad1eOBjN` (raiz CaixaPreta-Hok). Para este adendo, o arquivo foi enviado pelo webhook e **movido para `CaixaPreta-Hok/Especialista-N8N/` (`15-XSGgo2UngF6QpxulogL--AQjmT0MPB`) via Drive API**. Correção sugerida (não aplicada): propagar `folderId` no Code node para roteamento nativo por subpasta.
