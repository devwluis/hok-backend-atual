# ADENDO — Sessão 07/11 10:16-10:40 (BRT)

## Diagnóstico do Problema

### Causa-raiz 1: ModelB (minimax/minimax-m3:free) MORREU no OpenRouter
- OpenRouter retorna 404: "This model is unavailable for free"
- ModelB era o fallback padrão em TODO o sistema:
  - `tryOrchestrator`: redirect de modelos pagos/OpenCode → ModelB
  - `agentEffectiveModel`: default quando activeModel não é free
  - `sanitizeModelForOpenRouter`: redirect de slugs inválidos
  - `fallbackChatModel`: fallback do chat normal
  - `claude_code_client`, `opencode_client`, `hermes_client`: fallback secundário

### Causa-raiz 2: tool_call XML vazando para o usuário
- Modelos sem function-calling nativo (nemotron, gemma, etc.) retornam:
  ```
  <tool_call><function=n8n_list_executions>...</function>...</tool_call>
  ```
- O código verificava `len(respMsg.ToolCalls) == 0` → retornava Content bruto
- Resultado: XML puro exibido no chat do frontend

### Cadeia de falha completa
1. Usuário seleciona "Auto" → activeModel="auto" no DB
2. `isFreeModel("auto")=false` → req.Model reescrito para ModelB
3. ModelB (minimax) morreu → OpenRouter 404
4. Fallback para ModelC (nemotron) → nemotron suporta tools via native
5. Mas quando falha, nemotron retorna XML como texto
6. XML exibido ao usuário

## Soluções Implementadas

### Fix 1: Substituir ModelB
```
globals.go:
- ModelB = "minimax/minimax-m3:free"
+ ModelB = "nvidia/nemotron-3-super-120b-a12b:free" // minimax-m3:free morreu no OpenRouter
```
- Todas as 50+ referências a `ModelB` no código agora apontam para nemotron
- `validatedModels` deduplicado (ModelB = ModelC, 1 entrada)

### Fix 2: stripToolCallXML()
```
agent_orchestrator.go:
+ func stripToolCallXML(s string) string {
+     re := regexp.MustCompile(`(?s)<tool_call>.*?</tool_call>`)
+     return re.ReplaceAllString(strings.TrimSpace(s), "")
+ }
```
Aplicado em:
- `RunOrchestrator`: resposta final (line 517)
- `RunOrchestrator`: loop detectado (line 562)
- `RunOrchestrator`: spinning detectado (line 598)
- `runSubagent`: resposta final (line 822)
- `runSubagent`: spinning forçado (line 835)

## Validação

### Testes automatizados
- `go test -count=1 -timeout 60s ./...` → PASS (todos)

### Produção
- **Nemotron (ModelB=ModelC)**: orchestrator completou 3 steps em ~20s, resposta limpa
- **DeepSeek V4 Pro**: completou em 10s, sem timeout
- **Journal**: sem erros, sem spinning, sem XML leakage

## Impacto
- Todos os paths de fallback do sistema agora usam nemotron (function-calling validado)
- Respostas com XML são sanitizadas antes de chegar ao frontend
- ModelB morto (minimax) não afeta mais nenhum fluxo

## Commit
- `159d230` — "fix: ModelB dead on OpenRouter + strip tool_call XML from replies"
- Push para origin/main

## Arquivos alterados
- `globals.go` — const ModelB updated
- `agent_orchestrator.go` — stripToolCallXML + 5 pontos de aplicação
- `ai.go` — comment update (ModelB reference)
- `opencode_client_test.go` — test assertion updated
