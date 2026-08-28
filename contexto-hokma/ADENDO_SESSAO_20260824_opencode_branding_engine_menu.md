# ADENDO — SESSÃO 24/08/2026 — BRANDING OPENCODE NO SELETOR "MOTOR DE PROCESSAMENTO" (MODO CLARO)

Correção visual no menu de engines do **Hok OS** (`/root/hokma-web/artifacts/hok-os` → `devwluis/hok-frontend-atual`, branch `main`).

## 1. Problema
- Na lista do seletor `[◈ ENGINE ▾]` ("Motor de processamento"), o item **OpenCode Terminal** aparecia com texto cinza genérico, sem identidade visual, misturado ao fundo claro do popover (o gradiente da marca perdia o contraste sem fundo escuro).

## 2. Correção aplicada (só no item opencode; Hok/Claude/Hermes intocados)
- **`src/index.css`** — nova classe `.engine-chip-opencode`: fonte pixel blocada retrô **Silkscreen** (local, fallback VT323) com gradiente cinza→branco (`#C4C4C4 → #FFFFFF`) via background-clip:text, replicando os blocos pixelados do logo oficial.
- **`ChatScreen.tsx`** (~linha 1211) — no mapa de itens do menu, ramo dedicado ao `opt.id === "opencode"`: wrapper pílula **fundo preto** (`bg-black rounded-md px-2 py-1` + hairline branco 8%), mesmo padrão do chip já existente no item Hok Orquestrador.
- Botão colapsado `[◈ ENGINE]`, estado selecionado (✔) e demais engines permanecem como estavam.

## 3. Fluxo executado
- **Backups:** `src/components/screens/ChatScreen.tsx.bak_20260824_033430` e `src/index.css.bak_20260824_033430`; snapshot do deploy anterior em `/root/backups/hokos_frontend_20260824_*`.
- **Build:** `typecheck` OK; `PORT=3002 BASE_PATH=/ NODE_ENV=production npm run build` → `dist/public`.
- **Deploy estático:** publicado em `/var/www/hok-os` (nginx porta 3002). Validações: local HTTPS 200 · público `https://app.imoveischaves.com` 200 · fonte `/assets/fonts/Silkscreen-Regular.ttf` 200 · classe `.engine-chip-opencode` presente no CSS e JS servidos.
- **Commit/push:** `3a79598` — "fix(engine-menu): item OpenCode Terminal com identidade oficial da marca…" → `origin/main` (`devwluis/hok-frontend-atual`).

## 4. Notas
- Fontes seguem hospedadas localmente em `public/assets/fonts/` (sem dependência externa).
- Em modo escuro o chip também funciona: preto puro + hairline destacam o item dentro do popover escuro.

**Data/Hora:** 24/08/2026, ~03:40 UTC
**Status:** Deployado e validado; commit `3a79598` pushed.
