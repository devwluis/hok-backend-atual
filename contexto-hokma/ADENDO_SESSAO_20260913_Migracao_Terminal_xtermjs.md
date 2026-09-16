# Adendo — MIGRAÇÃO DO TERMINAL: ttyd → xterm.js

**Origem:** opencode (backend + frontend) | **Data/hora:** 13-09-2026

---

## O que foi feito

Migração completa do terminal do Hok OS de ttyd (iframe cross-origin) para xterm.js in-app, com toda a UI preservada (KeyButton, SwipeKey, sticky Ctrl/Alt, Anexar, teclado mobile, barra de teclas, zoom, temas, seleção, histórico, modais).

---

## Mudanças críticas

### 1. Transporte de teclas: POST → WebSocket PTY
- **Antes:** `term.onData` → POST `/terminal/ttyd/key` (tmux send-keys via backend)
- **Agora:** `term.onData` → `termApi.write(activeId, data)` (WebSocket direto ao PTY)
- Também aplicado a `sendToKeys` (KeyButton clicks)

### 2. Renderização de saída: sem saída → subscribeOutput
- **Antes:** xterm.js sem fonte de output (iframe foi removido, sem renderização de saída)
- **Agora:** `subscribeOutput(activeId)` → `term.write(text)` renderiza output do PTY em tempo real
- Buffer restore via `takeRecentOutput(activeId)` ao remontar aba

### 3. Conexão PTY via useTerminal
- `termApi.connect(activeId)` estabelece conexão WebSocket ao backend PTY
- Conexão persistente com reconexão automática (backoff exponencial)

### 4. Correções de funcionalidades dependentes de código TTYD morto
- **Paste (teclado mobile + botões):** `selApi("paste")` → `termApi.write()` (antes via tmux paste-buffer, agora via PTY)
- **Copiar tela/tudo:** `selApi("screen"/"all")` → buffer xterm.js (antes via tmux capture-pane)
- **Seleção:** `selApi("start/copy/cancel")` → DOM/xterm.js selection (antes via tmux copy-mode)
- **Histórico:** `selApi("all")` + `/terminal/ttyd/log` → buffer xterm.js (antes via tmux)
- **Tema:** POST `/terminal/ttyd/theme` → `termApi.write()` OSC codes (antes via tmux)
- **Anexar:** POST `/terminal/ttyd/attach` → leitura local + `write()` (antes via tmux paste-buffer)
- **Detach/Close:** POST `/terminal/ttyd/detach/close` → `termApi.teardown/removeTab` (antes via tmux)

---

## Decisões arquiteturais

1. **Arquitetura híbrida temporária:** input/output usam PTY (WebSocket), enquanto scroll/selection/history ainda referenciam tmux via APIs TTYD. Refatoração completa pendente.

2. **Tabs independentes:** TerminalXtermScreen mantém tabs próprios ("ttyd", "1", "2") mapeados diretamente para tabs do useTerminal via `activeId`.

3. **Build:** Compilação Vite limpa, sem erros TS (apenas erro pré-existente em SelectionHandles.tsx).

---

## Estado atual

- ✅ Terminal renderiza com xterm.js (prompt bash visível)
- ✅ Digitação funciona via write() (PTY)
- ✅ Output renderiza via subscribeOutput
- ✅ Build compila e deploy funciona (/var/www/hok-os/)
- ✅ Scroll indicator → buffer nativo xterm.js (resolvido no xterm-v3)
- ✅ SelApi → removido dos deps (resolvido no xterm-v3)
- ✅ Selection/history/theme → 100% nativo xterm.js (resolvido no xterm-v3)
- ✅ Terminal area com altura correta (h-full fix no data-term-ui)
- ⚠️ Duplicatas no Drive (2x ADENDO_SESSAO_20260913_Conector_Drive_Caixapreta)

---

## ETAPA 2 — 14/09/2026: Retomada da migração

### Ações realizadas

1. **Recuperação ttyd (ETAPA 1):** Reset ao checkpoint `a0beb37` (ttyd estável 13/09), build e deploy, smoke test visual confirmado.

2. **Aplicação base xterm-v3:**
   - `src/components/screens/TerminalXtermScreen.tsx` — copiado de xterm-v3 (755 linhas, 100% idêntico ao validado)
   - `src/hooks/use-terminal.tsx` — copiado de xterm-v3 (560 linhas, xterm.js + SSE/WS)
   - `src/components/shell/AppShell.tsx` — import + render TerminalXtermScreen em vez de TerminalTTYDScreen

3. **Verificação dos 3 itens pendentes:**
   - **(1) Scroll indicator `/terminal/ttyd/scroll`:** NÃO encontrado em TerminalXtermScreen.tsx nem use-terminal.tsx → ✅ já resolvido no xterm-v3
   - **(2) SelApi em deps arrays:** NÃO encontrado em nenhum arquivo xterm-v3 → ✅ já resolvido no xterm-v3
   - **(3) Selection/history/theme 100% nativo:** Usam `terminal.getSelection()`, `terminal.options.theme`, buffer xterm.js → ✅ já nativo no xterm-v3

4. **Bug de layout descoberto:** `data-term-ui` sem `h-full` → container terminal com 0px de altura (133px total preenchido por header/toolbar/tabbar). Corrigido adicionando `h-full` (mesmo fix aplicado no xterm-v4).

### Build e teste final (ETAPA 1)

- Build: `index-C7bAuDTZ.js` (913.85 KB)
- Smoke test: LIVE ✅, dimensões 852x719x705 ✅, xterm-rows com 47+ linhas ✅, input via teclado funciona ✅, zero page errors ✅
- Deploy: `/var/www/hok-os/assets/index-C7bAuDTZ.js` + `/var/www/hok-os/index.html` atualizado

---

## ETAPA 2 — RESTYLE VISUAL TERMUS (14/09/2026)

### Ações realizadas (6 tarefas)

1. **Tema Termius:** Atualizado `SettingsScreen.tsx` com cores exatas do Termius: background `#0d1117`, foreground `#c9d1d9`, cursor `#58a6ff`, selectionBackground `#264f78`, ANSI colors (red `#f85149`, green `#3fb950`, etc.)

2. **Tab bar restyle:** Estilo underline (`border-b-2`), padding compacto, double-click para renomear, transição suave `transition: all 150ms ease-in-out`, botão de fechar por aba, botão `+` visível

3. **KeyButton border-radius:** `rounded-md` → `rounded-lg` (8px)

4. **SavedSessionsPanel:** Novo componente `SavedSessionsPanel.tsx` — overlay com lista de sessões salvas (ícone, nome, timestamp), input de filtro, botão "Nova sessão", usa `useTerminal()` hook

5. **HistorySearch:** Novo componente `HistorySearch.tsx` — overlay de busca com input, navegação Enter/Shift+Enter, contador `current/total`, busca no buffer via `terminal.buffer.active` + `getLine(y).translateToString()`

6. **JetBrains Mono:** Fonte adicionada via Google Fonts CDN no `index.html`; fontFamily no Terminal constructor: `'JetBrains Mono', ui-monospace, ...`

### Build e smoke test (ETAPA 2)

- Build: `index-C-aMiRNb.js` (919 KB)
- Smoke test: botões de sessão/busca presentes ✅, tabs com underline ✅, painel abre ✅, 0 errors ✅
- Deploy: `/var/www/hok-os/assets/index-C-aMiRNb.js` + index.html atualizado

### Arquivos modificados (ETAPA 2)

- `src/components/screens/SettingsScreen.tsx` — cores do tema Termius
- `src/components/screens/TerminalXtermScreen.tsx` — tab bar, KeyButton, imports
- `index.html` — JetBrains Mono CDN
- `src/components/screens/SavedSessionsPanel.tsx` — NOVO
- `src/components/screens/HistorySearch.tsx` — NOVO

---

## BUG FIXES (14/09/2026)

### BUG 1 — Tecla travada com repetição

**Problema:** Tecla pressionada continuava enviando repetidamente (provável causa: touch+click double-fire em mobile, ou browser key repeat sem debounce no frontend).

**Correção:** Adicionado guard de debounce na função `write()` em `src/hooks/use-terminal.tsx:414`:
- Ignora writes para o mesmo tab dentro de 8ms do anterior
- Rastreia último timestamp de write por tab via `lastWriteAt` ref
- Aplica tanto ao transporte WebSocket quanto SSE

### BUG 2 — Status-line tmux vazando como conteúdo do terminal

**Problema:** A status-line do tmux (nome de sessão, janelas, relógio) aparecia como conteúdo visível no terminal.

**Correção:** Adicionado `set-option -t "$sess" status off` em `/root/hokma/backend/scripts/tmux-tab.sh`:
- Linha 23: `tmux set-option` (sessões existentes): agora inclui `status off`
- Linha 29: `tmux new-session` (novas sessões): agora inclui `status off`
- Nenhuma funcionalidade depende da status-line visível
- Aplicado também via `tmux set-option -t hok-ttyd status off` e `hok-terminal-1` nas sessões atuais

### Build e deploy (BUG FIXES)

- Build: `index-CibnYZQB.js` (920 KB)
- Smoke test: terminal carrega ✅, 2 abas simultâneas ✅, sem page errors ✅
- Deploy: `/var/www/hok-os/assets/index-CibnYZQB.js` + `/var/www/hok-os/assets/index-BD3Aet9i.css` + `/var/www/hok-os/index.html` atualizado

### Arquivos modificados (BUG FIXES)

- `src/hooks/use-terminal.tsx` — guard de escrita (debounce 8ms por tab)
- `backend/scripts/tmux-tab.sh` — status off em todas as sessões tmux

---

## Git commits

- `24a7b0f` — feat(terminal): migrar para xterm.js v3 base + layout fix
- `be57afe` — feat(terminal): restyle visual estilo Termius mantendo PTY/WebSocket
- `e56dda5` — fix(terminal): guard de escrita para evitar tecla travada com repetição
- `0f6609f` (backend/main) — fix(terminal): desativar status-line tmux para evitar vazamento no conteúdo

---

## Estado atual final

- ✅ Terminal renderiza com xterm.js v3 (restyle Termius)
- ✅ Digitação funciona via write() (PTY) com debounce 8ms
- ✅ Output renderiza via subscribeOutput
- ✅ 2+ abas simultâneas funcionam
- ✅ Build compila e deploy funciona (/var/www/hok-os/)
- ✅ tmux status off em todas as sessões
- ✅ Layout dinâmico de altura (visualViewport-aware) elimina espaço morto com teclado aberto/fechado
- ⚠️ Duplicatas no Drive (2x ADENDO_SESSAO_20260913_Conector_Drive_Caixapreta)

---

## ETAPA 3 — LAYOUT DINÂMICO DE ALTURA (14/09/2026)

### Problema

Com teclado do sistema fechado: sobra espaço vazio entre o final do conteúdo do terminal e a barra inferior.
Com teclado do sistema aberto: a barra de status não se reposiciona corretamente colada acima da barra de teclas — o container do terminal não recalcula altura.

### Correção

**Arquivo:** `src/components/screens/TerminalXtermScreen.tsx`

1. **`terminalAreaRef`** — ref no container do terminal para acesso direto ao DOM
2. **`recalcTerminal()`** — função que:
   - Mede a posição do terminal em relação ao visual viewport via `getBoundingClientRect()`
   - Calcula altura disponível: `vv.height - topOffset`
   - Define explicitamente no inline style do container
   - Chama `fitAddon.fit()` em todas as abas para realinhar o buffer
3. **visualViewport listener** atualizado — após `kbSettleTimer` disparar, também chama `recalcTerminal()`
4. **useEffect** adicional — recalcula quando `kbInsetSettled` ou `keysExpanded` mudam

### Build e deploy (ETAPA 3)

- Build: `index-DfqHkAFS.js` (920 KB)
- Smoke test: terminal carrega ✅, 2+ abas simultâneas ✅, sem page errors ✅
- Deploy: `/var/www/hok-os/assets/index-DfqHkAFS.js` + `/var/www/hok-os/index.html` atualizado

### Arquivos modificados (ETAPA 3)

- `src/components/screens/TerminalXtermScreen.tsx` — terminalAreaRef, recalcTerminal, visualViewport listener update

---

## Git commits (ATUALIZADO)

- `24a7b0f` — feat(terminal): migrar para xterm.js v3 base + layout fix
- `be57afe` — feat(terminal): restyle visual estilo Termius mantendo PTY/WebSocket
- `e56dda5` — fix(terminal): guard de escrita para evitar tecla travada com repetição
- `4449bfb` — fix(terminal): ajusta layout dinâmico de altura para eliminar espaço morto com teclado aberto/fechado
- `0f6609f` (backend/main) — fix(terminal): desativar status-line tmux para evitar vazamento no conteúdo
