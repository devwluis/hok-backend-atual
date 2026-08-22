# ADENDO — SESSÃO 21→22/08/2026 — TERMINAL MOBILE: COPIAR/SELECIONAR TEXTO [DEPLOYADO] + INTEGRAÇÃO CHAT↔TERMINAL [PARCIAL — BLOQUEIO DOCUMENTADO]

Sessão após o `ADENDO_SESSAO_20260821_FIX_WS_CLOSE_1002.md`. Frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), backend em `/root/hokma/backend` (repo `devwluis/hok-backend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082).

## 1. COPIAR/SELECIONAR TEXTO NO TERMINAL (estilo Termius) — IMPLEMENTADO E DEPLOYADO
Arquivo alterado: `src/components/screens/TerminalScreen.tsx` (+115/−23; backup `.bak_copyfix_20260821_192505`). Nenhuma alteração de PTY/WebSocket/auth.

- **Menu de long-press expandido** (o long-press de 550ms da FASE 2 já existia e selecionava a linha tocada): agora abre menu com 3 ações estilo Termius:
  - **Copiar** — `term.getSelection()` → `navigator.clipboard.writeText` (fallback `execCommand("copy")`); feedback "Copiado ✓" por 1.4s;
  - **Selecionar tudo** — `term.selectAll()` (mantém o menu aberto p/ copiar em seguida);
  - **Colar** — `navigator.clipboard.readText()` → escreve no PTY via `write(tabId)`; feedback "Colado ✓" / "Sem acesso ao clipboard".
- **Seleção parcial**: após o long-press disparar, ARRASTAR o dedo estende a seleção linha a linha (`selAnchorRowRef` como âncora; `touchmove` chama `selectLines(min,max)` em vez de rolar o buffer).
- **Botão fixo "Copiar tudo"** no header do terminal (`data-testid="term-copy-all"`): copia o scrollback INTEIRO iterando `buffer.active` com `translateToString(true)` (sem seleção visual — robusto p/ 10k linhas); ícone vira ✓ por 1.4s. Novo método no handle da aba: `copyAll(): Promise<boolean>`.
- Refatoração interna do feedback do menu: estado único `menuMsg` substitui o antigo boolean `copied`.

### Validação / Deploy
- `typecheck` + `build` OK (bundle novo `index-BUOmMpwt.js`); dev server smoke 200.
- Deploy frontend: backup `/var/www/hok-os.bak_copyfix_20260821_193415`; bundle servido 200; nginx :3002 OK.
- Commit `21f1fdc` pushado na main do `hok-frontend-atual`.
- Smoke no Android real pendente do usuário: long-press→menu, arraste estendendo seleção, Colar, botão header.

## 2. INTEGRAÇÃO CHAT ↔ TERMINAL — PLANO APROVADO, EXECUÇÃO BLOQUEADA POR ERROS REAIS DE COMPILAÇÃO
Spec aprovada pelo usuário: triggers duplos (`/terminal `/`/term ` explícito + linguagem natural), reusar a MESMA sessão PTY ativa (nunca criar nova), delimitador `___HOK_CMD_DONE___`, captura de output até o marcador, timeout 15s com nota de output parcial, strip ANSI, resposta prefixada `[Terminal]`, acesso SOMENTE admin (leads/WhatsApp bloqueados), log de auditoria de todo comando, comandos perigosos (`rm -rf`, `dd`, `curl|sh`...) bloqueados/logados.
Trade-off aceito pelo usuário: reusar a sessão manual significa que digitação simultânea no terminal web pode se entrelaçar com comandos do chat (aceitável single-user).

### Erros reais encontrados (go build, colados na sessão)
Toda tentativa de inserir um branch `terminal_exec` dentro de `runSmartText` (via sed na linha 432/433 e via inserção Python antes de `model := selectBestModel(`) quebrou consistentemente:

```
./smart_chat.go:449:6: syntax error: unexpected name extractBashCommand, expected (
./smart_chat.go:481:6: syntax error: unexpected name isReadOnlySafeCommand, expected (
./smart_chat.go:524:6: syntax error: unexpected name isSelfModCommand, expected (
./smart_chat.go:559:6: syntax error: unexpected name smartChatSystemPrompt, expected (
./smart_chat.go:589:2: syntax error: unexpected keyword if at end of statement
```

Em tentativa anterior (branch `if engine == "terminal_exec"`):
```
./smart_chat.go:478:58: syntax error: unexpected newline, expected := or = or comma
./smart_chat.go:473:8: no new variables on left side of :=
./smart_chat.go:433:2 / 454:36 / 473:2: undefined: model
```

### Causa raiz (analisada com o erro na mão)
`runSmartText` tem ~8 retornos precoces cada um com a tupla completa `(reply, mode, skill, engine, modelUsed)`; a variável `model` só é declarada no fim (`model := selectBestModel(msg)`, linha ~433); cadeia condicional profundamente aninhada. Inserções pontuais quebram os limites das funções seguintes ou o escopo de `model`.

## 3. O QUE FOI APLICADO (decisão do usuário — mudança mínima)
Após ver os erros, o usuário autorizou APENAS:
1. `containsTerminalKeyword()` mantida como função STANDALONE em `smart_chat.go` (+22 linhas, após `containsN8nKeyword`) — gatilhos `/terminal `/`/term ` + heurísticas NL ("qual o status", "roda/executa", "comando", "shell"). SEM wiring em `classifyEngine`/`runSmartText` (verificado: 0 linhas alteradas nessas funções).
2. `findActiveSession(userKey)` implementada em `terminal_session.go` (+17 linhas): busca sessão PTY VIVA do usuário no registry (`!closed`, bash `ProcessState == nil`), NÃO cria sessão, retorna nil se nenhuma.
3. `runSmartText` NÃO tocado.

Backups: `smart_chat.go.bak_termexec_20260822_035953`, `terminal_session.go.bak_termexec_20260822_035953`, `terminal_exec.go.bak_termexec_20260822_035953`.
`terminal_exec.go` permanece não-rastreado com rascunho `ExecResult`/`executeTerminalCommand` (duplicata de `findActiveSession` removida; arquivo compila mas é dead code — base para retomar a integração).

### Validação
`go build -o hokma_test .` OK · `go vet .` OK · `HOK_TOKEN=<descartável> go test ./...` → `ok hokma_backend`.

## 4. ESTADO ATUAL
- **Produção**: WS fix 1002 + recurso de copiar/colar deployados e funcionando (zero `closeCode=1002` desde o deploy). Binário/serviço hokma NÃO reiniciados nesta etapa.
- **Backend local**: alterações acima aplicadas e validadas, PORÉM **nada commitado/pushado/deployado** — aguardando autorização do usuário. `models_routes.go` continua com diff pré-existente alheio à sessão.
- **Pendente de decisão futura**: arquitetura da integração chat→terminal (candidatos: handler próprio chamado ANTES de `runSmartText` em `handleSmartChat`, evitando a função monolítica; ou refatoração controlada de `runSmartText`). Rascunho de execução preservado em `terminal_exec.go`.

**Data/Hora:** 22/08/2026 ~04:00 UTC
**Status:** Copiar/selecionar deployado (`21f1fdc`); integração chat↔terminal parcialmente aplicada (detecção + reuso de sessão prontos, captura de output pendente), build/vet/test locais verdes, aguardando autorização de commit/deploy.