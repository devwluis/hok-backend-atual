# COMMIT — Calibracao de persona do chat (dev PT-BR, comunicacao fluida)

Data: 2026-08-13 20:15 · Por: Washington + opencode (sessao remota via SSH)
Escopo: backend Go (/root/hokma/backend) + agente n8n hok-saude (Telegram)

---

## 1. Diagnostico (validado ao vivo antes das mudancas)
- Mensagem "Hok, me diz quem e voce" era SEQUESTRADA pelo skill router:
  classificada como skill "Perfil do Criador" e bloqueada pedindo aprovacao
  para rodar sqlite3. Conversa casual nao fluia.
- SOUL.md nao existia em /root/hokma/backend/ -> getSoul() caia no
  defaultSoul() com identidade ERRADA (dizia Hetzner, v22, sem ferramentas).
- System prompt do /chat/smart era minimo ("Responda sempre em PT-BR").
- Agente n8n hok-saude (Telegram) com instrucao de 1 linha ("agente de saude").

## 2. Mudancas aplicadas
### 2.1 SOUL.md real (novo, /root/hokma/backend/SOUL.md)
Identidade correta: VPS hokma (Hostinger KVM2, Debian 13), backend Go v22
porta 8082, memory.db, n8n Docker 5678, frontend /var/www/hok-os, repos,
capacidades reais (n8n workflows, agent-loop, shell/fs, CRM, visao, skills,
self-mod com gate) + regras de persona dev PT-BR e honestidade.
- Beneficio duplo: rota / (e /chat usada pelo agente Telegram) injeta o SOUL
  via getSoul(); /chat/smart usa smartChatSystemPrompt() que tambem injeta.

### 2.2 Pre-filtro de small-talk no skill router (task_agent.go)
Nova funcao isSmallTalkOrIdentity(msg):
- Lista de saudacoes/identidade ("oi", "bom dia", "quem e voce", "o que voce
  faz"...) -> retorna true -> trySkillForMessage retorna imediatamente sem LLM.
- Mensagens curtas (<=45 chars) SEM palavra de acao (ver/listar/criar/rodar/
  docker/nginx/status...) -> tambem pulam o roteador (conversa, nao skill).
- Prompt de decisao do roteador reforcado: conversa casual e perguntas sobre
  o proprio HOK NUNCA sao skills; escolher skill so com intencao clara de
  executar acao.

### 2.3 System prompt do /chat/smart (smart_chat.go)
smartChatSystemPrompt(): persona dev senior brasileiro + regras de conversa
(natural, PT-BR, honesto, nunca se apresentar como outra IA) + SOUL.md
injetado. Substitui o prompt minimo anterior.

### 2.4 Agente n8n hok-saude (Telegram) via DB do n8n
Instrucoes atualizadas (agents.schema + agent_history versao ativa
ea5f98b2): persona dev PT-BR para o Telegram, conversa casual curta, usar a
tool chat_hok_backend para conhecimento HOK OS (CRM/imoveis/infra), 3-5
frases maximo. Container n8n_oficial reiniciado; workflow "HOK OS Multi IA
Telegram" segue ATIVO. API publica do n8n nao tem rota de agents (404) —
update feito direto no SQLite do volume n8n_data_v2.

## 3. Validacao E2E (apos deploy, binario novo)
- /chat/smart "quem e voce" -> mode:chat, resposta com identidade real
  (Hostinger, porta 8082, n8n, frontend) — ANTES: skill + aprovacao.
- /chat/smart "Bom dia Hok!" -> resposta natural curta, sem gate.
- /chat/smart "Ver status do docker" -> mode:skill, skill Docker Status
  corretamente identificada (skills reais preservadas).
- /chat (rota do agente Telegram) "quem e voce" -> responde com persona real.
- go build OK, go vet OK, hokma.service ativo.

## 4. Commits
- Backend: 8b8f474 (feat: calibracao de persona...) — 3 arquivos, push OK
  (4dd98c7..8b8f474 main).
- Contexto: este arquivo (branch clean_master).

## 5. Pendencias em aberto
- Rate limit Groq 100k tokens/dia (chat usa llama-3.3-70b-versatile via
  Groq) — ja estourou hoje de manha; fallback OR deepseek "Insufficient
  Balance". Considerar default deepseek via OR ou credito.
- Vision quebrada: OR Vision "User not found", Gemini creds invalidas,
  OpenAI com chave Groq errada (gsk_...).
- Rotacionar token do n8n (exposicao previa no settings do Claude Code).
- Seguranca SaaS 12/08 (8082 sem auth /introspect /status, CORS *, timeout).

---

*Gerado a partir de sessao real no servidor (13/08/2026, 20:15). Complementa
COMMIT_20260813_200045 e os demais COMMIT_* da pasta.*
