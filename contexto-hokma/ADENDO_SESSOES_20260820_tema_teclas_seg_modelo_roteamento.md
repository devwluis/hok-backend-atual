# ADENDO — SESSÕES 19-20/08/2026 — TEMA, TELEFONE, SEGURANÇA, MODELO E ROTEAMENTO DO CHAT

Sessões após o `ADENDO_SESSAO_20260819_dock_creditos_modelos_branding.md`. Frontend em `/root/hokma-web/artifacts/hok-os` (repo `devwluis/hok-frontend-atual`), backend em `/root/hokma/backend` (repo `devwluis/hok-backend-atual`), produção `/var/www/hok-os` + serviço `hokma` (porta 8082).

## 1. FRONTEND — FIX MODO CLARO (commit `da03555`)
- **Header:** título `HOK-ORQUESTRADOR` → **`HOK OS`** (mesma pílula preta, "HOK" âmbar `#F5A623` + " OS" branco, Poppins ExtraBold Italic); `<title>`/metas OG/Twitter → "HOK OS — plataforma de automação, N8N e IA"; BrainScreen atualizado.
- **Dropdown "Motor de Processamento":** item **Hok** agora com **chip preto** (`bg-black`) — "Hok" âmbar + "Orquestrador" branco + "(padrão)" branco/70 — legível nos 2 temas; demais itens intocados.
- **Dock flutuante:** fundo `bg-zinc-900/90 border-white/10` (hardcoded) → **`bg-popover/90 border-border`** (segue claro/escuro); highlight âmbar + dot ativo mantidos.
- Validação Playwright mobile 390×844 (light+dark): 6/6 (1 falso negativo de regex `rgb` vs `oklab` — bg claro confirmado).

## 2. FRONTEND — BARRA DE TECLAS ESPECIAIS NO TERMINAL (commit `38984dd`)
- TerminalScreen ganhou barra compacta (1 linha ~32px) ancorada **acima do teclado virtual** (`visualViewport` → `bottom = kbInset + 8`; sem teclado → acima do Dock `bottom = 116`).
- Teclas: **Ctrl · Alt (sticky)** · Esc · Tab · Ctrl+C · Ctrl+D · setas ↑↓←→.
- **Modificador sticky:** tocar Ctrl/Alt destaca (armado); a próxima tecla do teclado do sistema vira `Ctrl+<tecla>` (código de controle) / `Alt+<tecla>` (`ESC+<tecla>`) e desarma.
- **Refocus da textarea do xterm** após qualquer tecla da barra (senão o toque roubava o foco e fechava o teclado).
- Sequências: `\x03` Ctrl+C, `\x04` Ctrl+D, `\x1b` Esc, `\x09` Tab, `\x1b[A/B/C/D` setas.
- **Bug de sobreposição corrigido:** o Dock (`z-100`) cobria as teclas do meio da barra (`z-40`) — resolvido posicionando a barra acima do dock quando não há teclado.
- Testes Playwright mobile: **9/9 sequências** + funcionais no shell real (`sleep 30`+Ctrl+C interrompeu em 673ms; `whoam`+Tab→`whoami`; `↑` histórico).

## 3. BACKEND — COMANDOS SOMENTE-LEITURA SEM GATE (commit `9b41b7f`)
- **Novo `cmdclassify.go`:** `isReadOnlyCommand(cmd)` com **allowlist fail-safe** — `ls cat pwd whoami uptime df free uname grep ps netstat ss`; `git log/status/diff/show` (resto → gate); `find` sem `-delete/-exec/...`; `systemctl status` (resto → gate); `top -bn1`; `curl` GET/HEAD só hosts internos, sem payload/upload.
- **Guarda em profundidade:** sem `; && || > >> < \` $(` `|`; sem token de binário de escrita (`rm mv cp chmod sudo git docker echo...`); sem caminho sensível (`containsSecretPath`); skills multi-linha exigem TODAS as linhas read-only; prompts NL extraem comandos e exigem ≥1 + todos read-only.
- **Aplicado nos 4 motores:** task_agent (skills `/task` + `trySkillForMessage`), agent_loop n8n (bash_exec read-only executa imediato e continua o loop), Claude Code/OpenCode (fast path via `promptContainsOnlyReadOnlyCommands`). **Hermes:** já roda em sandbox docker sem gate (sem mudança).
- **Log dedicado:** tabela **`command_execution_log`** (tenant_id, source, cmd, output_len, status) — fora de `self_modifications`.
- Testes: unitários + integração executor/log + suíte completa PASS; smoke 18086 OK.

## 4. BACKEND — UNIFICAÇÃO DE MODELO NOS 4 MOTORES (commit `0465aa0`)
- **Auditoria (Etapa 1):** a arquitetura de modelo único global **já existia** — `activeModel` (ai.go) persistido em `app_settings`, `setActiveModel`/`/models/select` → `propagateActiveModelToMotors` (escreve `~/.claude/settings.json` + `~/.opencode/opencode.json`). Claude Code, Hermes, OpenCode e Hok-chat **já** usavam `getActiveModel()`.
- **Único gap:** `RunAgentLoop` (orquestrador n8n/Hok) usava `MINIMAX_AGENT_MODEL`/`minimax-m3` fixo.
- **Etapa 2:** orquestrador passa a usar `getActiveModel()` com **fallback seguro** para `minimax-m3` → `ModelB` na 1ª falha (reprocessa o passo) + mensagem clara de incompatibilidade; `MINIMAX_AGENT_MODEL` vira override de escape.
- **Log de incompatibilidade:** `logModelIncompatibility(engine, model, err)` → tabela `logs` (`model_incompat|<modelo>`, WARN, source=engine) em Claude Code, Hermes e n8n.
- Opt-in China: repassado sem lógica nova. Troca de modelo **imediata** (sem restart).
- Testes: build/vet/suíte PASS + novo `ai_model_incompat_test.go`; smoke 18087.
- ⚠️ **Efeito colateral do smoke corrigido:** o `/models/select` do teste sobrescreveu o modelo de produção (`memory.db` + configs claude/opencode) → **restaurado para `deepseek-v4-flash`** (estado anterior).

## 5. BACKEND — BUG: CHAT INTERPRETAVA TEXTO COLADO COMO COMANDO BASH/READ_FILE (commit `87a98f3`)
- **Causa raiz (2 heurísticas pré-modelo):**
  - `isSelfModCommand` (substring `ls/cat/git/...` + `backend/frontend/root` em qualquer posição) → texto markdown colado virava "Comando de automodificação detectado" com o **texto inteiro como script bash** (erro de sintaxe).
  - Forcing de `read_file` no passo 1 do agent loop (substring `"leia o arquivo"/"mostra o conteudo"`) → "Vou executar a ação 'read_file'" (args arbitrários, ex: main.go), disparado quando o texto colado rotava via `containsN8nKeyword` (ambiente/node/workflow/...).
- **Correção:** `isSelfModCommand` agora **só dispara para comando shell literal de 1 linha** (sem `\n`, sem markdown `# ** Contexto: - [ * |`, sem pontuação de prosa `!?:()—`, `$ ` opcional, binário conhecido, contexto de projeto); `shouldForceReadFile` só força para pedido **curto (≤140 chars) de 1 linha sem markdown**. Texto colado → **vai para o modelo como instrução em linguagem natural** (ação real só via tool-use do próprio modelo).
- Gate de aprovação e aba Terminal **inalterados**.
- Testes de regressão (`chat_misroute_test.go`): markdown longo → false; prosa 1 linha → false; comandos literais → true. Suíte completa PASS; smoke 18088 OK.

## 6. DEPLOY / ESTADO ATUAL
- **Frontend:** deployado/validado (último bundle da sessão de teclas: `index-CV7OIFrO.js` + `index-CzuooERs.css`); commit `38984dd` pushado. Commits do modo claro (`da03555`) e teclas pushados.
- **Backend:** múltiplos restarts validados; HEAD `87a98f3` pushado; commits: `e05437f` (openode/catalog TTL), `9b41b7f` (read-only sem gate), `0465aa0` (modelo unificado), `87a98f3` (fix roteamento chat). Serviço `hokma` ativo, catalog 200, `/opencode/status` 200, modelo ativo `deepseek-v4-flash`.
- **Testes pendentes do usuário:** repetir os 2 casos que travavam (prompt de teclas do terminal e teste de modelo unificado) no Chat para confirmar a correção na prática.

**Data/Hora:** 20/08/2026, ~04:15 UTC
**Status:** Todos os deploys aplicados e validados; pendente re-teste manual dos 2 casos de roteamento do chat.