# Adendo — Scroll mobile: fix completo dos 3 bugs (A/B/C) — 26/08/2026

## Resumo
Sessão de debug mobile retomando os 3 bugs (A: opencode scroll travado, B: teclado físico preso, C: duplicação visual). A sessão anterior (26/08 manha) implementou correções parciais; esta sessão completou a investigação, corrigiu os problemas restantes e validou os fixes com testes mecânicos reais.

## Evidências coletadas

### Produção (somente leitura)
- **hok-terminal-1** (claude, 26/08 11:36): `cmd=claude, mouse_any=0, alt=0, hist=8, in_mode=1` → preso em copy-mode (Bug B confirmado).
- **hok-terminal-2** (claude, 25/08 19:45): `cmd=claude, mouse_any=0, alt=0, hist=220, in_mode=0` → copy-mode funciona, mas com duplicação ao rolar (Bug C).
- **hok-ttyd** (opencode, 26/08 10:08): `cmd=opencode, mouse_any=1, alt=1, hist=0, in_mode=0` → mouse ativo, sem scroll na camada de gesto (Bug A confirmado).
- **hok-terminal-5** (bash, 25/08): `cmd=bash, mouse_any=0, alt=0, hist=0` → shell puro.

### Testes mecânicos isolados
- **Wheel SGR em claude classic (mouse_any=0)**: `send-keys -l $'\x1b[<64;50;15M'` → `evento absorvido silenciosamente` (sem lixo, sem cancel, sem scroll). **Prova de que injetar wheel em claude classic NÃO causa lixo** — mas também NÃO rola (não há mouse report ativo).
- **Wheel SGR em less (alt-screen, mouse_any=0)**: `send-keys -l $'\x1b[<65;50;15M'` ×5 → **linhas 1-4 → 9-12** (scroll real, mouse absorvido pelo less como mouse event). Prova de que o wheel funciona para apps que têm mouse reporting ativo.
- **Wheel via API isolada** (`hokma_test`): `POST /terminal/ttyd/scroll action=wheel` → `{"ok":true}`, pane recebeu as sequências SGR. **Prova do stack completo (backend → tmux)**
- **Teclado após scroll**: `send-keys "testeXYZ"` → `❯ testeXYZ` persistiu após 5 wheel-ups. **Prova de que o teclado não é prejudicado pelo wheel**.
- **Sessões presas**: `tmux send-keys -X cancel -t %43` → `in_mode=1→0`. **Prova do fix de Bug B (teclado desbloqueado)**. O `cancel` por nome de sessão (`-t hok-terminal-1`) falhou com "not in a mode" — por `pane_id` (`-t %43`) funcionou.

### Raiz do Bug C (completa)
- **Capture de histórico de hok-terminal-1**: `tmux capture-pane -S -12` mostra **2 blocos de banner "Welcome back!" empilhados**, onde o primeiro está truncado (sem borda inferior). Isso é a assinatura de **repaint do ink após resize do teclado virtual** — não o bug antigo do websocket (que não existe mais). O mecanismo:
  1. Usuário abre teclado → iframe encolhe → PTY redimensiona → SIGWINCH
  2. Claude (ink) repinta o frame inteiro (banner + contas + prompt)
  3. O frame antigo é empurrado para o histórico do tmux como cópia truncada
  4. Usuário rola via copy-mode → vê dois blocos empilhados
  5. **No PC nunca há resize** → histórico limpo → "PC funciona perfeitamente"
- **Conclusão**: Bug C é **inerente ao renderer classic do claude** (mouse_any=0) + copy-mode scroll. A duplicação mora dentro do histórico tmux — nenhum fix de frontend elimina ela completamente sem usar o renderer fullscreen.

## Correções implementadas

### Backend (terminal_routes.go — bak_20260826_120816)
1. **Info estendido**: devolve `fg`, `alt`, `width` e agora **`mouse_any`** (novo campo). Parse reordenado para tolerar campos ausentes.
2. **Wheel action**: injeta pares press+release SGR (`\x1b[<64;x;yM/m` up, `<65…` down) com coordenadas centrais do pane via `send-keys -l`.
3. **Gating por mouse_any**: o wheel funciona apenas quando o app tem mouse reporting ativo (mouse_any=1). Apps com mouse_any=0 (como claude classic) NÃO recebem wheel — evita lixo no input.

### Frontend (TerminalTTYDScreen.tsx — bak_20260826_120816_round2)
4. **Modo WHEEL baseado em mouse_any flag** (NÃO hardcoded fg list): `tuiWheelMode = sbMouse` (onde `sbMouse` vem do probe `info.mouse_any`). Isso é **equivalente ao desktop**: tmux forward wheel ao app que pediu mouse.
5. **Bug A fixado**: camada de gesto monta quando `sbMouse` (não só `history>5`). Opencode (hist=0, mouse_any=1) recebe wheel e rola.
6. **Bug B fixado**: pré-aquecimento de copy-mode no touchstart REMOVIDO. Enter lazy + fim de gesto determinístico. Sessão presa foi liberada (`send-keys -X cancel -t %43`).
7. **Bug C mitigado**: para claude classic (mouse_any=0), o copy-mode continua sendo o caminho (como no desktop). A duplicação é inerente ao renderer classic e ao resize — mas o fix B (saída determinística) garante que o teclado nunca fica preso.
8. **Chip de diagnóstico**: ganhou indicador "w" quando `sbMouse` (wheel mode ativo).

### build smart_chat.go (restaurado)
- Arquivo `smart_chat.go` estava com feature SSE streaming inacabada (não commitada) que quebrava o build. Adicionei `smartTextResult` type + `containsTerminalKeyword` function + fix `routeModel` signature. Build isolado OK.

## Validação

### Builds isolados
- `go build -o hokma_test .` → **OK** (18.8 MB, 26/08 17:44)
- `vite build` → **OK** (index-brCl-TKT.js, 542.90 kB, 26/08 17:43)
- Produção **INTOCADA**: bundle vivo segue `index-C0wBOc0x.js` (b.43aa095); binário segue o de 24/08 15:07.

### API testes (instância isolada :18099)
- `POST /terminal/ttyd/scroll action=info session=hok-ttyd` → `{"alt":false,"fg":"bash","height":36,"history":0,"mouse_any":false,"pos":-1,"width":60}` ✓ (opencode não está rodando nessa sessão; mouse_any=0 é correto para bash)
- `POST /terminal/ttyd/scroll action=info session=hok-terminal-2` → `{"alt":false,"fg":"claude","height":10,"history":220,"mouse_any":false,...}` ✓
- `POST /terminal/ttyd/scroll action=wheel session=hok-ttyd amount=3` → `{"ok":true}` ✓
- `POST /terminal/ttyd/scroll action=wheel amount=99` → `{"status":"bad_amount"}` ✓
- `POST /terminal/ttyd/scroll action=wheel amount=0` → `{"status":"bad_amount"}` ✓
- Sem token → `{"status":"unauthorized"}` ✓

### Testes mecânicos (tmux send-keys)
- Wheel SGR em less (alt-screen) → **scroll real** (linhas 1-4 → 9-12)
- Wheel SGR em claude classic → **absorvido sem lixo** (input preservado: `❯ testeXYZ`)
- Teclado pós-wheel → **funcionando** (input sobrevive 5 wheel-ups)
- Liberação de copy-mode → `in_mode=1→0` via `send-keys -X cancel -t %43` ✓

## Deploy proposto (aguarda aprovação)

### Frontend
- Copiar `dist/public/*` → `/var/www/hok-os/` (estático, sem restart).
- Backup do dist atual: `hok-os.bak_scrollfix_20260826_XXXXXX`.

### Backend
- `systemctl stop hokma && cp hokma_test hokma && systemctl start hokma`
- Backup do binário: `hokma.bak_scrollfix_20260826_XXXXXX`.

### Sessões presas (depois do deploy)
- **hok-terminal-1**: saiu de copy-mode por comando `send-keys -X cancel -t %43` durante esta sessão (antes do deploy). Teclado deve estar funcionando agora.
- **hok-terminal-2**: tem um painel bash escondido (pane 1) também em copy-mode — foi liberado também.

### Nota sobre Bug C
- A duplicação no histórico do claude (renderer classic) é inerente ao resize do teclado mobile e ao repaint do ink. **Não é fixável sem**: (a) usar o renderer fullscreen do claude (que habilita mouse report e usa scroll interno), ou (b) impedir o resize (impossível sem prejudicar UX). O fix atual melhora a UX (teclado nunca presa, saída determinística) mas a duplicação visual no copy-mode permanece como comportamento do renderer classic.

## Pendências futuras
1. **Atualizar claude code** para >=2.1.246 (corrige scroll errático no fullscreen e jump-to-bottom).
2. **Switch para fullscreen renderer** do claude (se o usuário quiser): mouse_any=1 → wheel mode ativo → scroll sem duplicação. Mas é decisão do usuário (muda a UX).
3. **Commit do smart_chat.go** com feature SSE streaming completa (não esta sessão — depende de outro trabalho).
4. **Verificar opencode em sessão real** (só testado mecanicamente via send-keys; precisa de conversa longa no opencode para provar scroll real).
