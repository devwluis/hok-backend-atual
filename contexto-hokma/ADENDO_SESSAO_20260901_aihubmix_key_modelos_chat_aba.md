# ADENDO — SESSÃO 01/09/2026 — Credencial AIHubMix + validação de modelos (ox-alpha)

Configuração da chave da AIHubMix (que estava vazia desde 27/08, placeholder do template),
validação do modelo `aihubmix/ox-alpha` (que estava em "unavailable") e investigação do bug de
chat que "trava ao trocar de aba". Tudo aplicado em **produção (8082)** com aprovação do usuário.

---

## 1. Credencial AIHubMix — preenchida e validada ✅

### Estado anterior
- `backend/.env:25` → `AIHUBMIX_API_KEY=` **vazia** (com comentário "gerar em https://aihubmix.com/token").
- **Nunca foi preenchida**: todos os backups do `.env` desde 27/08 (predep/card/jobs/status/zumbi/
  hermesplan/align_model) tinham `len=0`. Era placeholder do template, não omissão recente.
- Impacto: `aihubmix/ox-alpha` (selecionado no picker) **nunca funcionava** — `routeModel` retornava
  `modelUnsupportedReply` imediatamente; modelos AIHubMix do pool em cascata também eram pulados.

### Tentativas
1. **1ª chave** (`sk-po0gy...x32n`, 35 chars) → **401 `invalid key`** na API (teste com chave literal no
   curl também deu 401). Reportada, **sem restart**. Backup: `.env.bak_20260901_095830_aihubmix_key`.
2. **2ª chave** (`sk-FSbDob...6b4c`, 51 chars) → **HTTP 200** no chat `ox-alpha`. Válida.

### Aplicado
- Gravada em `backend/.env:25` (sem ecoar o valor; só comprimento/prefixo mascarados).
- Backup: `.env.bak_20260901_100218_aihubmix_key2`.
- `systemctl restart hokma` → ativo, health `{"status":"ok"}`, env carregada no processo (PID 433799).

### Validação do modelo
- `POST /models/select {"model":"aihubmix/ox-alpha"}` → `{"active":"aihubmix/ox-alpha","status":"ok"}`.
- Chat async: **`model_used: aihubmix/ox-alpha`**, reply "abóbora" em 3s — **saiu do "unavailable"**.
- Propagado: `~/.opencode/opencode.json` → `openrouter/aihubmix/ox-alpha`; `~/.claude/settings.json` →
  `aihubmix/ox-alpha`.

> Observação: a listagem `GET /api/v1/models` da AIHubMix é **pública** (407 modelos mesmo sem key) —
> NÃO serve para validar chave. Validação real = chamada de chat.

---

## 2. Investigação do bug: chat "trava" ao trocar de aba ✅ (causa raiz identificada)

### Pergunta central: frontend usa caminho async ou sync?
**USA o caminho ASYNC.** Evidência:
- Source `ChatScreen.tsx:1029` → `async: true` no body do `/chat/smart`.
- Build servido em produção (`/var/www/hok-os/assets/index-BbODCeSI.js`) contém o fluxo completo:
  `job_id`, `chat/job`, `visibilitychange`, `async:!0`, `"running"` (hash idêntico ao `dist`).
- Backend `chat_jobs.go` roda em goroutine com `context.WithTimeout(context.Background(), 10min)` —
  **desacoplado da conexão HTTP** (não usa `r.Context()` no caminho async).

Logo, **não é** "usar o caminho que já existe" — ele já está em uso.

### Causa raiz (2 bugs combinados, no FRONTEND)
1. **Polling throttled em background**: `ChatScreen.tsx:1058-1060` — `for(;;)` com `setTimeout(2000)`.
   Browsers throttl timers de tabs ocultas (~1/min ou pausado em mobile) → o polling para de fato
   quando a aba perde foco, mesmo com o job `done` no backend.
2. **Retomada bloqueada por `loadingRef`**: `ChatScreen.tsx:744` — handler de `visibilitychange`
   faz `if (loadingRef.current) return;`. Durante o `send()` ativo, `loadingRef=true` → ao voltar à
   aba, o handler retorna e NÃO reanexa o polling → bolha "pensando" eterna, resposta nunca coletada.

### Sobre a hipótese de "cascata dentro de r.Context()"
Risco real **só no caminho síncrono** (`smart_chat.go:237`, `runSmartText(r.Context(),...)`) — que o
frontend **não usa mais**. No caminho async o contexto é `context.Background()` (sobrevive à
desconexão). Teste empírico: job com modelo falhando (`z-ai/glm-5.2:free`) completou em ~3s via pool.

### Modelos testados (Parte 1)
- `z-ai/glm-5.2:free` — falha no provider (`API error: Provider returned error`) → cai no pool
  (`callLLMWithFallback`, ai.go:741) → responde via fallback (`google/gemini-2.5-flash`). Rápido.
- `aihubmix/ox-alpha` — após a key, respondeu diretamente (`model_used: aihubmix/ox-alpha`).

---

## 3. Correção proposta (NÃO aplicada — aguarda decisão)

**A. Frontend `ChatScreen.tsx`:**
- Remover/tornar cooperativo o `if (loadingRef.current) return;` no handler `visibilitychange` (744):
  mesmo com envio ativo, buscar `/chat/job?conv_id` e coletar resultado se `done`.
- No polling do `send()`, checar `document.visibilityState` no loop: ao voltar a ficar `visible`,
  fetch imediato (em vez de esperar o `setTimeout` throttled).

**B. Backend (defensivo, opcional):** job é em memória (`chatJobs` map) — morre no restart. Persistir
em banco ou usar histórico `conv_messages` como fallback (o frontend já pode usar
`GET /conversations/...`).

**C. AIHubMix:** key agora válida → nada a remover do pool. Manter observação: `activeModel` atual é
`aihubmix/ox-alpha`.

---

## Arquivos/estado
- `.env` → `AIHUBMIX_API_KEY` preenchida (51 chars). Backups: `*.bak_20260901_095830_aihubmix_key`,
  `*.bak_20260901_100218_aihubmix_key2`.
- `activeModel` (app_settings) = `aihubmix/ox-alpha`.
- Nada commitado (`.env` não é versionado).

## Segurança
- Chaves nunca ecoadas no terminal (só comprimento/prefixo mascarados); orientado `clear` no ttyd.
- Backups do `.env` não são rastreados pelo git.

## Pendências
- Aplicar correção do bug de chat (item 3A) quando aprovado.
- Decidir `activeModel` padrão (ox-alpha vs minimax-m3-free) para o picker.
- Commit/push do trabalho de catálogo frontend anterior (SHOW_PAID_MODELS, commit 6705ee3) — pendente.