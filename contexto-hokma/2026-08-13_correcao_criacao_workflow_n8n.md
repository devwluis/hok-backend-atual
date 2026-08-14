# Contexto — Correção de falhas na criação de workflows n8n via chat

- **Data:** 2026-08-13 (deploy 23:59 UTC) / 2026-08-14 (commit)
- **Sintoma relatado:** criação de workflow via chat às vezes falha ou não entrega resultado, sem erro claro pro usuário.
- **Branch:** main (backend em /root/hokma/backend, serviço systemd `hokma` porta 8082).

## Causa raiz

1. **Corrupção intermitente do modelo `minimax/minimax-m3`** (via OpenRouter) ao gerar `arguments` da tool `n8n_create_workflow`:
   - `"nodes": [""]` (76x nos logs de 7 dias), `"nodes": []` (13x), `["", "", ""]` (2x)
   - Variantes: `position` como objeto `{"item": ["0","0"]}`, `typeVersion` como string `"1.1"`, `settings`/`staticData`/`connections` como strings de espaço/tab.
2. **Erro silencioso:** quando `RunAgentLoop` retornava erro (passos esgotados / timeout OpenRouter), `runSmartText` caía no chat genérico SEM avisar o usuário que a criação falhou.
3. **Bug no fallback do MCP:** `n8nValidateWorkflowViaMCP` retornava `false` (bloqueando a criação) com mensagem "[... fallback local]" quando o MCP respondia sem veredito parseável, mas NUNCA rodava o fallback local.
4. **Binário desatualizado:** correções do patch das 21:49 (agentFailure, validationRetryHint, validações estendidas) estavam no fonte mas o binário de produção (build 20:30) não as continha.

## Mudanças aplicadas (backups `*.bak_pre_20260813_225943`, binário `hokma.prev_20260813_235700`)

| Arquivo | Mudança |
|---|---|
| `smart_chat.go` | `agentFailure`: retry único do agent loop + aviso explícito ao usuário ("Nao consegui concluir a acao de automacao... Nada foi criado") — elimina erro silencioso |
| `pending_action.go` | `validateArgsBeforePending`: + `name` string não-vazia, `settings`/`staticData`/`connections` objetos, `name`+`type` string por node, `typeVersion` número, `position` array de 2 números |
| `agent_loop_groq.go` | `validationRetryHint` (ensina formato correto de nodes no retry) + indentação corrigida do bloco de retry |
| `mcp_n8n_validate.go` | Fallback local REAL (`validateWorkflowJSON`) quando MCP responde sem veredito parseável (antes retornava false e bloqueava) |
| `mcp_n8n_repair_defaults.go` | `n8nRepairNodeDefaults` normaliza `position` objeto → `[x,y]` numérico e `typeVersion` string → número (removido se não converter) |

## Validação

- Build isolado em /tmp (todos .go + internal/chat) + `go vet` + suíte completa: OK
- Testes unitários (11 casos de corrupção conhecida + variantes novas): OK
- **E2E real no n8n de produção** (workflows temporários `zz-e2e-*`, deletados com backup):
  - create válido via MCP validate → OK (id criado)
  - `nodes:[""]` → bloqueado antes da API com `{"status":"error"}`
  - `position:{"item":[...]}` + `typeVersion:"2"` → reparado e criado com sucesso
  - 0 leftovers
- Deploy: binário novo (md5 `a63e1083964d1bf478c130056a795a99`) idêntico ao testado; serviço ativo sem erros no journalctl; /health OK.

## Modelo e retry

- Modelo padrão mantido: `minimax/minimax-m3` (env `MINIMAX_AGENT_MODEL`). Não foi trocado — corrupção tratada por validação + retry.
- Retry: hint + re-execução; aborta com mensagem visível após 2 falhas consecutivas (`maxConsecutiveValidationFails = 2`, `maxAgentSteps = 10`).
- Gate de aprovação (`pendingActionMap`) mantido: toda ação de escrita passa por ele.

## Pendências

- Arquivos .go modificados AINDA NÃO commitados (este commit contém apenas este contexto).
- Proposto: commit + push pro branch `hok-backend-atual` após confirmação do usuário do teste em produção.
- Teste de criação real via chat ainda pendente de disparo manual pelo usuário (gasta créditos LLM).