# ADENDO — SESSÃO 22/08/2026 — REFACTOR `runSmartText`: CASCATA DE HELPERS COM PONTO ÚNICO DE SAÍDA [DEPLOYADO]

Sessão após `ADENDO_SESSAO_20260822_copia_texto_e_chat_terminal.md`. Backend em `/root/hokma/backend` (repo `devwluis/hok-backend-atual`), produção serviço `hokma` (porta 8082). Frontend não foi tocado nesta etapa.

## 1. MOTIVAÇÃO
Toda tentativa de inserir um novo engine (`terminal_exec`) diretamente em `runSmartText` quebrava a compilação — erros reais documentados na sessão anterior (syntax errors nos limites das funções seguintes; `undefined: model`; `no new variables on left side of :=`). Causa: ~16 returns precoces com tupla de 5 valores, `model` declarada só no fim da função (~linha 433), cadeia condicional profundamente aninhada.

## 2. PLANO APROVADO (pelo usuário, abordagem cascata — descartado goto)
- Struct interna `smartTextResult{reply, mode, skill, engine, modelUsed}` (campos = tupla pública 1:1);
- Cada engine virou helper-guard que retorna `*smartTextResult` quando resolve, **nil** quando "não aplicado/falhou — tenta o próximo";
- Ordem de prioridade PRESERVADA: security → n8n (2 tentativas) → skill → claude_code → opencode → hermes → fallback chat;
- `agentFailure` vira retorno explícito de `tryN8nAgent`, consumido só pelo fallback;
- `runSmartText` mantém assinatura pública IDÊNTICA (corpo = 2 linhas); `runSmartTextCascade` tem UM único return;
- Integração do terminal NÃO plugada — terreno preparado: futuro `tryTerminalExec(...)` é inserção de baixo risco na posição de prioridade desejada.

## 3. VALIDAÇÃO DE PARIDADE (mecânica, backup vs novo)
Script comparou função antiga vs região nova: **16/16 literais** de resultado presentes e correspondentes; strings de reply byte-idênticas (planejar/bloqueado/Confirma?/⚠️ fallback/Erro no chat); logs ⚠️/❌ **8/8 verbatim**; 6 gates idênticos; `setPendingAction` mesmos args/ordem (claude_code → opencode); núcleo do fallback idêntico (selectBestModel, system prompt, searchDDG, dedup hist, routeModel). Quirks preservados: `skill="DeepHat-V1-7B"` só no #1; `skill=msg` no #4; #15 com `modelUsed=""`; #14 vence agentFailure; claude plan FALHANDO cai pro OpenCode SEM pending.
Matriz dos 16 cenários + 2 críticos extras validada campo a campo.
`go build -o hokma_test .` OK · `go vet .` OK · suíte completa OK.

## 4. SMOKE COM CHAMADAS REAIS (teste temporário, removido após)
Binário de teste NÃO roda `initSQLite()` (só main chama) — primeira execução panicou em caminho que grava uso no SQLite; corrigido chamando `initSQLite()` no teste (limitação ambiental, código antigo panicsaria igual).
3 cenários via `runSmartText` direta:
- **#8 claude_code_pending**: msg destrutiva + ForceClaudeCode → gate bloqueou API (0.08s), `setPendingAction` criada e descartada no cleanup. Tupla exata: `("Vou executar via Claude Code…Confirma? (responda sim/nao)", "claude_code_pending", "", "claude_code", "")`.
- **#14 routeModel error**: binário de teste sem keys do `.env` → `"Erro no chat: API error: Missing Authentication header"`, `("…" , "error", "", "chat", "")`. Match exato.
- **Fallthrough multi-helper**: keyword de segurança + DeepHat falhou (sem keys) → security/skill/claude/opencode/hermes todos nil → fallback #14. Prova a fiação completa da cascata.
Nota honesta: #16 (chat normal com sucesso) não executável localmente por falta de keys; linha final byte-idêntica à original e caminho compartilhado com os cenários acima.

## 5. COMMIT/PUSH/DEPLOY
- Commit **`3bed50a`** pushado na main (`2556d39..3bed50a`): smart_chat.go (+171/−82) — somente essa função; `models_routes.go` alheio intocado.
- Deploy autorizado: backup **`hokma.bak_refactor_20260822_044435`**; rebuild do estado pushado; hash deployado **`2272211b46fcf547fd349b79fbd96815eb5038e874e7faad40de631ee09eeee5`** (= hokma_test do commit).
- Serviço `active (running)` desde 04:44:35 UTC; `/health` = 200; boot limpo (catalog 514 modelos, auto-healer, trigger loop).

## 6. PÓS-DEPLOY OBSERVADO (primeiros ~10 min)
- **0** panics/fatais/**closeCode=1002**;
- Terminal web reconectou sozinho pós-restart (`[term-ws] viewer conectado … procAlive=true`) — WS fix e copiar/colar intactos;
- Uma desconexão **1006** ("unexpected EOF") às 04:45:39 = queda normal de rede do browser (sessão PTY sobreviveu, `procAlive=true`; 1006 é o caso que o reattach já cobria — diferente do bug 1002);
- Sem tráfego real de chat/leads nos primeiros minutos (~01:45 BRT, madrugada) — observação passiva a continuar; smoke #16 ao vivo ocorrerá naturalmente no primeiro uso.

**Data/Hora:** 22/08/2026 ~05:00 UTC
**Status:** Refatoração deployada em produção com paridade total verificada; rollback pronto (`hokma.bak_refactor_20260822_044435`); terreno pronto para integração chat→terminal via novo helper na cascata.