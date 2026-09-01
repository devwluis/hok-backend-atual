# ADENDO — SESSÃO 01/09/2026 — AIHubMix key + bug chat/aba + fallback pago silencioso (ModelB free + transparência)

Consolidado da sessão de 01/09: configurar credencial AIHubMix, corrigir o bug de chat que
"trava ao trocar de aba" (deploy em produção), e investigar/corrigir o fallback silencioso do pool
de IA que consumiu crédito real com modelo free selecionado. Correção A aplicada em código (sem
deploy); Correção B em plano aguardando aprovação.

---

## 1. Credencial AIHubMix — preenchida e validada ✅

- `backend/.env:25` → `AIHUBMIX_API_KEY=` estava **vazia desde 27/08** (placeholder do template;
  confirmado em todos os backups: len=0).
- 1ª chave testada (`sk-po0gy...x32n`) → **401 invalid key** (sem restart).
- 2ª chave (`sk-FSbDob...6b4c`, 51 chars) → **HTTP 200** no chat `ox-alpha`. Válida.
- `systemctl restart hokma` → `aihubmix/ox-alpha` saiu de "unavailable"; chat respondeu com
  `model_used: aihubmix/ox-alpha`.
- Backups: `.env.bak_20260901_095830_aihubmix_key`, `.env.bak_20260901_100218_aihubmix_key2`.
- Observação: a listagem `/api/v1/models` da AIHubMix é **pública** (407 modelos sem key) — NÃO
  serve para validar chave (validação real = chamada de chat).

## 2. Bug "chat trava ao trocar de aba" — CORRIGIDO e em produção ✅

### Causa raiz (2 bugs frontend, ChatScreen.tsx)
1. Polling do `send()` com `setTimeout(2000)` num `for(;;)` — browsers throttlam timers de tabs
   ocultas → polling para em background.
2. Handler `visibilitychange` (linha ~744): `if (loadingRef.current) return` → durante envio ativo
   sempre true → ao voltar, nunca reanexava o polling → bolha "pensando" eterna.

### Fix aplicado (commit `c309948`, deploy em produção)
- **A**: handler `visibilitychange` agora, se há `activeJobIdRef.current`, busca o job por `id` e
  aplica a resposta se `done` (independente do `loadingRef`).
- **B**: no loop do `send()`, o `setTimeout(2000)` foi substituído por um sleep que **acorda
  imediatamente quando `document.visibilityState === "visible"`**.
- Novo ref `activeJobIdRef` (limpo no done e no finally).
- Build: `index-Cde30dgP.js` (deploy `/var/www/hok-os`). Teste via API: job completou em 3s,
  persistido em `conv_messages` (sobrevive até restart).

## 3. Fallback PAGO silencioso no pool de IA — investigado, Correção A aplicada ✅

### Problema (confirmado com dados reais)
- **ModelB (`globals.go`) = `google/gemini-2.5-flash`** — **PAGO** ($0.30/M in, $2.50/M out).
- Quando o modelo free selecionado (ex: `z-ai/glm-5.2:free`) atinge **rate limit 429**, o
  `callLLMWithFallback` (ai.go:741) cai automaticamente no ModelB SEM avisar.
- **Hoje: 8 chamadas caíram nesse fallback pago → `usage_daily = $0.0168`** (UI mostrava o modelo
  free como se tivesse respondido). Bate com o relato do usuário ($2.04 → $2.01).
- `google/gemini-2.5-flash:free` **não existe** na lista OpenRouter.

### Correção A — aplicada em código (SEM deploy)
- `globals.go`: `ModelB = "minimax/minimax-m3:free"` (pricing 0/0 confirmado na API, ctx 1M).
- `opencode_client_test.go`: `TestModelConstants` atualizado.
- `HERMES_MODEL_B` não setada no `.env` (usa default ModelB → agora free).
- Build `hokma_test` OK + testes PASS (incl. `TestModelConstants`, `TestCatalogPaidDenylist`).
- Backups: `globals.go.bak_20260901_105649_modelb_free`, `opencode_client_test.go.bak_*`.

> Observação: `agent_loop.go:24` e `autopatch_loop.go:248` têm cascatas próprias com
> `google/gemini-2.5-flash` (NÃO usam ModelB) — candidatas a limpeza futura, fora do escopo desta
> correção.

### Correção B — transparência (PLANO, aguardando aprovação)
- Backend **já retorna** `model_used` (sync `/chat/smart` + async `/chat/job`).
- LACUNA: frontend não guarda o `model_used` real em `msg.meta` (usa `selectedModel`), e
  `applyResult` faz `setActiveModelId(modelUsed)` **sem avisar**.
- Proposta: badge discreto "respondido por: X (modelo selecionado indisponível)" quando
  `model_used` ≠ selecionado. Aguardando aprovação da opção visual (bloco vs inline).

## 4. Investigações (rastreabilidade / limitações)

- **Lacuna de diagnóstico**: `APIResponse` (types.go:15) e `ChatResponse` (ai.go:836) **não capturam
  o `id` da generation OpenRouter nem `usage`** — sem isso não dá para consultar
  `GET /generation?id=` retroativamente e isolar custo por chamada. Sugestão: adicionar `id`/`usage`
  na resposta capturada + log (próxima sessão).
- **Correlação confirmada**: mensagens `teste diag travar aba` (09:30) e `melancia` (10:02) caíram
  no fallback pago; `usage_monthly = weekly = daily = 0.0168` (todo o gasto é de hoje).

## Arquivos/estado
- `.env` → AIHUBMIX_API_KEY preenchida. Backups: 3 (`*_aihubmix_key`, `*_aihubmix_key2`, `*_modelb_free`).
- Código corrigido (sem deploy): `globals.go`, `opencode_client_test.go`.
- Deploy em produção: fix de visibilidade do chat (`index-Cde30dgP.js`).
- `activeModel` = `aihubmix/ox-alpha` (validado).
- Commits: `c309948` (visibilidade) — push pendente.

## Segurança
- Chaves nunca ecoadas (só comprimento/prefixo mascarados); orientado `clear` no ttyd.

## Pendências
- **Correção B**: aprovar opção visual + implementar badge no ChatScreen (frontend).
- **Deploy da Correção A** (ModelB free): build `hokma_test` pronto, aguardando restart+deploy aprovado.
- **Lacuna de rastreio**: adicionar `id`/`usage` da generation no backend (diagnóstico futuro).
- Cascatas próprias com `google/gemini-2.5-flash` (agent_loop.go, autopatch_loop.go) — limpeza futura.
- Push pendente do trabalho de frontend (`6705ee3`, `c309948`).