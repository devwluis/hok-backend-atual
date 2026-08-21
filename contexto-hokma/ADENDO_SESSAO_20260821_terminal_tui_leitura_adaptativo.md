# ADENDO — SESSÃO 21/08/2026 (PARTE 2) — TERMINAL: MODO LEITURA DA CONVERSA (TUI), MODO ADAPTATIVO E ROLAGEM MANUAL

Sessão após o `ADENDO_SESSAO_20260821_terminal_accessory_view.md`. Frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), backend em `/root/hokma/backend` (repo `devwluis/hok-backend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082), nginx :3002.

## 1. PROBLEMAS RELATADOS (após a accessory view)
1. **Rolagem não funcionava nas "conversas"**: dentro do OpenCode/Claude Code (TUI), a
   rolagem (swipe/scrollbar) não rolava — não dava para ler todo o contexto/conversa.
2. **Faltava modo visual adaptativo**: com o OpenCode/Claude Code abertos, o terminal
   não se adaptava (diálogos cortados, cenários não visíveis).
3. **Rolagem automática indesejada**: ao rolar para cima até o início da conversa, o
   terminal descia sozinho (o `onOutput` fazia `scrollToBottom()` incondicional).

## 2. DESCOBERTA TÉCNICA IMPORTANTE — OpenCode NÃO usa alternate screen
Captura do stream real do OpenCode no pty (via WebSocket):
- **Nenhum `\x1b[?1049h`** (smcup) — o OpenCode desenha TUDO no buffer principal com
  **cup (`\x1b[<lin>;<col>H`) + SGR**, SEM `\n`/`\r` no stream.
- Os redraws SOBRESCREVEM as mesmas células → o scrollback do xterm nunca acumula →
  a rolagem por scrollback é impossível durante a TUI.
- O OpenCode negocia o **kitty keyboard protocol** (`\x1b[?2026h`) mas o DESLIGA logo
  depois (`?2026l`) — não serve como detector confiável de TUI.

## 3. SOLUÇÕES IMPLEMENTADAS (TerminalScreen.tsx — commits `7c90b41` + `54961e6`)
1. **Modo leitura** (botão 📄 no header): overlay full-screen com o **texto limpo de
   tudo** que passou pelo pty. Como o stream bruto da TUI é ilegível (cup sem `\n`),
   o modo leitura captura **snapshots do RENDER** (`.xterm-rows` innerText) a cada
   800ms, com dedup (frames repetidos/spinner ignorados) e filtro de ruído
   (caracteres Braille/box-drawing/blocos com >60% de proporção). Scroll nativo do
   `<pre>` — funciona no touch.
2. **Detecção de TUI ativa**: cup no stream (`\x1b[\d+;\d+H`) com debounce de saída
   (2.5s sem cup → shell) + alternate screen interceptado (`?1049h/l`, `?1047h/l`,
   `?47h/l`) — o Claude Code/vi/less/htop passam a desenhar no buffer principal
   (scrollback + rolagem por swipe funcionam nelas também).
3. **Modo adaptativo**: TUI ativa + teclado virtual aberto → o container do terminal
   encolhe para `vvHeight` (área visível acima do teclado) e o resize do pty é
   disparado (debounce 350ms) — o diálogo do OpenCode cabe inteiro e o input fica à
   vista. Sem TUI ou teclado fechado → tamanho cheio (accessory view).
4. **Rolagem MANUAL**: `onOutput` só chama `scrollToBottom()` se o usuário está no
   fundo (`atBottomRef`) — output novo (spinner/redraws/job em background) NÃO
   empurra o viewport no modo histórico. Digitar qualquer tecla volta ao fundo
   (`onData`). Swipe por toque (listeners nativos `passive:false`) → `scrollLines`;
   trilho da scrollbar customizada alargado (`w-2` → `w-4`) para arrasto no touch.

## 4. VALIDAÇÃO (Playwright + OpenCode REAL no pty de teste, porta 18090)
- Modo leitura: overlay abre com a conversa legível (mensagens do OpenCode sem
  spinner/bordas), scroll nativo funciona, fechar funciona.
- Swipe no shell: rola o histórico; swipe com teclado aberto mantém a barra de teclas.
- Modo adaptativo: TUI + teclado (vv=500) → container = 500px, barra bottom=352px.
- Rolagem manual: subiu → output automático (job background) chegou SEM digitar →
  viewport PERMANECEU no histórico (badge "voltar ao live" continuou visível).
- Ctrl+C no OpenCode → volta ao shell; `typecheck` + `build` OK em todas as iterações.

## 5. DEPLOY / COMMITS
- Frontend `devwluis/hok-frontend-atual` @ main: `54961e6` (+ `7c90b41` da sessão anterior).
- Backups: `/var/www/hok-os.bak_touchscroll_20260821`, `.bak_tuileitura_20260821`,
  `.bak_manualscroll_20260821`.
- Deploy atual: bundle `index-C-5SkSus.js` (rolagem manual) — nginx :3002 200.

## 6. STATUS DO USUÁRIO (21/08, validação manual no celular)
- ✅ Modo adaptativo aprovado ("perfeito, ficou bom").
- ✅ Rolagem nas conversas aprovada.
- ⚠️ **Rolagem manual "quase bom" — NÃO 100%**: o usuário relatou que a rolagem pode
  descer em algumas situações, e mencionou possível instabilidade do servidor ao ler
  (não confirmado). Pendente: re-teste manual e investigação se o problema é do
  frontend (algum auto-scroll residual) ou da estabilidade do pty/servidor.
- Próxima sessão: fases de UX inspiradas em terminais mobile (indicador de conexão,
  seleção por long-press, swipe nas teclas, temas, comandos customizáveis).

**Data/Hora:** 21/08/2026
**Status:** Deploy validado parcialmente (rolagem manual pendente de 100%); commits pushados.
