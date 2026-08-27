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
---

# ATUALIZAÇÃO 27/08 13:10 UTC — Investigação controlada do spin/exit (mobile + desktop)

## Reprodução MOBILE explícita (novo registro)
Washington reproduziu o bug DE NOVO às ~13:00 UTC, desta vez EXPLICITAMENTE NO
**CELULAR (mobile, teclado virtual/IME)** — travou o terminal no padrão
documentado (spin de CPU, exit 0, sessão morre). É a primeira reprodução
registrada com contexto mobile explícito (as anteriores não distinguiam o
dispositivo). O gatilho relatado foi o mesmo padrão do "segundo toque".

## Reproduções DESKTOP controladas (hoje, mesmo binário 1.18.23, via tmux
## send-keys em sessões de teste isoladas — sem afetar sessões reais)

| # | Teste | Resultado |
|---|---|---|
| A | 200 chars ASCII, 1 burst | sem pico (CPU ~27% estável) |
| B | 4096 chars ASCII, 1 burst (sessão fresca) | **spin ~48% pico** → decaimento em ~2-3 min, **SEM morte** |
| B2 | 4096 chars ASCII, 1 burst (sessão com histórico) | sem spin (~9-19%) — **não-determinístico** |
| C | **133 bytes multi-byte** (acentos+emoji+ZWJ), 1 burst | **spin ~70% pico** → decaimento ~36s, SEM morte |
| D | 200 chunks de 1-2 chars (digitação rápida) | sem pico (~14%) |
| F | Recomposição IME (texto+30×backspace+retexto) | sem pico (~12%) |
| G | Burst multi-byte + resize do pane simultâneo | sem pico (~10%) |

## Respostas às perguntas da decisão

1. **É sempre volume de bytes?** NÃO. O spin correlaciona com BURST de
   conteúdo entregue DE UMA VEZ com caracteres de layout complexo:
   **133 bytes multi-byte (texto curto, ~60 chars) causaram spin MAIOR (70%)
   que 4KB ASCII (48%)**. Ou seja: IME/recomposição mobile com caracteres
   acentuados/wide/ZWJ em texto CURTO conta como burst — e proporcionalmente
   mais forte por byte. A hipótese é custo de LAYOUT do renderer OpenTUI por
   célula (wide/combining chars multiplicam o trabalho), não volume bruto.
   Digitação char-a-char (mesma quantidade total) NÃO causa spin.
2. **Desktop já foi testado antes?** SIM — o adendo de 27/08 03:48 documentou
   reproduções via tmux send-keys no servidor (200 chars OK / 1KB busy / 4KB
   spin ~2,5min → exit 0). **LACUNA**: hoje o exit 0 NÃO foi reproduzido em
   desktop (todos os spins se recuperaram; processo sobreviveu em todos os
   testes, incluindo 4KB monitorado por 4 min). A reprodução de hoje no
   CELULAR (morte/trava) é a mais recente e a primeira explícita em mobile —
   **não sabemos se o exit 0 é mobile-exclusivo** (teclado virtual/IME/timing
   do ttyd podem ser o fator que falta).
3. **ptrace indisponível hoje**: strace falhou com "Operation not permitted"
   em todos os processos (opencode, tmux server, ttyd) — o adendo de 03:48
   conseguiu anexar; o ambiente atual bloqueia. Não foi possível capturar os
   bytes exatos do burst via strace (lacuna). Os bytes foram inferidos dos
   arquivos de teste (ASCII/multi-byte) mas NÃO dos reais do teclado mobile.

## Estado da decisão
- Bug reproduzível (spin) em desktop controlado — com padrão não-determinístico
  e correlação com conteúdo multi-byte em burst único.
- Exit 0/morte: confirmado em produção mobile (usuário), NÃO reproduzido em
  desktop hoje. Falta capturar os bytes/timing reais do teclado virtual para
  fechar a causa exata.
- Opções para decidir (usuário): mitigação local (ex.: throttle de input no
  terminal visível) vs aguardar upstream (issues #2115, #38932, #33399).
  Nenhuma implementação feita — investigação apenas.

---

# DECISÃO 27/08 13:30 UTC — Não implementar mitigação por ora

Washington decidiu, com base na investigação controlada (spin correlaciona com
burst de layout complexo, não volume de bytes; exit 0 não reproduzido em
desktop; morte confirmada em mobile):

1. **NÃO implementar mitigação local** (opção a) neste momento.
2. **NÃO aprofundar instrumentação mobile** (opção c) agora.
3. O impacto real está limitado ao **terminal manual/visível** — fora do escopo
   que a Fase 3 já resolve (canal do Chat Web). Conviver com o bug por enquanto;
   evitar digitar acentos/emoji em burst no terminal quando possível.
4. **Condição de retomada**: se o bug virar prioridade (ex.: atrapalhar trabalho
   real com frequência), retomar com a **opção c** (instrumentação mobile —
   capturar bytes/timing reais do teclado virtual) ANTES de decidir entre
   mitigar localmente ou aguardar o upstream corrigir (#2115, #38932, #33399).

---

# CORREÇÃO DE REGISTRO 27/08 14:40 UTC — Bundle atual do fix de tela preta

A seção 2 deste adendo registrou o deploy do fix de tela preta com o bundle
`index-vjXxFCLZ.js`. **Registro desatualizado** (verificado em produção em
27/08 14:40): o bundle atualmente servido em /var/www/hok-os é
**`index-Cy5XPIhw.js`** (deploy do fix terminal_active em 27/08 12:38). O fix
de tela preta (iframeSrc/activeSession) está **incluído** nesse bundle — o
código-fonte atual não contém mais `iframeSrc` (0 ocorrências no
TerminalTTYDScreen.tsx). O `index-vjXxFCLZ.js` foi substituído pelo deploy
posterior; nenhuma regressão do fix registrada.
