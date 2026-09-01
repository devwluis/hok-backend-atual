# ADENDO — SESSÃO 01/09/2026 (continuação) — Buffer ideal do histórico + limpeza automática (opencode.db 518MB)

Complemento do adendo `ADENDO_SESSAO_20260901_hist_dedup_scroll_chat_ttyd.md`:
melhorias de buffer do histórico do terminal e limpeza automática do opencode.db.

## Problema relatado pelo usuário
1. O histórico do terminal é grande demais para "transferir para o chat" — o buffer
   parecia pequeno; o ideal seria 10.000.
2. Perguntou sobre automatizar limpeza ao atingir certo tamanho que pese/desacelere o sistema.

## O que foi implementado

### 1. Buffer de 10.000 linhas
- Frontend (`TerminalTTYDScreen.tsx`): `max=4000` → `max=10000`.
- Backend (`terminal_routes.go`): `termLogMaxDefault` 2000 → **10000** (hard 20000).

### 2. Clipboard robusto para texto grande
- `writeClipboard` ganhou fallback extra com elemento `contenteditable` + seleção
  manual (`execCommand("copy")`), para históricos >64KB que falhavam no WebView.

### 3. Botão "Enviar p/ chat"
- Novo botão no modal de histórico (`Enviar p/ chat`, azul). Ponte via **localStorage**
  (`hokma.chat.prefill.v1`) + navegação para a tela `chat`: o ChatScreen só existe
  montado na tela chat, então um `window` event se perderia (ChatScreen desmontado no
  terminal). O ChatScreen lê o localStorage ao montar e preenche o input.

### 4. Apagar histórico corrigido (2 toques)
- **Causa raiz**: `window.confirm()` retorna `false` silenciosamente em WebView de
  celular → o DELETE nunca era chamado.
- **Correção**: confirmação inline de 2 toques (1º arma "Confirmar?", 2º executa).
- Backend: DELETE grava **marcador de limpeza** (`/var/log/hok-term/<sess>.clear`)
  com timestamp; o transcript do opencode passa a mostrar apenas mensagens criadas
  DEPOIS do marco ("continua gravando a partir de agora").

### 5. Transcript real como fonte do histórico
- `readOpenCodeTranscript()` lê o `~/.local/share/opencode/opencode.db` (tabelas
  `message`/`part`, tipo text, roles user/assistant) — conversa real na ordem, sem
  duplicação, contexto desde o início. O GET `/terminal/ttyd/log` tenta o transcript
  PRIMEIRO (mesmo sem arquivo de log) e cai no log de snapshots (dedup) como fallback.

### 6. Corte por tamanho (1MB) + auto-limpeza (>1.5MB)
- `termLogMaxBytes = 1 MiB`: o transcript retornado ao celular é cortado em 1MB
  (mantém a conversa mais recente) — protege renderização do `<pre>` e o localStorage.
- `termLogAutoClearBytes = 1.5 MiB`: se o transcript passar disso, grava automaticamente
  o marcador de limpeza (equivale ao "Apagar") — retorno sempre leve, sem ação manual.

### 7. Limpeza automática do opencode.db (sessões >7 dias)
- `openCodeDBSweepOnce()` + `openCodeDBSweepLoop()` (roda 30s após o boot e a cada 24h).
- Apaga sessões com mais de `openCodeRetentionDays` (7) que NÃO sejam a ativa
  (a mais recente em `/root/hokma`). Remove **events + event_sequence** (aggregate_id =
  session.id, sem FK à sessão) e depois a sessão (FK CASCADE remove messages/parts).
- O opencode.db tinha **518MB**; a limpeza estima liberar **~262MB** (154 sessões + 150k events).
- Fail-safe: erro ao abrir/ping do banco apenas loga e aborta; erro por sessão é ignorado.

## Arquivos alterados
- `backend/terminal_routes.go` — transcript real, marker de limpeza, buffer 10k,
  corte por bytes, auto-limpeza, sweep do opencode.db.
- `web/artifacts/hok-os/src/components/screens/TerminalTTYDScreen.tsx` — max=10000,
  clipboard robusto, botão "Enviar p/ chat", confirmação de 2 toques no "Apagar".
- `web/artifacts/hok-os/src/components/screens/ChatScreen.tsx` — leitura do prefill
  via localStorage.

## Testes (usuário, celular)
- Histórico completo desde o início (transcript) — ✓.
- Enviar p/ chat — ✓ (após correção da ponte localStorage).
- Apagar histórico — ✓ (2 toques).
- Buffer 10k / corte por tamanho — aguardando validação.

## Pendências
- Validar corte por tamanho (1MB) e auto-limpeza em produção.
- Observar o sweep do opencode.db no próximo dia (log `[term-sweep]`).
- Commit/push pendente desta etapa (estado ANTES da limpeza manual).