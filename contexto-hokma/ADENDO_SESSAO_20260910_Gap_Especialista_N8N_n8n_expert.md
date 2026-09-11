# ADENDO — Gap de conhecimento do Especialista N8N (n8n_expert.md)

**Sessão:** 2026-09-10 (investigação e decisão)
**Status:** ⏳ Pendente de implementação — apenas investigação e recomendação (sem mudança de código/DB nesta sessão)

---

## 1. Resumo Executivo

O subagente "Especialista N8N" (dropdown do engine Orquestrador, `hok_agents` `ag_718147165`) **não tem acesso ao `n8n_expert.md`**, enquanto o caminho automático por palavra-chave (engine `n8n_agent`) injeta esse mesmo arquivo no prompt. É uma **lacuna por esquecimento**, não decisão intencional. Decisão: aplicar **Opção B** (injetar via código em `subagentSystemPrompt`, condicional) — **ainda não implementada**.

---

## 2. Contexto

Existem 2 caminhos distintos para tarefas de n8n:

| Caminho | Disparador | Conhecimento |
|---|---|---|
| **Dropdown → "Especialista N8N"** | Seleção manual (`agent_id` no payload, `forceOrchestrator: true`) | Subagente roda com instruções curtas do DB apenas ("Voce e especialista em n8n. Liste workflows e diagnostique erros.") — campo `knowledge` **vazio** |
| **Chat normal + keyword n8n** | Detecção de palavras-chave ("workflow", "n8n", "fluxo de trabalho") no `classifyEngine` → engine `n8n_agent` | `agent_loop_groq.go:738`: injeta `n8n_expert.md` via `n8nContextSuffix()` no system prompt |

**Consequência:** o "especialista" que o usuário escolhe manualmente é, na prática, **menos informado** que o caminho automático — apesar de ter as mesmas ferramentas (16 tools incluindo `n8n_expert_lookup`).

---

## 3. Investigação

### 3.1 Foi decisão intencional?
**Não.** Nenhum adendo anterior documenta essa separação como proposital. Pelo contrário:
- O agente foi criado **com propósito**: instruções específicas, `kind='subagent'`, `active=1`
- O campo `knowledge` está **vazio** — ou esquecido na criação, ou assumiu que `n8n_expert_lookup` (tool MCP) cobriria tudo
- SOUL.md: "Orquestrar n8n: listar/criar/atualizar/ativar/executar/deletar workflows..." — exatamente o que o n8n_expert.md cobre
- ADENDO_SESSAO_20260902_revisao_workflows_n8n_fix_401 (sessão dedicada a n8n): zero menção ao design dos caminhos de conhecimento

### 3.2 Tamanho do n8n_expert.md
| Métrica | Valor |
|---|---|
| Linhas | 234 |
| Caracteres | 13,523 |
| Estimativa tokens | ~3,400–3,900 |
| Cabe no contexto do subagente? | **Sim** — +~3,800 tokens por chamada, dentro do budget free tier (5-step loop) |

### 3.3 Opção A (popular campo knowledge no DB) tem defeito crítico
`subagentSystemPrompt` (agent_orchestrator.go:887) faz `truncateStr(a.Knowledge, 4000)`.
n8n_expert.md = 13,523 chars → seria cortado para **4,000 chars + "...[truncado]"**. **Perda de ~70% do conteúdo.** Inaceitável para um documento de 234 linhas de armadilhas e padrões.

### 3.4 Opção B (injetar via código) — recomendada
Injetar `n8nContextSuffix()` em `subagentSystemPrompt` (agent_orchestrator.go:880), condicionado:
```
Se a.Name == "Especialista N8N" && a.Knowledge == "":
    p += n8nContextSuffix()
```
- ✅ Conteúdo **completo** (sem truncamento)
- ✅ Auto-atualiza (lê o arquivo em runtime)
- ✅ Mesmo conteúdo do path `n8n_agent` (consistência)
- ✅ Condicional por nome — não afeta outros subagentes
- ✅ Se no futuro `knowledge` for popular no DB, a condição `== ""` ignora o fallback

---

## 4. Implementação

**Status: NÃO APLICADA** — sessão de investigação apenas (conforme instrução). Implementação e teste ficam para próxima sessão, aguardando aprovação.

**Plano quando aplicado:**
1. Editar `subagentSystemPrompt` em `backend/agent_orchestrator.go:880`
2. Adicionar condicional `if a.Name == "Especialista N8N" && a.Knowledge == "" { p += n8nContextSuffix() }` antes do `return p`
3. `go build` + `go test ./...`
4. Smoke test: selecionar "Especialista N8N" no dropdown, perguntar sobre `N8N_BLOCK_ENV_ACCESS_IN_NODE` — confirmar que responde com base no markdown (não apenas tenta usar a tool)
5. Commit + push

---

## 5. Validação

Não aplicável nesta sessão (implementação pendente).

---

## 6. Status

| Item | Status |
|---|---|
| Lacuna confirmada | ✅ Lacuna por esquecimento (não intencional) |
| Tamanho do n8n_expert.md confirmado | ✅ 234 linhas, 13.523 chars, ~3.800 tokens — cabe no contexto |
| Opção A descartada | ✌️ Perda de ~70% por truncateStr(4000) |
| Opção B escolhida | ✅ Recomendação documentada |
| Implementação | ⏳ Pendente — aguardando aprovação |
| Testes | ⏳ Pendentes |
| Commit/push | ⏳ Pendentes |

---

## 7. Pendências

1. **Implementar Opção B** — aplicar a mudança em `subagentSystemPrompt` (agent_orchestrator.go:880)
2. **Validar** — smoke test selecionando Especialista N8N no dropdown
3. **Commit + push** — seguir fluxo padrão
4. **Discutir** se condicional por nome é o melhor gatilho ou se seria melhor por `kind`/tag do agente
