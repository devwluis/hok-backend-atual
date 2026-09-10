# ADENDO_SESSAO_20260912 — Cache de Prompt, Terminal Text Selection e Modos do Orquestrador

## Resumo

Sessão abrangente com três frentes de trabalho:
1. **Auditoria de cache de prompt** (DeepSeek/OpenRouter) — diagnóstico completo + instrumentação de usage implementada e deployada; prefix caching explícito descartado; pendente coleta de dados reais.
2. **Investigação de seleção de texto no terminal** (estilo Termius) — diagnóstico da arquitetura com duas abordagens possíveis; decisão ainda em aberto.
3. **Investigação + plano técnico para modos do Orquestrador** (Planejar/Construir/Autônomo Total) — diagnóstico confirmou que os botões são cosméticos; plano técnico detalhado com 10 itens de implementação, aguardando confirmação para executar.

---

## 1. Auditoria de Cache de Prompt (DeepSeek/OpenRouter)

### Contexto

Investigar por que o sistema não estava aproveitando o cache de prompt do DeepSeek (que oferece 1M tokens/24h com cache hit). Prompt idêntico em chamadas consecutivas mas sem cache hit.

### Diagnóstico (leitura de código, sem alteração)

**Onde o prompt é montado:**
- `smartChatSystemPrompt()` (`smart_chat.go:963`) — system prompt base, idêntico entre chamadas.
- `buildFallbackChat()` / `buildFallbackChatWithModel()` — monta `[]chatMessage` com system + history + user message.
- `agent_loop.go` / `agent_orchestrator.go` —历史 acrescenta tools e contexto de agente.

**Dados dinâmicos que quebram cache:**
- Histórico de conversa (msgs anteriores) — muda a cada turn.
- Timestamps ou metadata variável no system prompt — verificado: NÃO há timestamps no system prompt.
- Tools list — muda conforme o engine (chat vs orquestrador).

**Campos de cache nas respostas:**
- DeepSeek API retorna `prompt_cache_hit_tokens` e `prompt_cache_miss_tokens` no campo `usage`.
- OpenRouter não retorna esses campos (cache é transparente).

**Conclusão:** o cache hit depende de prefixo idêntico. O system prompt é estável, mas o histórico variável impede cache hit no DeepSeek para conversas longas. Para prompts curtos (primeira mensagem), o cache deveria funcionar se o system prompt for idêntico.

### Item 1 — Instrumentação de usage/cache (IMPLEMENTADO, DEPLOYADO)

**O que foi feito:**
- Adicionado `APIUsage` struct em `types.go` com campos `PromptCacheHitTokens`, `PromptCacheMissTokens`.
- Adicionado logs `[usage]` em `ai.go` ( chamadas `callAPI`) e `agent_loop.go` (chamadas do agent loop).
- Log inclui: modelo, tokens in/out, cache hit, cache miss, source (chat/orchestrator/etc).

**Deploy:**
- Backup físico criado (`/root/backups/pre_cache_instrumentation_*`).
- Build Go → binário atualizado.
- Serviço reiniciado.
- Health check passou.

**Status:** ✅ Em produção. Logs `[usage]` e `[fallback:usage]` ativos.

**Pendência explícita:** Aguardando volume de uso real para reportar taxa de cache hit. A próxima sessão DEVE coletar amostra dos logs e calcular:
```
Taxa de cache hit = prompt_cache_hit_tokens / (prompt_cache_hit_tokens + prompt_cache_miss_tokens)
```
Se os números mostrarem cache hit baixo (<50%), os Itens 3 e 4 (histórico incremental, mover dados dinâmicos) serão implementados.

### Item 2 — Prefix caching explícito (DESCARTADO)

**Motivo:** DeepSeek já ativa prefix caching por padrão. Não há parâmetro `prefix_only` ou similar na API nativa do DeepSeek que precise ser setado explicitamente. A ausência de cache hit é causada pela variabilidade do prompt (histórico), não por falta de ativação.

### Itens 3 e 4 — Histórico incremental / mover dados dinâmicos (NÃO IMPLEMENTADOS)

**Status:** Aguardando aprovação após ver os números reais de cache hit do Item 1.

---

## 2. Investigação de Seleção de Texto no Terminal (Estilo Termius)

### Contexto

Feature desejada: permitir seleção de texto no terminal com handles arrastáveis (estilo Termius mobile), onde o usuário arrasta para selecionar e pode copiar/colar.

### Diagnóstico da arquitetura atual

**Terminal A (ttyd):**
- Servidor ttyd roda na porta 7681, acessado via iframe cross-origin.
- xterm.js renderiza DENTRO do ttyd (canvas).
- NÃO há acesso direto ao DOM do terminal via JavaScript do frontend HOK (cross-origin).
- Proxy reverso via backend Go (`terminal_routes.go`).

**Terminal B (xterm.js in-app):**
- xterm.js renderizado diretamente no React (componente `TerminalScreen.tsx`).
- Acesso total ao DOM e APIs do xterm.js.
-_terminal_has_selection()` e `terminal_get_selection()` já implementados.

### Duas abordagens diagnosticadas

**Opção A — Handles customizados no Terminal B (xterm.js in-app):**
- Injetar CSS/JS no xterm.js para criar handles de seleção arrastáveis.
- Viável: acesso total ao DOM, APIs de seleção já existem.
- Complexidade: média (precisa de overlay customizado + touch events).

**Opção B — Injeção de CSS/JS no ttyd (Terminal A):**
- Injetar script no iframe do ttyd para criar handles de seleção.
- Problemático: cross-origin policy bloqueia acesso ao DOM do iframe.
- Alternativa: modificar ttyd para servir script customizado, ou usar proxy para injetar.
- Complexidade: alta (depende de modificação no ttyd ou bypass de CORS).

**Decisão pendente:** Qual abordagem seguir? O usuário ainda não esclareceu se quer investir no Terminal B (mais controle, xterm.js nativo) ou no Terminal A (ttyd, mais limitado).

**Status:** ⏸️ Somente diagnóstico. Nenhuma implementação.

---

## 3. Modos do Orquestrador (Planejar/Construir/Autônomo Total)

### Contexto

Os botões Planejar/Construir/Autônomo/Autônomo Total no Chat HOK são funcionalmente cosméticos para o engine Orquestrador. Investigação confirmou que:
- `OrchestratorRequest` não tem campo `Mode`.
- `RunOrchestrator` não usa budget, rollback, nem mode.
- O catálogo `agentTools()` só tem tools de leitura (sem edit/write).
- O guard nativo (deepseek-native/*) pula o orquestrador inteiro → chat puro, zero tools.

### Diagnóstico detalhado (confirmado por leitura de código)

**Frontend:**
- `ModeSelector.tsx:90-104`: os 4 botões fazem `POST /session/mode` (persistido por conversa).
- `chat-stream.ts:181`: payload do chat envia `mode` quando é "plan"|"build".

**Backend:**
- `handleSmartChat` (`smart_chat.go:56-61`): carrega `req.Mode` de `sessionModeLoad` quando vazio.
- `tryOrchestrator` (`smart_chat.go:526`): NÃO passa mode para `RunOrchestrator`.
- `OrchestratorRequest` (`agent_orchestrator.go:53-60`): NÃO tem campo `Mode`.
- `RunOrchestrator` (`agent_orchestrator.go:352`): passa `append(agentTools(), runEngineTool())` sempre, sem checar mode.
- `agentTools()` (`agent_loop_groq.go:60`): `read_file`, `bash_exec` (allowlist read-only), `n8n_*`. **Sem edit/write.**
- `runEngineToolExec` (`agent_orchestrator.go:806`): chama `callOpenCode`/`callClaudeCode`/`callHermes` — tem tools reais mas sem gate de mode.
- Budget/rollback: `autonomousAllow` é engine-agnostic mas NUNCA é chamado pelo orchestrator.
- `smartChatSystemPrompt()` não recebe mode → prompt idêntico em todos os modos.

**Conclusão:**
- Para o engine Orquestrador: os botões são 100% cosméticos.
- Para Claude Code/OpenCode: os botões têm efeito real (gates de permissão).
- A resposta do modelo ("sem bash/edit/write") é tecnicamente correta para o catálogo do orquestrador.

### Plano técnico detalhado (CRIADO, AGUARDANDO CONFIRMAÇÃO)

**Arquitetura recomendada:** delegar mutação via `run_engine` → opencode/claude_code (que já têm tools e gates prontos), NÃO duplicar edit/write no catálogo do orquestrador.

**10 itens de implementação:**

| # | Arquivo | Ação |
|---|---|---|
| 1 | `agent_orchestrator.go` | Adicionar campo `Mode` em `OrchestratorRequest` |
| 2 | `smart_chat.go` | Passar `req.Mode` em `tryOrchestrator` |
| 3 | `agent_orchestrator.go` | Adicionar `describeRunEngineAction()` |
| 4 | `agent_orchestrator.go` | Gate de modo no tool dispatch (run_engine + mutantes) |
| 5 | `agent_orchestrator.go` | Budget check no topo do loop (autonomous_total) |
| 6 | `agent_orchestrator.go` | Atualizar `runSubagent` (adicionar param mode + gate) |
| 7 | `pending_action.go` | Adicionar case "run_engine" no resolver |
| 8 | `db.go` | Migração autonomous→build |
| 9 | `ModeSelector.tsx` | Remover botão "Autônomo", atualizar tipos |
| 10 | `autonomous.go` | Verificar/criar `isAutonomousLike` |

**Comportamento por modo:**
- **Planejar:** run_engine bloqueado. Tools read-only normais. Mutantes bloqueados (descrição + "nenhuma ação executada").
- **Construir:** run_engine e mutantes passam por pending_action (confirmação sim/nao). Pending_action resolve via `resolveRunEnginePendingAction`.
- **Autônomo Total:** budget gate via `autonomousAllow`. Executa direto sem aprovação. Auto-rollback como rede de segurança.

**Decisões tomadas:**
- D1: Mecanismo de aprovação = pending_action existente (não SSE do opencode serve).
- D2: Migrar sessões "autonomous" → "build" (mais seguro).
- D3: Aplicar mesmo gate em `runSubagent`.
- D4: Aplicar pending_action gate no orchestrator para mutantes (falha de segurança existente).

**Riscos identificados:**
- Ruptura do fluxo multi-step no modo Construir (pending_action interrompe o loop).
- `runSubagent` com mode "build" pode ser interrompido por pending_action.
- CHECK constraint legado permanece (SQLite não reavalia).

**Status:** ⏸️ Plano completo, aguardando confirmação do usuário para implementar.

---

## Pendências para Próxima Sessão

1. **Cache hit:** coletar amostra dos logs `[usage]` e `[fallback:usage]`, calcular taxa de cache hit. Se <50%, implementar Itens 3 e 4.
2. **Terminal:** decidir qual abordagem (Opção A: Terminal B handles, Opção B: ttyd injeção). Implementar conforme decisão.
3. **Modos do Orquestrador:** confirmar plano técnico e implementar os 10 itens na ordem definida.
4. **Tarefa descartada:** timeout do hermes_client.go era teste pontual — não retomar.

---

## Observação Final

Esta sessão avançou em diagnóstico e planejamento, mas sem alterações de código em produção (exceto a instrumentação de cache que já estava deployada de sessão anterior). O plano dos modos do Orquestrador é a tarefa de maior impacto e está pronto para implementação — aguarda somente confirmação.

Os três/status estão claros:
- **Cache:** instrumentado, aguardando dados reais.
- **Terminal:** diagnosticado, decisão pendente.
- **Modos:** planejado, implementação pendente.
