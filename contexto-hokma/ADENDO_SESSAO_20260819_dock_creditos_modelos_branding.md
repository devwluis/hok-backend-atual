# ADENDO — SESSÃO 19/08/2026 — DOCK, CRÉDITOS, MODELOS, ENGINES E BRANDING

Sessão completa do dia 19/08/2026 no frontend **Hok OS** (`/root/hokma-web/artifacts/hok-os` → `devwluis/hok-frontend-atual`) e backend (`/root/hokma/backend` → `devwluis/hok-backend-atual`).

## 1. DOCK FLUTUANTE — restauração e redesign
- **Causa raiz do desaparecimento:** o composer do chat (`z-50`, fundo opaco + blur) cobria o Dock (`absolute bottom-0 z-30`).
- **Correção:** Dock agora `fixed bottom-4 left-1/2 -translate-x-1/2 z-[100]`, acima de tudo; pílula `bg-zinc-900/90 backdrop-blur-md rounded-full`, itens 4 abas: **Chat · Terminal · N8N · Config** (ícones `hok-*.png?v=2` restaurados em `public/icons/` — estavam faltando e causando 404). Ativo = cápsula âmbar + anel + dot centralizado.
- **Espaçamentos de segurança:** chat (composer) `pb-[calc(env(safe-area-inset-bottom)+128px)]`; lista de mensagens `pb-28`; terminal `pb-36` + `overflow-y-auto` + `scrollToBottom()` em toda saída/conexão/resize — prompt/cursor sempre visível acima do Dock.

## 2. PAINEL FINANCEIRO (Settings) — cards de créditos
- **Grid 3 colunas** nos cards OpenRouter e OpenCode Go: `Total carregado` (fundo neutro) · `Gasto até o momento` (`bg-red-500/10 border-red-500/20 text-red-400`) · `Saldo atual` (`bg-emerald-500/10 border-emerald-500/20 text-emerald-400`).
- **Cálculo corrigido (regra):** se a API retorna `Saldo` → `Gasto = Total − Saldo` (autoritativo). Se retorna `Total`+`Gasto` → `Saldo = Total − Gasto`.
  - OpenRouter: saldo = `balance` (API) → gasto derivado; total = `total_credits ?? balance+usage_total`.
  - OpenCode Go: total = `monthly.limitDollars`, gasto = `monthly.usedDollars`, saldo = `max(0,total−gasto)`.
- **Linha de uso:** `Uso no mês: $X · semana: $Y · dia: $Z` (OpenCode: mês/5h/semana) + linha de Renovação.

## 3. CATÁLOGO DE MODELOS — seleção e atualização automática
- **Bug de seleção corrigido:** `getModel()` só procurava no cache; ao selecionar, o cache era invalidado e o label do botão `[⚡ IA]` caía em "Auto". Agora busca no cache → fallback → **sintetiza label bonito do id** (ex: `anthropic/claude-sonnet-4-5` → "Claude Sonnet 4.5") com cor do provider; `activeModelId` atualiza imediatamente no clique.
- **Atualização automática (OpenCode/OpenRouter):**
  - Backend (`models_catalog.go`): TTL do catálogo 1h → **5 min**; refresh em background a cada 5 min; novo parâmetro `?force=1` no `GET /models/catalog` (refresca na hora). Backup: `models_catalog.go.bak_20260819_181237`.
  - Frontend (`hok-models.ts`): cache com TTL de **60s**; catálogo sempre refetch ao abrir; **auto-refresh a cada 60s enquanto o catálogo estiver aberto**.
- **Teste isolado (porta 18085):** catálogo 200 com **477 modelos** (82 free / 395 pagos); `?force=1` atualiza `cachedAt` em 0.17s; 401 sem token ✓.

## 4. SELEÇÃO DE ENGINE — fontes e cores originais (guia de branding)
Aplicado no botão `[◈ ENGINE]` e no menu "Motor de processamento":
| Engine | Fonte | Estilo |
|---|---|---|
| **Hok Orquestrador** | Poppins ExtraBold Italic (local) | "Hok" `#F5A623` + " Orquestrador" branco |
| **Claude Code Terminal** | PressStart2P (local) | `#E8714A` + text-shadow `#8C3D1E` |
| **OpenCode Terminal** | VT323 (local, fallback Silkscreen) | Gradiente `#4A4A4A → #C4C4C4` |
| **Hermes** | PressStart2P (local) | Gradiente `#FFD400 → #C97A2E` |
- **Ícones removidos** do lado esquerdo dos nomes Claude Code / OpenCode / Hermes no menu (mantido só "Automático").
- Fontes baixadas e hospedadas localmente em `/assets/fonts/`: `PressStart2P-Regular.ttf`, `VT323-Regular.ttf`, `Silkscreen-Regular.ttf`, `Poppins-ExtraBoldItalic.ttf` (sem dependência externa).

## 5. BRANDING HOK-ORQUESTRADOR
- **Logo do TopBar:** pílula preta (raio 20px, `hok-title`) com **HOK** em `#F5A623` + **-ORQUESTRADOR** em branco (Poppins ExtraBold Italic, compacto 15px).
- **`index.html`:** `<title>` e metas OG/Twitter → "Hok-Orquestrador — plataforma de automação, N8N e IA"; lang `pt-BR`.
- **BrainScreen:** "Hokmá" → "Hok-Orquestrador".
- **Grep confirmado:** `Chat - Hok` / `Chat-Hok` → 0 ocorrências no source (só em bundles antigos de produção); `Hokmá` só em `.bak`.

## 6. COMMITS E DEPLOY
- **Frontend (HEAD `22c77be`):**
  - `f9b449b` cálculo financeiro (Gasto=Total−Saldo) + pb-32 composer + auto-scroll terminal
  - `d760627` dock fixo z-100 + saldo verde + pb-24 terminal
  - `4c68907` gasto vermelho + espaçamento dock + seleção IA imediata
  - `c685a9d` cards créditos grid 3 colunas + dock zinc-900 + ícones HOK
  - `43c9dc2` UX v5 unificado + Settings + hooks
  - `5834d2f` barra UX v5
  - `8f115cd` seleção de modelo refletida + catálogo TTL 60s
  - `1a8a27d` fontes/cores originais dos engines
  - `22c77be` branding 4 títulos (fontes locais)
  - **12 commits ao total aguardando push** para `devwluis/hok-frontend-atual`.
- **Backend:** `models_catalog.go` alterado (TTL 5min + force=1) — **aguardando confirmação para substituir binário e reiniciar `hokma`** (CLAUDE.md) e push para `devwluis/hok-backend-atual`.
- **Deploys validados:** backups em `/root/backups/hokos_frontend_20260819_*`; local (3002) e público (`https://app.imoveischaves.com`) sempre 200.

**Data/Hora:** 19/08/2026, ~19:40 UTC
**Status:** Frontend deployado (branding ativo); backend testado isolado — pendente restart do `hokma` + pushes.