# ADENDO — REDESIGN UX V5 UNIFICADO + SETTINGS + ENGINE OPENCODE — 19/08/2026

## 1. CHAT (UX v5 unificado) — frontend
- **Barra de input:**
  - Lado esquerdo: 2 seletores compactos `hok-console-button` (flex-1):
    - `[⚡ IA ▾]` (`button-model-selector`) — abre o "Catálogo de IA" (`#model-menu`): busca no topo (`input-model-search`), grupos PAGO / FREE por provedor / OpenCode Zen (tag esmeralda FREE), erro com "Tentar novamente", skeleton de carregamento.
    - `[◈ ENGINE ▾]` (`button-engine-selector`) — menu "Motor de processamento" (`button-mode-picker-close`): Automático (recomendado) · Hok Orquestrador · Claude Code Terminal · OpenCode Terminal · Hermes. Seleção persistida via `hokma.engine.v1` (migração `claude_code`→`claude`). Rosa quando forçado, âmbar no automático.
  - Menu `[+]` (`button-open-attachments`, 2×2): **Foto · Arquivo · Busca Web · Debug** — agora agrupa os 4 itens (Web/Debug saíram do globo).
  - Lado direito: apenas textarea + mic (`button-microphone`) + send/stop (`button-stop`/`button-send`).
  - Removidos: botões soltos `Planejar/Build`, pill N8N manual (auto-detect mantida via `n8n-expert`), pill "Modelos", globo de ferramentas, indicadores `web:on`/`debug:on`.
- **Dock inferior:** removida a aba "Modelos" — ficam 4 abas: Chat · Terminal · N8N · Settings.

## 2. SETTINGS — frontend
- **Limpeza de chaves:** removidos os campos DeepSeek/Gemini/OpenAI/Groq/Anthropic (server-side). Restam: Server URL, HOK_TOKEN e OpenRouter (necessária ao painel de créditos).
- **Card OpenRouter** (novo layout): header com botão de refresh (`button-credits-refresh`, spin ao carregar); linhas Saldo / **Gasto este mês (fundo `bg-red-500/10` + texto destrutivo)** / Total comprado / Limite / Restante.
- **Card Assinatura OpenCode Go** (novo, abaixo do OpenRouter): header com Zap âmbar + refresh (`button-opencode-refresh`); Status (badge verde "Ativo"), **Uso no Mês (fundo vermelho suave: % + $usado/$limite)**, Uso (5h), Uso (semana), Renovação (pt-BR).
- **Hooks novos:** `use-openrouter-credits.ts` e `use-opencode-status.ts` (lêem Server URL/HOK_TOKEN do `hokma.settings.v1` e fazem fetch com `X-Hok-Token`).

## 3. BACKEND (Go)
- **Repasse de Engine:** já existente — `types.go` (ForceClaudeCode/ForceHermes/ForceOpenCode) + `smart_chat.go` (rotas de decisão `forceOpenCode`/`isOpenCodeTask`). O frontend envia `forceClaudeCode`/`forceHermes`/`forceOpenCode` conforme o seletor; sem mudança necessária.
- **Endpoint novo:** `GET /opencode/status` (`opencode_status.go`, rota em `main.go`):
  - Chave: env `OPENCODE_API_KEY` → fallback `~/.local/share/opencode/auth.json` (objeto `{type,key}`; campos `opencode-go` → `opencode`).
  - Chama `https://opencode.ai/zen/go/v1/usage` (Bearer, timeout 15s).
  - Limites do plano: 5h=$12 · semanal=$30 · mensal=$60 (hardcoded) — enriquece cada janela com `limitDollars`/`usedDollars`.
  - Erros: sem chave→500; 401→502 `{"code":"invalid_key"}`; 404/sem janelas→502 `{"code":"no_subscription"}`.
  - Resposta real validada: `{status,plan:"go",subscribed,rolling,weekly,monthly}` com `status/percent/resetsAt/limitDollars/usedDollars`.
- **Teste isolado** (`hokma_test_new`, porta 18083/18084, tokens via env do processo de produção sem exibição):
  - 401 sem `X-Hok-Token` ✓
  - Dados reais completos ✓
  - `OPENCODE_API_KEY=sk-teste-invalida` → 502 `invalid_key` ✓

## 4. DEPLOY
- **Frontend:** build `index-Dw08zfDi.js` + `index-BHhceIKh.css`; backup em `/root/backups/hokos_frontend_20260819_v5unificado/`; copiado para `/var/www/hok-os` (root:root, dirs 755, files 644); validado local (3002) e público (`https://app.imoveischaves.com`) 200; bundle contém os novos testids e "Assinatura OpenCode Go" (sem "Planejar"/"Modelos").
- **Backend:** build isolado OK — **aguardando confirmação para substituir binário e reiniciar o serviço `hokma`** (CLAUDE.md).

## 5. COMMITS (aguardando confirmação de push)
- Frontend: `43c9dc2` (UX v5 unificado + Settings + hooks) sobre `5834d2f` (barra UX v5) — repo `devwluis/hok-frontend-atual` (main).
- Backend: `opencode_status.go` (novo) + `main.go` (rota) não commitados — repo `devwluis/hok-backend-atual` (main), aguardando confirmação.

**Data/Hora:** 19/08/2026, ~17:00 UTC
**Status:** Frontend deployado; backend testado isolado — pendente restart + pushes.