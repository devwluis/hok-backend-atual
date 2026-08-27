# Adendo — Investigação do incidente do popup "Select model" + overlay reconnect (27/08)

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_INCIDENTE_20260827_terminal_popup_reconnect_trava_teclado.md (incidente relatado via Claude Web).

---

## Pergunta investigada (pedido de Washington)

"O overlay 'Press ↵ to Reconnect' tem handler de toque funcional no mobile quando
há outro elemento (popup do opencode) sobreposto na tela?"

## Resposta: NÃO existe handler de toque do React para esse overlay

1. **O overlay é INTERNO do iframe cross-origin** (renderizado pelo ttyd, não pelo
   React) — o próprio código confirma: `TerminalTTYDScreen.tsx:437-441`
   ("O overlay 'Press to Reconnect' é INTERNO do iframe cross-origin
   (inalcançável)"). O pai (React) não enxerga o elemento nem seus eventos; a
   recuperação pelo pai é feita por outro caminho (abaixo).

2. **O "handler natural" do overlay é o Enter do teclado virtual** (o ttyd espera
   Enter para reconectar o WebSocket). No incidente, o foco foi perdido na troca
   de aba e o teclado fechou — sem foco no iframe não há como mandar Enter.

3. **O "popup por cima" não é um elemento HTML sobreposto**: o popup "Select
   model" do opencode é desenhado no canvas do ttyd (mesma superfície do
   terminal). Com o WS morto, o canvas congela com o popup e o prompt de
   reconexão do ttyd fica desenhado por baixo/ao redor — a leitura visual é de
   "duas camadas", mas é uma única superfície congelada.

4. **O mecanismo real de recuperação mobile é do PAI** (`startRecovery` →
   `probeHealth` com backoff → remontagem do iframe/reattach tmux), e ele só
   roda quando `recovering=true`, disparado por:
   - evento `online`;
   - `visibilitychange` com **>10s fora** da aba (`TerminalTTYDScreen.tsx:489-490`
     — "Retornos curtos (troca de aba normal) NÃO recarregam");
   - botão manual RotateCcw na toolbar.
   
   **A troca de aba rápida (<10s) — exatamente o gatilho do incidente — NÃO
   dispara a recuperação.** O pai nunca fica sabendo que o WS caiu (cross-origin,
   sem evento), então o iframe fica congelado até o usuário fechar/reabrir a aba.

5. **O tap na tela** (mobile): existe a camada de gesto (`term-gesture`,
   `TerminalTTYDScreen.tsx:1336-1346`) que, em tap curto, faz
   `focusTerminalInput()` (refoca o iframe e reabre o teclado). PORÉM:
   - só existe quando `coarse && (sb.history > 5 || tuiWheelMode) && !recovering`;
   - mesmo focando o iframe e abrindo o teclado, o Enter não produz efeito útil
     quando o processo/pane morreu (causa raiz conhecida: burst de input →
     opencode spin 100% → exit 0 → pane morta) — o ttyd reconecta o WS, mas a
     tela volta morta/repete o ciclo. Isso explica a percepção de "teclado
     travado".

## Causa exata documentada

**Cadeia do incidente**: popup TUI "Select model" aberto → troca de aba (Terminal
→ Chat → Terminal, <10s) → processo derrubado (mesma causa raiz OpenTUI dos
adendos 27/08 03:48 e 03:30) → WS do ttyd cai → ttyd desenha "Press Enter to
reconnect" sob o canvas congelado (popup visível) → pai não detecta a queda
(sem evento de WS; `visibilitychange` <10s não dispara `startRecovery`) → tap na
tela refoca o iframe mas o processo está morto → único caminho de recuperação
real: fechar/reabrir a aba (remontagem total).

## Mitigações possíveis (NÃO implementadas — apenas registradas para decisão)

1. **Disparar `startRecovery()` em todo retorno de visibilidade** (remover o
   limite de 10s) — custo: remontagem a cada troca de aba se o servidor
   responder; barato e inofensivo se a conexão está viva (probeHealth gateia),
   mas causa churn de tela.
2. **Detectar pane morta pelo pai**: poll leve do estado da sessão ttyd no
   backend (ex.: `GET /terminal/status`) com threshold curto (ex.: 10-15s) para
   disparar recovery sem depender de visibilitychange — cobre também o caso de
   queda com o app em foreground.
3. **Botão manual mais visível**: o RotateCcw já existe, mas não é óbvio sob o
   popup; considerar um chip flutuante de "Reconectar" quando `conn=offline`
   (o hook já sinaliza estado — verificar se a aba ttyd expõe `conn=offline`).

## Escopo

- Nenhum fix aplicado (diagnóstico apenas, conforme pedido).
- Nenhum arquivo editado (somente leitura: TerminalTTYDScreen.tsx,
  use-terminal.tsx).
- Não é bloqueante nem prioridade — fora do escopo resolvido pela Fase 3
  (canal do Chat Web); o terminal manual/visível permanece nesta arquitetura
  por decisão §2.1. Revisitar quando a arquitetura do terminal visível for
  redesenhada.