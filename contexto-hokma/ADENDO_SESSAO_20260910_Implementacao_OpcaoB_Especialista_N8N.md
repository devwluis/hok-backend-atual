# ADENDO — Implementação Opção B: Gap do Especialista N8N (n8n_expert.md)

**Sessão:** 2026-09-10 (implementação e validação)
**Complementa:** ADENDO_SESSAO_20260910_Gap_Especialista_N8N_n8n_expert (fileId `1SEY3ADT_OXtO96rv0VWG8irsr6_-YR1G`)
**Status:** ✅ Implementação e testes concluídos. Commit e deploy PENDENTES (aguardando aprovação explícita).

---

## 1. Resumo Executivo

A Opção B documentada no adendo anterior foi **implementada e validada**. O subagente "Especialista N8N" (dropdown do engine Orquestrador) agora tem acesso completo ao `n8n_expert.md` (~13,5KB / ~3,800 tokens) quando seu campo `knowledge` está vazio — sem o defeito de truncamento da Opção A.

---

## 2. Contexto

Referência ao adendo anterior (mesmo problema):
- **ADENDO_SESSAO_20260910_Gap_Especialista_N8N_n8n_expert** (fileId `1SEY3ADT_OXtO96rv0VWG8irsr6_-YR1G`)
- Problema: dropdown "Especialista N8N" não tinha acesso ao `n8n_expert.md`, enquanto o engine automático `n8n_agent` já injetava esse arquivo
- Lacuna confirmada por esquecimento (campo `knowledge` vazio na tabela `hok_agents`)
- Opção A descartada: `truncateStr(a.Knowledge, 4000)` cortaria ~70% do conteúdo (13,523 → 4,000 chars)
- Opção B escolhida: injetar via código em `subagentSystemPrompt`, condicional ao nome + knowledge vazio

---

## 3. Implementação

### Diff aplicado
Arquivo: `backend/agent_orchestrator.go` (função `subagentSystemPrompt`, ~linha 886-892)

```diff
@@ -886,6 +886,9 @@ func subagentSystemPrompt(a *HOKAgent, task string) string {
 	if a.Knowledge != "" {
 		p += "\nBASE DE CONHECIMENTO:\n" + truncateStr(a.Knowledge, 4000) + "\n"
 	}
+	if a.Name == "Especialista N8N" && a.Knowledge == "" {
+		p += n8nContextSuffix()
+	}
 	return p
 }
```

### Confirmações técnicas
- `n8nContextSuffix()` já existia em `agent_loop_groq.go:1014` — mesma função usada pelo engine `n8n_agent` (`agent_loop_groq.go:738`)
- Ambas em `package main` → **sem necessidade de mover ou exportar**
- Acesso direto, sem importações novas
- 3 linhas adicionadas, sem alteração em outras funções ou caminhos

### Funcionamento
- **Especialista N8N + knowledge vazio** → injeta `n8n_expert.md` COMPLETO (sem truncamento)
- **Qualquer outro agente** → sem alteração (não injeta)
- **Especialista N8N + knowledge preenchido** → usa o knowledge do DB, sem injeção duplicada

---

## 4. Validação

### Gates técnicos
| Gate | Resultado |
|---|---|
| `go build ./...` | ✅ exit 0 |
| `go vet ./...` | ✅ exit 0 |
| `go test ./...` | ✅ `ok hokma_backend 23.762s` |

### Smoke test (teste Go temporário, executado e removido após validação)
| Caso | Resultado |
|---|---|
| Especialista N8N + knowledge vazio | ✅ n8n_expert.md injetado **COMPLETO** — 13,556 chars (sem "...[truncado]") |
| Outro agente + knowledge vazio | ✅ NÃO recebeu n8n_expert.md |
| Especialista N8N + knowledge preenchido | ✅ Usa knowledge do DB, sem injeção duplicada |
| Remoção do teste temporário | ✅ `smoke_n8n_expert_test.go` deletado após execução |

### Dados medidos
- Tamanho total do prompt com n8n injetado: **13,667 chars**
- Tamanho do conteúdo n8n injetado: **13,556 chars** (= conteúdo completo do `n8n_expert.md`)
- Marker confirmado no prompt: `--- BASE DE CONHECIMENTO N8N ---`
- Sem perda de conteúdo (vs Opção A que cortaria para 4,000 chars)

---

## 5. Status

| Item | Status |
|---|---|
| Lacuna documentada | ✅ (adendo anterior) |
| Opção A descartada | ✅ (truncamento 70%) |
| Opção B escolhida | ✅ (adendo anterior) |
| **Implementação** | ✅ **Aplicada** (diff acima) |
| **Build** | ✅ **OK** |
| **Vet** | ✅ **OK** |
| **Testes** | ✅ **OK** (23.762s) |
| **Smoke test** | ✅ **3/3 casos passaram** |
| **Commit** | ⏳ **PENDENTE** (aguardo mensagem do usuário) |
| **Push** | ⏳ **PENDENTE** (após commit) |
| **Deploy** | ⏳ **PENDENTE** (após push + aprovação) |
| **Validação em produção/staging** | ⏳ **PENDENTE** (após deploy) |
| Hash de commit | ❌ Não aplicável (não commitado ainda) |
| Status de deploy | ❌ Não aplicável (não deployado) |

---

## 6. Pendências

1. **Commit** — aguardando mensagem do usuário. Arquivo: `backend/agent_orchestrator.go` (3 linhas adicionadas)
2. **Push** — após commit, seguir fluxo padrão `git push origin main`
3. **Deploy** — após push, rodar `./deploy.sh` (build → test → backup → restart → health check)
4. **Validação em produção** — após deploy, smoke test real: selecionar "Especialista N8N" no dropdown em produção e confirmar que o prompt contém o conteúdo completo do `n8n_expert.md` (ex: perguntar sobre `N8N_BLOCK_ENV_ACCESS_IN_NODE` — deve responder com base no markdown, não apenas tentar usar a tool)
