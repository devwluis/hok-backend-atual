# ADENDO — SESSÃO 22/08/2026 — BUG 1 RESOLVIDO: TIMEOUT MUDO DO `/terminal` (TUI EM FOREGROUND) — GATE `foregroundPgrp()` + `terminal_exec_busy` [DEPLOYADO E VALIDADO AO VIVO]

Sessão após `ADENDO_SESSAO_20260822_chat_terminal_integracao.md`. Backend em `/root/hokma/backend`, produção serviço `hokma` (porta 8082).

## 1. SINTOMA
`/terminal ls -la` via chat: timeout mudo de 15s sem nenhum output na resposta, MESMO com o comando visivelmente executado no terminal web (`ls -la` + `echo ___HOK_CMD_DONE_<ts>___` apareceram na tela). Resposta do chat: `"(sem output ainda)\n\n_[comando ainda em execução, output parcial]_"`.

## 2. DIAGNÓSTICO (build instrumentado + probe de bytes)
Build temporário `[term-exec-dbg]` (log por chunk) trocado brevemente em produção autorizou capturar a evidência:
```
chunk#1 len=4095 head="\x1b[?2026h\x1b[?25l\x1b[1;6H\x1b[38;2;238;238;238m…┃…" rawLen=0 count=0
fim done=false timedOut=true chunks=2 rawLen=4433 elapsed=15.0s countFinal=1
```
Chunks eram **redraws de TUI** (synchronized output `\x1b[?2026h`, cursor hide, posicionamento absoluto, truecolor, box-drawing `┃`) — e `countFinal=1`: o marcador apareceu UMA vez só.

**Causa raiz confirmada por processo:**
```
2948038 (pgrp 2948038) /bin/bash            ← shell da sessão
2949730 (PGRP PRÓPRIO) opencode --continue  ← TUI EM FOREGROUND
```
O `tryTerminalExec` digitava no PTY com **opencode em primeiro plano** → nosso texto virou **input da TUI** (apareceu no composer dela), nunca foi executado pelo bash. Marcador contado 1× (renderizado pela TUI), `Count>=2` jamais atingido → timeout. Testes locais passavam porque usavam bash puro sem TUI — reprodução local com viewer+`ls -la` confirmou a lógica correta sob condições replicáveis (9 chunks/26.960B/count=2 em 0.04s).

## 3. FIX APLICADO (commit `351ce4e`, +13 linhas em `terminal_exec.go`)
Gate após encontrar a sessão ativa, ANTES de escrever qualquer coisa:
```go
if fg, err := foregroundPgrp(s.ptmx.Fd()); err == nil && fg != s.bashPgrp {
    log.Printf("[AUDIT] terminal_exec RECUSADO-TUI user=%s session=%s cmd=%q ts=%s", …)
    return &smartTextResult{reply: "⚠️ O terminal está com um programa em primeiro plano (ex.: opencode/vim).\nFeche-o (ou volte ao prompt do shell) e reenvie o comando.", mode: "terminal_exec_busy", engine: "terminal"}
}
```
Instrumentação `[term-exec-dbg]` removida no mesmo patch (net zero). Novo mode distinto: `terminal_exec_busy`.

## 4. VALIDAÇÃO LOCAL (6/6, PTY real)
T1 echo limpo ✓ · T2 sem sessão ✓ · T3 sleep 20 parcial ✓ · T4 blocklist ⛔ ✓ · T5 NL sem verbo não executa ✓ · **T6 TUI simulada (bashPgrp desalinhado) → recusa elegante + AUDIT RECUSADO-TUI ✓**
Build/vet/suíte completa OK.

## 5. DEPLOY + VALIDAÇÃO EM PRODUÇÃO (uso real)
- Backup: `hokma.bak_tuigate_20260822_105206`; binário deployado hash **`00932a5776b4e90c…`** (= build de `351ce4e`); health 200; boot limpo.
- **Teste ao vivo confirmado pelo usuário**: com o shell livre, `/terminal ls -la` retornou output completo e correto no chat (`total 551364`, arquivos listados). Recusa elegante esperada quando TUI ativa.
- Push: `b94bd57..351ce4e main`.

## 6. PRÓXIMOS BUGS (enfileirados pelo usuário)
- **Bug 2**: duplicação de conteúdo no terminal;
- **Bug 3**: tela em branco ao reconectar / scroll manual necessário.
Ambos provavelmente relacionados ao mesmo cenário TUI-in-tab; aguardando contexto detalhado do usuário.

**Data/Hora:** 22/08/2026 ~11:00 UTC
**Status:** Bug 1 resolvido, deployado e validado em produção com uso real; push concluído; Bugs 2 e 3 na fila.