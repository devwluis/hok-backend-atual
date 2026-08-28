# Adendo — 27/08 03:30 · Bug do terminal web mobile (segundo toque trava) + bundle quebrado

## 1. Tela preta no terminal (causa raiz, confirmada)
O bundle de produção `index-Ccgql2MH.js` (27/08 00:27) está **quebrado** — o device do usuário
abre o terminal com tela preta.

- `npx tsc --noEmit` → `TS2552: Cannot find name 'iframeSrc'` em
  `TerminalTTYDScreen.tsx:1574`.
- O `useMemo` que define `iframeSrc` foi colocado **dentro** do `.map()` das abas (linha ~1373) —
  hook inválido + variável fora de escopo. No bundle minificado, `src:iframeSrc` é uma
  **variável livre indefinida** (0 definições) → o iframe nunca conecta.
- **Outro crash que matava o render antes de chegar ao iframe**: o efeito de auto-post do
  buffer de diagnóstico (90s) referenciava `activeSession` no array de deps do `useEffect`
  **antes** do `const activeSession = ...` (linha ~611) → `ReferenceError: Cannot access
  'activeSession' before initialization` na render → React desmonta a árvore → tela preta.

## 2. Correção (aprovada e publicada)
- `TerminalTTYDScreen.tsx.bak_20260827_0340_blackfix`
- Movido `const iframeSrc = useMemo(...)` para o **corpo do componente** (antes do `return`).
- Removido o `useMemo` do interior do `.map()` das abas.
- Movido o efeito de auto-post de diag para **depois** da declaração de `activeSession`.
- `useRef<() => Promise<void>>()` → `useRef<... | undefined>(undefined)` (TS2554).
- `mouse_any` adicionado ao tipo de retorno de `sbApi`.
- `tsc --noEmit` → **0 erros**; `vite build` → `index-vjXxFCLZ.js` (548178B).
- Verificado no bundle: `src:Ya` com `Ya = b.useMemo(() => \`${m.current ?? n}&arg=...\`, [n, Mt, S])`
  no escopo certo; map das abas sem hook; auto-post após `yt` (activeSession).
- Deploy: `dist/public/*` → `/var/www/hok-os/` (estático, sem restart). `nginx reload` OK.
  `index.html` aponta para `index-vjXxFCLZ.js`. Antigo renomeado `.bak_20260827_0340`.

## 3. Bug do segundo toque — investigação em andamento (sem fix aplicado)
- **Evidência reproduzida:** opencode entra em spin de 100% CPU ao receber ~4KB+ de input em um
  único burst e **sai sozinho com exit status 0** (strace do tmux server:
  `SIGCHLD CLD_EXITED si_status=0 si_utime=5370` + `wait4 WEXITSTATUS==0`). Isso reproduz
  exatamente a assinatura "fill morre com exit code 0 sem explicação" das sessões anteriores.
  Threshold: 200 chars OK, 1KB busy, 4KB spin ~2,5min → exit 0.
- **Evidência de upstream:** opencode tem issues conhecidas #2115 (paste congela), #38932
  (5000+ chars → hang irreversível), #33399 (100% CPU + teclado morto, bug de renderer OpenTUI).
- **Evidência de produção (01:03–01:09):** loop de WS conecta/desconecta a cada 2–4s; tokens
  re-emitidos em 01:05:18/01:05:44/01:06:01; fill saiu com exit 1 às 11:07 (sessão inexistente,
  recriada só às 01:08:59).
- **Instrumentação ativa** (sem perturbar o usuário): strace no ttyd (3523974), strace -f no
  bash do pane (3529701), strace -f no servidor tmux (3529700), hooks tmux
  `pane-exited`/`client-detached`. Arquivos em `/tmp/opencode/traces/`.
- **Faltando:** reprodução real no celular para capturar os bytes exatos do segundo toque e
  quem fecha a conexão primeiro. Hipótese atual: input de multi-KB (recomposição da IME /
  paste / re-render do prompt com linha longa) → layout patológico do OpenTUI → spin →
  exit 0 → pane morre → sessão morre → fill exit 0 → WS cai → "Press ↵ to Reconnect".

## Pendências
- Commit/push no branch `hok-backend-atual` (aguardando confirmação).
- Reprodução no celular para fechar o bug do segundo toque.