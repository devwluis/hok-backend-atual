# ADENDO — Implementação Opção B: Gap do Especialista N8N (n8n_expert.md)

**Sessão:** 2026-09-10 (implementação e validação)
**Complementa:** ADENDO_SESSAO_20260910_Gap_Especialista_N8N_n8n_expert (fileId `1SEY3ADT_OXtO96rv0VWG8irsr6_-YR1G`)
**Complementa:** ADENDO_SESSAO_20260910_Implementacao_OpcaoB_Especialista_N8N (fileId `13ivXGkEKxWQ09am2kgmxAnssSKgDKIWO`)
**Status:** ✅ Implementação, testes e commit concluídos. Push concluído. Deploy PENDENTE (aguardando aprovação explícita).

---

## 1. Resumo Executivo

A Opção B documentada no adendo anterior foi **implementada, validada e commitada**. O subagente "Especialista N8N" (dropdown do engine Orquestrador) agora tem acesso completo ao `n8n_expert.md` (~13,5KB / ~3,800 tokens) quando seu campo `knowledge` está vazio — sem o defeito de truncamento da Opção A.

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
| **Implementação** | ✅ **Aplicada** |
| **Build** | ✅ **OK** |
| **Vet** | ✅ **OK** |
| **Testes** | ✅ **OK** (23.762s) |
| **Smoke test** | ✅ **3/3 casos passaram** |
| **Commit** | ✅ **f3dc91a** — "fix: injeta n8n_expert.md no Especialista N8N via subagentSystemPrompt" (1 arquivo, 3 linhas) |
| **Push** | ✅ `d1d458d..f3dc91a main -> main`, sincronizado (0/0) |
| **Deploy** | ⏳ **PENDENTE** — aguardando aprovação explícita, ainda NÃO executado |
| **Validação em produção/staging** | ⏳ **PENDENTE** (após deploy) |
| Hash de commit | ✅ `f3dc91a` |
| Status de deploy | ⏳ Não aplicável (não deployado) |

---

## 6. Pendências

1. **Deploy** — aguardando aprovação explícita do usuário para rodar `./deploy.sh` (build → test → backup → restart → health check)
2. **Validação em produção** — após deploy, smoke test real: selecionar "Especialista N8N" no dropdown em produção e confirmar que o prompt contém o conteúdo completo do `n8n_expert.md` (ex: perguntar sobre `N8N_BLOCK_ENV_ACCESS_IN_NODE` — deve responder com base no markdown, não apenas tentar usar a tool)
