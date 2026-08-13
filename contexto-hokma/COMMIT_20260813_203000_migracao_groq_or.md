# COMMIT — Migracao do LLM: Groq -> OpenRouter DeepSeek v4 flash

Data: 2026-08-13 20:30 · Por: Washington + opencode (sessao remota via SSH)
Escopo: backend Go (/root/hokma/backend) + .env

---

## 1. Motivo
Usuario nao usa mais Groq: stack e OpenRouter com DeepSeek v4 flash
(deepseek/deepseek-v4-flash-0731, mesmo modelo do .env MINIMAX_AGENT_MODEL
e do agente n8n hok-saude). O backend ainda chamava Groq em 6 pontos:
selectBestModel, routeModel (pool), callGroq (fallback /chat e /summary),
callLLMWithFallback (pool com Groq/Llama-70B), askModelForSkill (skill
router) e default do handleRoot (llama-3.1-8b-instant).

## 2. Mudancas (commit 365282f, +47/-86)
- ai.go: selectBestModel -> sempre defaultChatModel
  (deepseek/deepseek-v4-flash-0731). routeModel: deepseek/* -> callAPI
  direto no OR (sem pool). Removido bloco llama/gemma/mixtral para Groq.
  Pool callLLMWithFallback: OR/DeepSeek-v4-flash primeiro, depois
  Cerebras -> Gemini Flash-Lite -> OR free (sem Groq).
- routes.go: handleRoot default = defaultChatModel; fallback callGroq ->
  callOR(defaultChatModel); /summary fallback idem.
- task_agent.go: askModelForSkill usa OR_KEY + defaultChatModel (era
  GROQ_KEY + llama-3.3-70b-versatile, com fallback OR); callers sem o
  bloco de leitura de GROQ_KEY do .env.

## 3. BUG descoberto e corrigido no .env
- OR_KEY no .env estava INVÁLIDA ("User not found", 401) — o codigo
  priorizava OR_KEY e o chat falhava com "Erro no chat: API error: User
  not found." A chave valida e OPENROUTER_API_KEY (uso $1.97, OK).
- Fix: OR_KEY = OPENROUTER_API_KEY (mesma chave). Backup: .env.bak_20260813_2027.
- Lição: validar chaves de API antes de assumir que funcionam (a
  "Insufficient Balance" vista em logs antigos era dessa chave invalida).

## 4. Validacao E2E
- Chat: "Oi Hok, como voce esta hoje?" -> resposta natural do DeepSeek
  v4 flash via OR (mode:chat).
- Skill router: "Ver status do docker" -> skill Docker Status identificada
  (mode:skill), agora decidida pelo DeepSeek v4 flash via OR.
- go build OK, go vet OK, hokma.service ativo, /health OK.

## 5. Notas
- ASR de audio (callGroqASR / transcribeAudio) AINDA usa Groq Whisper
  (whisper-large-v3) — nao e LLM, e o unico ASR configurado. Trocar por
  outra fonte se desejado.
- callGroq ainda existe como funcao morta (sem callers) — remover depois
  se quiser.
- Pendencias: rotacao token n8n, seguranca SaaS 12/08, vision com chaves
  invalidas (OR Vision ja funcionando com a chave corrigida, verificar
  Gemini/OpenAI).

---

*Gerado a partir de sessao real no servidor (13/08/2026, 20:30). Complementa
COMMIT_20260813_201500 e os demais COMMIT_* da pasta.*
