# ADENDO_SESSAO_20260824 — Tremor do terminal + copiar/colar/selecionar (refeito com causa raiz)

## Reclamação do usuário (24/08)
"tremor continua" · "copy+paste+select all estilo Termius nada disso foi resolvido".

## Causas raiz encontradas (auditoria do código real)

### Tremor (2 bugs somados)
1. **`kbInsetSettled` declarado e NUNCA usado** (TerminalTTYDScreen.tsx).
   O debounce de 140ms da rodada 337b5bb foi criado mas o iframe continuava
   usando o `kbInset` BRUTO — que muda a cada frame da animação do teclado
   (~30–60 eventos resize/scroll do visualViewport). Com
   `transition: height 160ms` no iframe, a altura perseguia um alvo em
   movimento contínuo = tremor.
2. **fitNudge duplicado**: dois tremores ARTIFICIAIS de 2px (350ms + 1500ms)
   disparados no onLoad E a cada mudança de kbShift — multiplicava o problema.

### Copiar/colar/selecionar (3 bugs somados)
3. **Toast nunca renderizado**: o estado `toast` existia ("copiado ✓" /
   "falha ao copiar") mas NÃO HAVIA elemento JSX pra ele — nenhuma ação dava
   feedback visual, parecendo "nada funciona".
4. **navigator.clipboard sem fallback**: em contexto não seguro
   (http://IP-LAN, WebViews/PWA) `clipboard.writeText/readText` não existe →
   falha silenciosa. Era exatamente o cenário do dispositivo.
5. **Backend "all" frágil**: dança copy-mode/top-line/begin-selection/
   bottom-line/copy + `tmux show-buffer` (buffer GLOBAL — corrida entre abas;
   seleção zero-length nem buffer cria). Reproduzido em teste: passos com
   exit 0 e nenhum buffer novo.

## Correções

### Backend (terminal_routes.go)
- `action:"all"` → `tmux capture-pane -t <sessão> -p -S -` (histórico inteiro,
  UM comando, sem copy-mode, sem estado).
- NOVO `action:"screen"` → `capture-pane -p` (só tela visível).
- start/copy/cancel (seleção manual por setas) mantidos; paste inalterado.
- Backup: terminal_routes.go.bak_20260824_144826.

### Frontend (TerminalTTYDScreen.tsx + shell-layers.ts)
- kbShift do iframe agora usa `kbInsetSettled` (muda UMA vez, fim da animação);
  inset bruto só nos elementos flutuantes (ícone/barra colam no teclado ao vivo).
- `transition` de altura REMOVIDA do iframe (era metade do tremor).
- fitNudge: UM único nudge pós-onLoad (350ms); removido do ciclo do teclado.
- `writeClipboard()` com fallback textarea+execCommand (funciona em http:// e WebView).
- Modal de colagem manual: se leitura do clipboard for negada, abre campo com
  long-press nativo → "Inserir no terminal" (caminho à prova de permissão).
- Toolbar com chips ROTULADOS: Selecionar / Tela / Tudo / Colar (+ Reconectar,
  Maximizar). Ícones de 13px sem rótulo não eram notados.
- Modo seleção: toast-guia ("setas estendem · ⏎ copia") e ⏎ na barra confirma cópia.
- Toast RENDERIZADO (chip flutuante topo) + camada nova `terminalModal:140`
  no registro shell-layers.ts (contrato).

## Validações
- gofmt ok no arquivo editado; build isolado hokma_test OK.
- E2E :18099 (binário novo, banco descartável): screen ✓ all ✓ (61 linhas +
  marca no topo) start/cancel ✓ paste chegou na sessão ✓ key ✓ scroll info ✓
  401 sem token ✓.
- Produção pós-deploy: hokma active/health 200; smoke screen capturou tela
  viva de hok-terminal-1; all HTTP 200. nginx :3002 servindo index-Jt0s_0-h.js.

## Builds
- backend: binário trocado via hokma_test (restart autorizado pelo usuário).
- frontend: bundle index-Jt0s_0-h.js em /var/www/hok-os (backup prévio criado).

## Validação pendente no dispositivo
1. Abrir teclado dentro do terminal: SEM tremor (altura muda uma vez só).
2. Colar: botão Colar → cola direto OU abre modal (long-press → Inserir).
3. Tudo/Tela: toasts de confirmação visíveis.
4. Selecionar: setas estendem destaque, ⏎ copia, ✕ cancela.
5. Estabilidade padrão (opencode 60s / scroll 20s / troca de aba).
