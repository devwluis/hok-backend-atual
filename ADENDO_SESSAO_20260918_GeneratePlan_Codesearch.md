# ADENDO SESSÃO 20260918 — GeneratePlan + Codebase Memory (SQLite FTS5)

## 1. GeneratePlan() — Plano obrigatório antes de pending_action

**Arquivo:** `agent_orchestrator.go`
**Backup:** `agent_orchestrator.go.bak_20260918_104907`
**Commit:** patches aplicados localmente (não commitados)

### O que foi feito
- Nova função `generatePlan()` (line 343): chama LLM com system prompt obrigatório + terminador `PLANO_PRONTO`. Retorna `[]string` (lista de passos) ou `[task]` como fallback.
- 3 call sites modificados para gerar plano antes de `setPendingAction`:
  - `RunOrchestrator` run_engine build (line ~756)
  - `RunOrchestrator` mutant build (line ~814)
  - `runSubagent` run_engine build (line ~979) — plano JSON-masheado com `|||PLAN|||` marker
- Handler de subagent pending_action extrai plano do marker (line ~701), fallback para `parsePlan` se ausente.
- Zero alteração em gates de segurança.

## 2. Codebase Memory — SQLite FTS5

**Arquivo:** `scripts/codesearch.py`
**Index:** `.codesearch/index.db`

### O que foi feito
- Script standalone Python (stdlib apenas: sqlite3, os, pathlib) que indexa o repo via SQLite FTS5.
- Comandos: `--init` (indexar), `--force` (rebuild), `--status` (info), `<query>` (buscar).
- Filtros: ignora `node_modules`, `.git`, `vendor`, `__pycache__`, `.codesearch`, `backup_*`, `venv`, `*.bak`, `*.tmp`, etc.
- Resultados rankeados com BM25, limit 20 por padrão.

### Causa raiz do 535MB (desproporcional)
FTS5 cria tabelas auxiliares internas:
- `code_index_data` (BLOB blocks)
- `code_index_content` (content rows)
- `code_index_docsize` (doc sizes)
- `code_index_idx` (inverted index segments)

Essas tabelas acumulam páginas não utilizadas (fragmentação) a cada INSERT/DELETE. Sem VACUUM, o SQLite mantém as páginas alocadas mesmo após DROP/RECREATE. Resultado: 44k rows × ~12KB overhead por página = ~510MB para apenas 1.9MB de texto bruto.

### Fix do VACUUM
Adicionado `conn.execute("VACUUM")` ao final de `init_index()`. Após VACUUM: **510MB → 6.7MB** (índice normal, ~17% do texto bruto). Script agora chama VACUUM automaticamente em todo rebuild.

### Automação do rebuild no fluxo de patch Python
Lógica de `needs_rebuild()`:
1. Se `index.db` não existe → reindexar
2. Se `--force` → reindexar
3. Comparar mtime de `index.db` contra mtime de todos os arquivos fonte (`.go`, `.ts`, `.js`, `.md`, etc.)
4. Se qualquer arquivo fonte é mais novo que `index.db` → reindexar
5. Caso contrário → pular (index já está atualizado)

Integração no fluxo de patch Python: após `go build -o hokma_test .` e `go vet ./...`, adicionar:
```python
subprocess.run(["python3", "scripts/codesearch.py", "--init"], cwd="/root/hokma/backend", check=False)
```
Isso reindexa automaticamente quando arquivos fonte foram modificados pelo patch (~3-5s de execução).

## 3. Arquivos modificados/criados

| Arquivo | Ação |
|---------|------|
| `agent_orchestrator.go` | Modificado (generatePlan + 3 call sites) |
| `agent_orchestrator.go.bak_20260918_104907` | Backup |
| `scripts/codesearch.py` | Criado |
| `.codesearch/index.db` | Criado (6.7MB) |
| `.codesearch/config` | Criado |
| `.codesearch/index.db.bak_*` | Backup (se existente) |
| `.gitignore` | Modificado (adicionado `.codesearch/`) |
| `.gitignore.bak_codesearch_*` | Backup |

## 4. Comando de exemplo

```bash
$ python3 scripts/codesearch.py "generatePlan"
```
Resultado:
```
agent_orchestrator.go:980 [go]
				planFromLLM, _ := generatePlan(ctx, task, messages, usedModel, apiKey)
```

## 5. Pendências

- Nenhuma. Todos os passos implementados e testados.
