# ADENDO SESSÃO 20260825 — Engine menu: remove Automático, Hok OS, Claude Code

## Contexto
Ajustes no seletor `[◈ ENGINE ▾]` ("Motor de processamento") do Chat Web Hok OS.
Frontend: `/root/hokma-web/artifacts/hok-os` (`devwluis/hok-frontend-atual`, main).
Produção: `/var/www/hok-os` (nginx :3002).

## Parte 1 — Investigação (evidência)
"Automático" e "Hok Orquestrador" são **o mesmo caminho de roteamento**:
- `ChatScreen.tsx` (~855-857): só envia `forceClaudeCode`/`forceHermes`/`forceOpenCode` — `hok` e `auto` enviam nenhuma flag.
- `chat-stream.ts` (~178-180): corpo só serializa as 3 flags.
- `types.go:45-47`: `ClientRequest` não tem flag de "forçar Hok".
- `smart_chat.go:314-331` `classifyEngine`: com flags falsas, cascata dinâmica idêntica (security → n8n → claude_code → opencode → hermes → chat).
- `ChatScreen.tsx` (~962): o próprio frontend mapeia `forcedEngine === "hok"` → `"auto"` no ElectricCore.
Diferenças eram só cosméticas (borda rosa, chip, valor no localStorage).

## Mudanças (commit `d7a3a8c`, 2 arquivos)
1. **Parte 2:** opção `auto` removida de `ENGINE_OPTIONS`; `hok` renomeado para **"Hok OS"** (sub "padrão"). Migração: localStorage `hokma.engine.v1`/`hokma.model.selected.v1` com valor `auto` → lido como `hok` (`readForcedEngine`/`readModelSelection`). Chips "Orquestrador" → "OS" (botão fechado + menu). Fallback de `engineLabel` → "Hok OS". Import `AutomaticIcon` removido.
2. **Parte 3:** label `claude` → **"Claude Code"**; `.engine-brand-claude` 10px→**9px**, letter-spacing 1px→0.5px (`index.css`); no botão fechado a palavra "ENGINE" fica oculta ≤440px (Tailwind `max-[440px]:hidden`), mantendo `◈`. Lógica de roteamento do claude intocada.

## Validações
- `tsc --noEmit` ✓ · build isolado `PORT=3000 BASE_PATH=/` ✓ (bundle `index-GzWgX2RH.js`).
- Playwright (servidor local :8899): popover desktop/mobile com 4 itens completos, sem "Automático"; botão fechado sem truncamento — medições scrollWidth/clientWidth: Hok OS 63/63, Claude Code 105/105, opencode 79/79, Hermes 66/66 (1280px e 390px).
- Deploy: backup `/var/www/hok-os.bak_p2p3_20260825_085135` → rsync `dist/public/` → perms root:root 755/644. Nginx :3002 servindo bundle novo ("Hok OS" ×2, "recomendado" ×0) · `https://app.imoveischaves.com` → 200.
- "Automático" restante no bundle é do catálogo de IA (`ModelsScreen.tsx`, seletor ⚡ IA) e `ElectricCore.tsx` — fora de escopo.

## Backups
- `src/components/screens/ChatScreen.tsx.bak_20260825_052344` / `index.css.bak_20260825_052344` (Parte 3)
- `src/components/screens/ChatScreen.tsx.bak_20260825_083035_parte2` / `index.css.bak_20260825_083035_parte2` (Parte 2)
- `/var/www/hok-os.bak_p2p3_20260825_085135` (dist pré-deploy)

## Rollback
`cd /root/hokma-web/artifacts/hok-os && git revert d7a3a8c && PORT=3000 BASE_PATH=/ npm run build && rsync -a --delete dist/public/ /var/www/hok-os/`
(ou restaurar o backup do dist)
