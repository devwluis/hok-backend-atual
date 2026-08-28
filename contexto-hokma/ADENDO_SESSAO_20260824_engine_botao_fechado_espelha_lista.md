# ADENDO — SESSÃO 24/08/2026 (PARTE 3) — BOTÃO FECHADO [◈ ENGINE] ESPELHA O ESTILO DA LISTA

Complementa as partes 1 e 2 do dia (`ADENDO_SESSAO_20260824_opencode_*`). Frontend **Hok OS** (`/root/hokma-web/artifacts/hok-os` → `devwluis/hok-frontend-atual`, branch `main`).

## 1. Problema
- O botão fechado `[◈ ENGINE]` (que mostra o motor selecionado antes de abrir a lista) ficava em texto puro cinza para **Hok Orquestrador** e **OpenCode** — os dois motores que na lista aberta têm badge preto próprio.

## 2. Mudança (`ChatScreen.tsx`, bloco button-engine-selector)
- **Hok Orquestrador:** label do botão agora envolto no mesmo chip preto da lista (`bg-black rounded-md px-1.5 py-0.5`) com "Hok" âmbar (`hok-h`) + "Orquestrador" branco (`hok-orq`).
- **OpenCode Terminal:** label substituído pelo wordmark oficial SVG em blocos pixel (`/assets/opencode-logo.svg`, h=12px) dentro do chip preto com hairline branco 8% — igual ao item da lista.
- **Claude Code / Hermes:** sem alteração (já refletem suas fontes de marca via `ENGINE_BRAND`; na lista também não usam badge preto).
- Estado "Automático" e demais comportamentos intocados.

## 3. Fluxo executado
- **Backup:** `ChatScreen.tsx.bak_20260824_035621` + snapshot pré-deploy em `/root/backups/hokos_frontend_20260824_*`.
- **Build:** typecheck OK · `PORT=3002 BASE_PATH=/ NODE_ENV=production npm run build`.
- **Deploy:** `/var/www/hok-os`. Validações: novo bundle `index-DcCc79Kg.js` com 2 ocorrências de `opencode-logo.svg` (lista + botão fechado) · público `https://app.imoveischaves.com` → 200.
- **Commit/push:** `60343b4` → `origin/main` (commit isolado; entre as partes 2 e 3 entrou commit de terceiros `5cc3ca6` — ttyd active, autor Washington — não relacionado).

**Data/Hora:** 24/08/2026, ~04:00 UTC
**Status:** Deployado e validado; commit `60343b4` pushed.
