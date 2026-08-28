# ADENDO — SESSÃO 24/08/2026 (PARTE 2) — OPENCODE NO MENU DE ENGINES: WORDMARK OFICIAL EM SVG

Complementa o adendo anterior do dia (`ADENDO_SESSAO_20260824_opencode_branding_engine_menu.md`). Frontend **Hok OS** (`/root/hokma-web/artifacts/hok-os` → `devwluis/hok-frontend-atual`, branch `main`).

## 1. Correção pedida
- O item "OpenCode Terminal" no menu "Motor de processamento" continuava com aparência de **fonte monoespaçada genérica** (VT323/fallback), sem os blocos pixelados do logo oficial.

## 2. Descoberta técnica (importante)
- Verificado no site oficial (`opencode.ai`): o wordmark "opencode" **NÃO é uma fonte instalável** — é um **SVG desenhado à mão em blocos quadrados de 12×12** (letra por letra, fills `#F1ECEC`/`#B7B1B1` na variante escura e `#CFCECD`/`#656363`/`#211E1E` na clara). Não existe TTF "oficial".
- Portanto a fidelidade total foi alcançada usando **o próprio SVG oficial**, extraído direto do site e hospedado localmente.

## 3. Mudanças
- **Novo arquivo:** `public/assets/opencode-logo.svg` — wordmark oficial (variante para fundo escuro: blocos cinza-claro/quase-branco), servido sem dependência externa.
- **`ChatScreen.tsx`:** ramo `opencode` do menu agora renderiza `<img src="/assets/opencode-logo.svg" alt="OpenCode Terminal" className="h-[14px] w-auto" />` dentro do chip preto (`bg-black rounded-md px-2 py-[5px]` + hairline branco 8%). Altura 14px, proporcional aos demais itens (~10–13px de texto).
- **`index.css`:** classe `.engine-chip-opencode` (tentativa de fonte Silkscreen) removida — virou código morto; comentário novo documenta que o wordmark é SVG, não fonte.
- Demais engines (Automático, Hok Orquestrador, Claude Code Terminal, Hermes) intocados. Botão colapsado `[◈ ENGINE]` intocado.

## 4. Fluxo executado
- **Backups:** `ChatScreen.tsx.bak_20260824_034820`, `index.css.bak_20260824_034820`; snapshot pré-deploy em `/root/backups/hokos_frontend_20260824_*` (segunda cópia).
- **Build:** typecheck OK; `PORT=3002 BASE_PATH=/ NODE_ENV=production npm run build`.
- **Deploy:** `/var/www/hok-os`. Validações: `/assets/opencode-logo.svg` → 200 · novo bundle `index-Dg3qImjc.js` referencia o logo · público `https://app.imoveischaves.com` → 200.
- **Commit/push:** `5f19c99` — "fix(engine-menu): item OpenCode Terminal com o WORDMARK OFICIAL…" → `origin/main`.

## 5. Notas
- Se algum dia quiserem o efeito "fonte pixel" selecionável (copiável), a alternativa mais próxima é Silkscreen (já local) — mas o visual autêntico do logo é este SVG.
- Possível cache no dispositivo: o bundle mudou de hash (`Dz32Njei`→`Dg3qImjc`); se necessário, recarregar com cache limpo.

**Data/Hora:** 24/08/2026, ~03:55 UTC
**Status:** Deployado e validado; commit `5f19c99` pushed.
