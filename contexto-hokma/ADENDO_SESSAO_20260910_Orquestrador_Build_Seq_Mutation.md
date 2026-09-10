# ADENDO — Sessão 2026-09-10

## Resumo
Fix do orquestrador para mutações sequenciais em build mode — garantir que o modelo use `run_engine` sequencialmente (arquivo N de M) e pare ao completar o plano.

---

## 1. Problema
O modelo do orquestrador repetia a mesma mutação em vez de progressar para o próximo arquivo da sequência. Em build mode com task "Edit 2 files: 1) a.txt, 2) b.txt", o modelo executava a.txt e repetia a.txt indefinidamente.

## 2. Causa Raiz
- O prompt do orquestrador não continha instruções claras de sequenciamento numerado
- O `pending_action.description` não incluía informação de posição na sequência ("arquivo N de M")
- Após concluir uma mutação, o modelo não tinha instrução explícita para continuar ao próximo item
- Ferramentas `read_file` e `bash_exec` disponíveis no build mode permitiam ao modelo inspecionar arquivos em vez de usar `run_engine`

## 3. Fixes Implementados

### 3.1 Instruções de expansão de task (`orchestratorInstructions`, `agent_orchestrator.go:822-860`)
- Adicionada regra: expandir task ANTES de planejar, listando cada mutação como passo numerado
- Cada passo deve usar caminho absoluto e especificar a ação exata
- Instrução: "arquivo N de M" deve ser referenciado no planejamento
- Regra: nunca parar silenciosamente após aprovação — sempre continuar ao próximo passo ou declarar conclusão

### 3.2 `describeRunEngineAction` atualizado (`agent_orchestrator.go`)
- Extração de informação de sequência ("arquivo N de M") da task
- Append ao description do pending_action para feedback visual ao usuário

### 3.3 `progressMsg` atualizado (`pending_action.go`)
- Mensagem de progresso inclui: "arquivo N de M" e instrução explícita "nunca pare silenciosamente"
- Após aprovação, o modelo recebe instrução de continuar ao próximo passo

### 3.4 Ferramentas restritas em build mode (`agent_orchestrator.go:404-416`)
- `read_file` e `bash_exec` filtradas das ferramentas do orquestrador quando `req.Mode == "build"`
- Força o modelo a usar exclusivamente `run_engine` para mutações

### 3.5 Persistência de estado (`orchestrator_state` table)
- Tabela `orchestrator_state` criada em `db.go`
- `saveOrchestratorState`, `loadOrchestratorState`, `clearOrchestratorState` implementados
- Estado inclui: Messages, Step, UsedModel, Plan, Completed, NextTask

### 3.6 Resume com `ApprovalResult` (`RunOrchestrator`)
- Restaura mensagens/step/model do `ResumeFrom`
- Injeta `ApprovalResult` como tool result + instrução de continuação
- O modelo resume exatamente de onde parou

## 4. Validação
- Testes confirmaram: expansão de task funciona consistentemente
- Modelo identifica corretamente "arquivo 1 de 2", "arquivo 2 de 2"
- Progresção a.txt → b.txt confirmada em múltiplas execuções
- API DeepSeek instável (`context deadline exceeded`) impede validação completa do end-of-plan

## 5. Notas
- DeepSeek v3.1 no OpenRouter é instável — timeouts frequentes
- Nemotron (fallback) não disponível via OpenRouter
- Código compila e funciona — testes end-to-end bloqueados por infraestrutura

---

## 6. DeepSeek Native Support (10/09)

### Changes Made
- Modified `callGroqAgentLoop` in `agent_loop_groq.go` to detect `deepseek-native/*` models and route to `DS_URL` (`https://api.deepseek.com/v1/chat/completions`) instead of OpenRouter
- Strips `deepseek-native/` prefix from model name before sending to DeepSeek API
- Falls back to `DEEPSEEK_API_KEY` when `DS_KEY` is not set
- Removed guard in `agent_orchestrator.go` that blocked native models
- Removed guard in `agent_loop_groq.go` that blocked native models

### Status
- Code: ✅ Working — `deepseek-native/deepseek-flash` routes to `DS_URL` correctly
- DeepSeek API governor: ❌ Rate limit (`Authentication Fails (governor)`) — intermittent
- Confirmed working alternative: `anthropic/claude-sonnet-4` on OpenRouter (end-to-end test passed)

### API Test Result
- Direct curl to DeepSeek API: ✅ Works
- Orchestrator routing to DS_URL: ✅ Works
- DeepSeek governor/rate limit: ❌ Blocks full orchestrator flow
