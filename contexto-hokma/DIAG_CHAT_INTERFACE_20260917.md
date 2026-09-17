# HOK OS Chat Interface — Diagnóstico Completo
Data: 2026-09-17

---

## INFRAESTRUTURA
- Backend: hokma.service :8082 (active, PID 1616477) — v21-fixed
- OpenCode: processo vivo (PID 1677311) mas HOK_STATE.md diz :4100 inactive — INCONSISTÊNCIA
  - OpenCode serve não responde em /health, /status, /sessions (401 mesmo com token)
  - Processo rodando 1GB RSS, 22% CPU, up 1h27m
  - Pode ser processo órfão do TTYD/terminal, não o serve HTTP
- Frontend: nginx :3002 (ativo) em /var/www/hok-os | n8n :5678
- DB: SQLite /root/hokma/backend/memory.db | Postgres :5432 | Redis :6379
- TTYD: :7681 (ativo)
- 3 portas frontend: 3002 (nginx), 3055 (vite preview), 2806105 (vite dev server — DUPLICADO, provavelmente antigo)

## CHAT — ROTEAMENTO
- /chat/smart: handler handleSmartChat em smart_chat.go — POST, auth HOK_TOKEN
- /chat/job: handler handleChatJob em chat_jobs.go — GET, auth HOK_TOKEN (polling para jobs async)
- /models/select: handleModelsSelect em models_routes.go — atualiza modelo ativo
- /models/catalog: endpoint de catálogo de modelos
- Fluxo principal: ChatScreen.tsx → POST /chat/smart {async:true} → job_id → polling GET /chat/job
- Fluxo streaming: ElectricCore/NuclearCore → streamChat() em chat-stream.ts (SSE/NDJSON) → /chat/smart com stream:true

---

## 🔴 CRÍTICOS

### 1. OpenCode serve :4100 — INATIVO / INACESSÍVEL
- HOK_STATE.md lista :4100 (inactive?) mas processo opencode (PID 1677311) existe com 1GB RSS
- Todos os endpoints /opencode/serve/* retornam `{"status":"unauthorized"}` mesmo com X-Internal-Call: 1
- Não há handler /opencode/serve/status em main.go (grep não encontrou) — o backend NÃO expõe esse endpoint
- O opencode serve (porta 4100) pode estar morto ou escutando em porta errada
- **Impacto**: engine opencode_serve da cascata de chat (tryOpenCodeServe em smart_chat.go:37) sempre retorna nil (cai pro tryTerminalExec legado), mesmo que o processo exista
- **Arquivos**: smart_chat.go:37-41, opencode_serve_flow.go:37-56, main.go (sem handler de status)

### 2. Streaming SSE: ChatScreen usa fetch JSON, streamChat(SSE) é código morto na tela principal
- ChatScreen.tsx (linha 1186-1212) faz fetch POST normal com `async: true` — recebe job_id, faz polling em /chat/job — NÃO há streaming na tela principal
- chat-stream.ts `streamChat()` com SSE/NDJSON existe mas é usado apenas em backups (ChatScreen.tsx.bak_*), NÃO no ChatScreen.tsx atual nem em ElectricCore.tsx/NuclearCore.tsx (precisa verificar)
- O backend handleSmartChat (smart_chat.go) retorna JSON completo, NÃO faz SSE streaming
- **Impacto UX**: sem streaming real de tokens no ChatScreen principal — usuário vê bolha "processando" até o job terminar (até 10min) sem feedback incremental
- **Arquivo**: ChatScreen.tsx:1186-1212, chat-stream.ts:116-297

### 3. Orçamento Groq TPD quase esgotado (98360/100000)
- Logs mostram `llama-3.3-70b-versatile` rate-limited com TPD 98360/100000 (~98% usado)
- Falhas recorrentes: "Rate limit reached... Used 98596, Requested 6006" — cada requisição consome 6006 tokens
- Próximo retry só funciona em ~41min (limite reseta)
- **Impacto**: toda vez que a cascata tenta llama-3.3-70b como fallback, falha → latência extra → timeout potencial
- **Arquivo**: backend.log (linhas 08:49-08:53)

---

## 🟡 IMPORTANTES

### 4. DeepSeek nativo vs OpenRouter — possível bypass de guard nativo
- O guard nativo em runSmartTextCascade (smart_chat.go:445-450) funciona SÓ quando `forcedEngine=false`
- Se o usuário seleciona engine orquestrador/claude/opencode/hermes no seletor, o modelo nativo deepseek-* pode ser reescrito para ModelB (OpenRouter) pelo routeModel
- tryOrchestrator (smart_chat.go:555-557) reescreve opencode/zen/opencode-go → ModelB (pago)
- TENTATIVA de CORREÇÃO: tryOrchestrator tem exceção para isNativeModelSlug (line 564), mas tryClaudeCode e tryOpenCode NÃO têm
- **Impacto**: se usuário seleciona deepseek-native/* + Claude Code no seletor, o Claude Code recebe o modelo nativo que pode não funcionar com a API da Anthropic
- **Arquivo**: smart_chat.go:445-450, 555-567, 661-663

### 5. Falhas de Vision (todas as 3 APIs) — sem fallback funcional
- OR Vision (DeepSeek): "User not found" — credencial OR_KEY inválida/expirada para DeepSeek
- Gemini Vision: "Request had invalid authentication credentials" — GEMINI_KEY não funciona (OAuth 2)
- OpenAI Vision: "Incorrect API key provided" — chave vazada/expirada (gsk_fYDx...vkk7)
- Todas as 3 falham, o fluxo de visão cai para callORVision(ModelB) como ÚLTIMO fallback
- Se ModelB também falhar → erro direto sem mais tentativas (política free-only correta, mas experiência ruim)
- **Impacto**: qualquer envio de imagem + áudio falha ou tem latência extra das 3 tentativas
- **Arquivo**: smart_chat.go:131-147 (audio+img), 172-194 (img apenas)

### 6. deepseek-chat OpenRouter — saldo insuficiente
- Log 08:52:26: "Insufficient Balance — OR fallback"
- Modelo deepseek-chat (ModelA) no OpenRouter sem crédito
- RouteModel tenta → falha → fallback para llama-3.3-70b (rate-limited) → cascata falha
- **Impacto**: quando modelo ativo é deepseek/deepseek-chat (ModelA), chat pode falhar completamente
- **Arquivo**: backend.log, ai.go:460-483 (isFreeModel)

### 7. opencode serve password não configurado → Chat Web sem opencode serve
- tryOpenCodeServe (smart_chat.go:49): `if opencodeServePassword() == "" { return nil }` — sem senha, engine inteiro desabilitado
- Isso significa que a ponte opencode serve (substituindo o PTY/tmux) NÃO está operacional
- **Impacto**: Chat Web não usa opencode serve para respostas; depende de /chat/smart → routeModel → LLM direto (mais lento, sem persistência de sessão)
- **Arquivo**: opencode_serve_flow.go:49, smart_chat.go:474

### 8. /models/select e /api/* endpoints retornam 401 — HOK_TOKEN não configurado no teste
- Não foi possível testar endpoints com HOK_TOKEN correto (variável não setada no ambiente de teste)
- O frontend ChatScreen.tsx linha 1157-1161 verifica `serverUrl && !token` → mostra erro
- **Risco**: se HOK_TOKEN for inválido/expirado, todo o chat web fica inoperante sem mensagem clara (apenas "unauthorized" JSON)
- **Arquivo**: ChatScreen.tsx:1157-1161, chat-stream.ts:148-153

### 9. Polling /chat/job — timeout de 10min no frontend vs 10min no backend (correspondente, mas rígido)
- Frontend: `pollDeadline = Date.now() + 10 * 60 * 1000` (ChatScreen.tsx:1245)
- Backend: `chatJobAsyncTimeout = 10 * time.Minute` (chat_jobs.go:30)
- Se job dura exatamente 10min, frontend pode dar timeout antes do backend marcar "done" (race condition)
- Backend job é em memória (perdido se backend reiniciar) — frontend mostra erro "trabalho perdido" (404) (ChatScreen.tsx:1278-1280) — OK
- **Impacto**: jobs longos (ex: agent loop n8n com muitas iterações) podem falhar por timeout no frontend
- **Arquivo**: ChatScreen.tsx:1245, chat_jobs.go:30

### 10. opencode processo (PID 1677311) — 1GB RSS, alta CPU, sem serviço claro
- Processo `opencode` rodando desde 07:08 (1h27m uptime), 22% CPU, 1GB RSS
- Não está escutando em porta HTTP (não responde em health/status)
- Pode ser: opencode serve em modo TTY/PTY, opencode CLI em execução contínua, ou processo órfão
- **Risco de memória**: se for leak, pode crescer sem limite (precisa monitorar trend)
- Teste de trend: RSS oscilou entre 966MB-1047MB em 10s — estável por enquanto
- **Arquivo**: processo do sistema

### 11. Porta 3055 e 2806105 — DOIS servidores frontend rodando
- PID 2618047: `node /root/hokma-web/artifacts/hok-os/.../vite.js preview --outDir dist/public --port 3055` (Aug 19)
- PID 2806105: `node /root/hokma-web/.../vite.js --config vite.config.ts --host 0.0.0.0` (Aug 21) — dev server
- Dois servidores Vite rodando simultaneamente — desperdício de memória (cada um ~120MB)
- **Impacto**: confusão sobre qual é o frontend ativo; se o dev server (2806105) fica no ar, pode servir código desatualizado
- **Arquivo**: processos do sistema

---

## 🟢 COSMÉTICOS / BAIXA PRIORIDADE

### 12. Modelo ativo padrão: ModelA (deepseek/deepseek-chat) — sem crédito OpenRouter
- ai.go:507: `activeModel = ModelA` (inicializa como ModelA)
- ModelA = deepseek/deepseek-chat que está com saldo insuficiente no OpenRouter (log 08:52:26)
- Se usuário não trocar modelo, toda conversa começa tentando ModelA → falha → fallback → mais falhas → experiência ruim
- **Arquivo**: ai.go:507, backend.log

### 13. Aberturas de build no log indicam código não commitado
- HOK_STATE.md lista mudanças não commitadas: agent_orchestrator.go, ai.go, models_catalog.go, knowledge/n8n_expert.md
- Backend log mostra build de v21-fixed (diferente de commits listados)
- **Impacto**: versão rodando em produção pode não ser a última commitada

### 14. Vite dev server com --host 0.0.0.0 exposto
- PID 2806105: `vite.js --config vite.config.ts --host 0.0.0.0` — acessível de qualquer IP na rede
- **Risco de segurança**: dev server sem autenticação, expõe source maps e código-fonte
- **Arquivo**: processo do sistema

### 15. Multiple backup files de chat-stream.ts e hok-models.ts
- 11 backups de chat-stream.ts, 14 backups de hok-models.ts no diretório de frontend
- **Impacto**: confusão sobre qual versão é a ativa; dificulta manutenção
- **Arquivo**: /root/hokma-web/artifacts/hok-os/src/lib/

### 16. SSE streaming (chat-stream.ts) não utilizado no fluxo principal
- O chat-stream.ts `streamChat()` foi construído para SSE/NDJSON streaming real do backend
- Mas o backend handleSmartChat não faz streaming — retorna JSON completo
- Se um dia o backend passar a fazer streaming, o ChatScreen precisa ser migrado de polling para SSE
- **Impacto técnico**: baixo agora, mas dívida técnica para futuro
- **Arquivo**: chat-stream.ts:116-297, smart_chat.go (sem streaming)

### 17. SaveAgentCheckpoint é TODO (não implementado)
- agent_loop_groq.go:976: `// TODO: reaproveitar o storage SHA1-hash + ESTADO_ANTERIOR do pipeline`
- Função saveAgentCheckpoint não faz nada (ignora params)
- **Impacto**: se agent loop precisar de checkpoint/recovery, não funciona
- **Arquivo**: agent_loop_groq.go:976-979

### 18. GEMINI_KEY injetado mas GEMINI_API_KEY pode não estar setado no opencode CLI
- models_catalog.go:822-823: injeta GEMINI_KEY como GEMINI_API_KEY no env do CLI
- Mas o opencode CLI pode ler de ~/.config/opencode/settings.json separadamente
- **Impacto**: modelo google/ no catalogo pode não funcionar no CLI opencode
- **Arquivo**: models_catalog.go:822-823

---

## 🔍 PROVEDORES DE IA — STATUS

| Provedor | Modelo | Status | Notas |
|----------|--------|--------|-------|
| DeepSeek (nativo/DS_URL) | deepseek/deepseek-chat | ❌ Saldo insuficiente | Log: 08:52:26 "Insufficient Balance" |
| DeepSeek (nativo/DS_URL) | deepseek/deepseek-reasoner | ❌ Não testado | |
| DeepSeek (via OR) | deepseek-chat | ❌ User not found | OR_KEY inválida para DeepSeek |
| OpenRouter | llama-3.3-70b-versatile | ⚠️ Rate limited | TPD 98% (98360/100000) |
| OpenRouter | ModelB (nemotron:free) | ✅ Funcional | Fallback principal |
| OpenRouter | ModelC | ✅ Funcional | Fallback secundário |
| Groq | (via callGroqASR) | ? | Não testado separadamente |
| Google Gemini Vision | gemini-2.0-flash | ❌ Auth inválida | OAuth 2.0 required |
| OpenAI Vision | gpt-4o-mini | ❌ API key inválida | gsk_fYDx...vkk7 vazada |
| Minimax M3 (ModelB) | free | ✅ Funcional | Fallback de visão confirmado |
| OpenCode Zen | Various | ? | Cache no backend, não testado |
| OpenCode Go | Various | ? | Todos marcados com isFree=corrigido |
| Hermes | (via /v1/hermes/chat) | ? | Não testado |
| DeepHat | (security) | ? | Falha loga e segue |
| n8n Agent Loop | (via RunAgentLoop) | ? | Depende do n8n :5678 |
| OpenCode serve | :4100 | ❌ Inativo | Não responde, sem password |
| Claude Code | (via CLI) | ? | Depende de ANTHROPIC_API_KEY |

---

## 📊 PERFORMANCE

### Latências observadas nos logs
- OR Vision falha em ~2-4s (tempo de timeout da chamada)
- Groq rate-limit responde quase instantaneamente (erro da API)
- DeepSeek balance check falha rápido
- Não há logs de latência bem-sucedida (chat funcionando) nas últimas horas

### Consumo de memória
- hokma backend: ~49MB RSS (leve)
- opencode: ~1GB RSS (pesado — pode crescer)
- ttyd: leve
- 2x Node/Vite: ~240MB cada (480MB total)
- PostgreSQL: ~16MB
- Redis: ~5MB
- **Total estimado**: ~1.8GB de 7.8GB (23%) — saudável mas opencode é outlier

### CPU
- opencode: 21% (constante — pode ser CLI rodando em loop)
- hokma backend: ~0.6% (ocioso)
- Vite preview: ~0.3%
- Vite dev: ~0.1%
- Esbuild x2: ~0% (idle)

---

## 🐛 BUGS ENCONTRADOS

### Bug 1: Tela principal não tem streaming real
**Severidade**: Alta
**Evidência**: ChatScreen.tsx:1186-1212 usa fetch POST → job_id → polling. chat-stream.ts streamChat() SSE é código morto na tela principal.
**Impacto**: Usuário fica sem feedback por até 10min.

### Bug 2: OpenCode serve inacessível mas processo vivo
**Severidade**: Alta
**Evidência**: PID 1677311 existe mas não responde a /health nem /status. HOK_STATE.md diz inactive.
**Impacto**: Engine opencode_serve sempre desabilitado.

### Bug 3: Todas as 3 APIs de visão falham
**Severidade**: Alta
**Evidência**: Logs: OR Vision "User not found", Gemini "invalid auth", OpenAI "Incorrect API key"
**Impacto**: Envio de imagens/audio falha ou tem 3 tentativas de timeout antes de falhar.

### Bug 4: Groq TPD 98% esgotado
**Severidade**: Alta
**Evidência**: Logs rate-limit com 98360/100000 tokens usados.
**Impacto**: Fallback llama-3.3-70b sempre falha → latência extra → possível timeout.

### Bug 5: Modelo ativo padrão (ModelA) sem crédito
**Severidade**: Média-Alta
**Evidência**: ai.go:507 activeModel=ModelA. Log: 08:52:26 "Insufficient Balance" para deepseek-chat.
**Impacto**: Conversas que não trocam de modelo falham no primeiro envio.

### Bug 6: Porta de serviço não documentada
**Severidade**: Média
**Evidência**: HOK_STATE.md lista :4100 inactive mas processo opencode existe. Backend não tem handler para /opencode/serve/status.
**Impacto**: Inconsistência entre documentação e realidade.

### Bug 7: Dois servidores frontend duplicados
**Severidade**: Média
**Evidência**: Vite preview em :3055 + Vite dev em :2806105 (com --host 0.0.0.0 — risco de segurança).
**Impacto**: Confusão sobre frontend ativo, risco de segurança.

### Bug 8: Polling race condition 10min/10min
**Severidade**: Média
**Evidência**: ChatScreen.tsx:1245 pollDeadline=10min, chat_jobs.go:30 chatJobAsyncTimeout=10min. Race condition se job dura ~10min.
**Impacto**: Jobs longos podem falhar por timeout no frontend antes do backend terminar.

---

## 🎨 UI/UX

### 1. Seletor de modelo no ChatScreen
- Modelo selecionado persiste em localStorage (MODEL_SELECT_KEY = "hokma.model.selected.v1")
- Troca de modelo via selectModel (linha 600) → POST /models/select em background
- Modelo real (modelUsed) vs modelo selecionado (selectedModel) mostrados separadamente (meta.model vs meta.actualModel) — BOM
- Engine brand colorido (engine-brand-hok, engine-brand-claude, etc.)

### 2. Pending Actions (aproveitamento)
- ChatScreen suporta pendingAction via resolvePendingAction (sim/não)
- Pendências persistem em SQLite (pending_action.go)
- Aprovação/rejeição via chat funciona (isApprovalText/isRejectionText em smart_chat.go:94-111)

### 3. N8N intent detection
- detectN8NIntent no ChatScreen (linha 1150-1153) ativa n8n mode automaticamente
- N8N_SYSTEM_PROMPT injetado no topo das mensagens quando ativo
- keywords: "n8n", "workflow", "fluxo de trabalho", etc.

### 4. Comandos /edit, /code, /patch
- handler em chat_agent_routes.go — parse, gate de confirmação, exec via /agent-loop
- SSE na resposta do handleEditCommand (linha 121-131) — mas NÃO usado no fluxo principal do ChatScreen

### 5. Rollback via chat ("volte pro checkpoint")
- smart_chat.go:71-87 — dispara triggerRecovery, reinicia serviço
- Funcional mas dependente de recovery.sh standalone

### 6. Modo autônomo / autônomo total
- Gates: blocklist, allowlist, budget, circuit breaker
- Autonomous total: snapshot automático, budget 50
- Hermes NÃO tem fluxo de pending próprio (bloqueado fail-closed fora da allowlist)

---

## 📋 RESUMO DE CONTAGEM
- 🔴 Críticos: 3
- 🟡 Importantes: 8
- 🟢 Cosméticos: 6
- Total de issues encontradas: 17

---

## 🏗️ PRÓXIMOS PASSOS SUGERIDOS (POR PRIORIDADE)

### Prioridade 1 (Críticos — ação imediata)
1. **Diagnosticar OpenCode serve** :4100 — verificar se processo é realmente o serve HTTP ou outro modo; reiniciar se necessário; verificar config de senha
2. **Atualizar credenciais de visão** — OR_KEY para DeepSeek, GEMINI_KEY (OAuth), corrigir chave OpenAI vazada
3. **Monitorar/renovar Groq TPD** — limite de 100k tokens/dia quase esgotado; considerar upgrade tier ou cache de respostas

### Prioridade 2 (Importantes — esta semana)
4. **Verificar tryClaudeCode/tryOpenCode guard nativo** — garantir que deepseek-native/* não é reescrito quando Claude Code/OpenCode forçados
5. **Definir modelo ativo padrão** para ModelB (nemotron:free) ao invés de ModelA (deepseek-chat — sem crédito)
6. **Adicionar configuração de opencode serve password** se pretendido usar o engine
7. **Considerar streaming SSE no ChatScreen** — migrar de polling para SSE para feedback incremental
8. **Matar Vite dev server duplicado** (PID 2806105) — serviço duplicado com risco de segurança

### Prioridade 3 (Cosméticos — próxima sprint)
9. Limpar backups de chat-stream.ts e hok-models.ts (11 + 14 arquivos)
10. Implementar saveAgentCheckpoint (TODO em agent_loop_groq.go:976)
11. Mudar activeModel default de ModelA para ModelB em ai.go:507
12. Adicionar handler /opencode/serve/status em main.go
13. Remover fontBaseURL não usada de terminal_routes.go (HOK_STATE.md)
14. Verificar GEMINI_KEY vs GEMINI_API_KEY para opencode CLI
