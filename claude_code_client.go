package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// claudeCodeTimeout limita a duracao do claude_code CLI por chamada.
// FIX 16/08: 120s era menor que a latencia real do modo planejar (visto
// respostas legitimas de 69s+ no claude_code) e o nginx cortava em 60s.
// Subido para 300s (5min) como margem generosa para respostas complexas.
const claudeCodeTimeout = 300 * time.Second

// FIX 20/08 (root): usuario dedicado sem privilegios para rodar o CLI claude
// no fluxo aprovado (o CLI recusa --dangerously-skip-permissions como root).
// Criado em /etc/passwd sem senha de login; ~/.claude/settings.json proprio;
// ACL de rwx nos repos de codigo; sem sudo; sem acesso aos arquivos 600
// (.env, memory.db, config/...).
const claudeAgentUser = "hokma-agent"
const runuserBin = "/usr/sbin/runuser"

// errSystemPromptLeak indica que a resposta do claude_code continha
// vazamento de system prompt/skills do SDK e foi bloqueada (fix 16/08 item B).
var errSystemPromptLeak = errors.New("claude_code: resposta com vazamento de system prompt bloqueada")

// sinais FORTES: 1 ocorrencia basta. São frases do system prompt do SDK /
// narrativa interna do modelo que nunca deveriam aparecer numa resposta útil
// (o HOK responde em PT-BR; qualquer "respond in Portuguese" é regurgitação).
var systemPromptLeakStrong = []string{
	"still-silent",
	"/dev-discord-webhook",
	"system reminder says",
	"respond in portuguese",
	"per the project instructions",
	"must already want to invoke a skill",
	"skill description here",
	"these skills are available",
	"save analysis in your own memory",
	"let me just read",
	"let me read the",
	"let me look at",
	"the user has a question",
	"i should respond",
	"use the read tool",
	"use the bash tool",
	"use the edit tool",
	"delegated agent",
	"working notes",
	"harness attaches",
	"security agent that demands",
	"secure multi-agent facility",
	"opt-in as described",
	"discussions channel",
	"prompt-injection concerns",
	"git state only",
	"chain review methodology",
	"attack-surface-reduction",
}

// sinais FRACOS: precisam de 2+ ocorrencias distintas. São termos típicos do
// system prompt / lista de skills do SDK.
var systemPromptLeakSignals = []string{
	"## Skills",
	"# Skills",
	"list of agents",
	"list of skills",
	"/agents",
	"/workflows",
	"mcp__",
	"perform_scan",
	"keychain:",
	"worktree",
	"killer-content",
	"official login flow",
	"codeofconduct:",
	"digest: produce",
	"specialized reviewers",
	"the user wants me to",
	"the user wants to",
	"let me check",
	"let me run",
	"running from the project directory",
	"in this mode by default",
}

// narrativas internas em inglês do modelo (pensamento em voz alta antes da
// resposta útil) — presente em todos os vazamentos reais observados.
var internalNarrationSignals = []string{
	"the user wants me to",
	"the user wants to",
	"let me read it",
	"let me check",
	"let me look at",
	"let me run",
	"let me just read",
	"i should respond",
	"this is a simple",
	"the user has a question",
}

// palavras típicas de narrativa interna/instruções do SDK (em inglês).
// O HOK responde em PT-BR; uma resposta com densidade alta dessas palavras
// indica regurgitação do system prompt — camada estrutural que cobre
// variações não mapeadas (o vazamento muda a cada chamada).
var agentNarrationWords = []string{
	"against", "and", "available", "based", "brief", "channel", "checklist",
	"comparing", "control", "debug", "default", "demands", "described",
	"diff", "directly", "discussions", "exit", "explicit", "explore",
	"feedback", "file", "harness", "help", "include", "instructions",
	"internet", "invocation", "invoke", "known", "list", "local", "notes",
	"operation", "operate", "passing", "patterns", "permanent", "plan-mode",
	"prior", "problem", "produce", "project", "readiness", "recent",
	"recommend", "reminder", "report", "repository", "requires", "review",
	"security", "shorthand", "simple", "skill", "skills", "staged", "task",
	"tasks", "unstaged", "user wants", "vulnerability", "working diff",
	"working", "written", "ready",
}

// sinais fortes extras observados em vazamentos reais (16/08): frases de
// "skill invocation" do SDK que nunca aparecem numa resposta normal.
var systemPromptLeakStrong2 = []string{
	"as a skill invocation",
	"skills list",
	"is available as a skill",
	"local-tasks.txt",
	"the skills list",
}

// vocabulario exclusivo do system prompt do SDK (20/08): palavras/frases em
// ingles puro que nunca aparecem numa resposta util em PT-BR (o HOK responde
// em portugues) — 1 ocorrencia basta. Cobre fragmentos curtos de vazamento
// que nao alcancariam a camada de densidade (ex: "against known vulnerability
// patterns", "production readiness"). Sem palavras que sejam emprestimos
// comuns em PT tecnico (debug, task, file, list, checklist...).
var sdkPromptLeakWords = []string{
	"vulnerability",
	"harness",
	"invocation",
	"codeflow",
	"gophish",
	"genai",
	"microcontroller",
	"impostor",
	"opt-in",
	"plan-mode",
	"working diff",
	"readiness",
	"shorthand",
	"threat model",
	"known vulnerability",
	"pending security",
}

// detectSystemPromptLeak verifica se o texto contém sinais de vazamento do
// system prompt do SDK. Estrategia em camadas:
//  1. sinais fortes: 1 basta (frases exclusivas do system prompt/narrativa)
//  2. sinais fracos + narrativa interna: 2+ distintos
//  3. estrutural: 2+ linhas no formato "- nome: descricao" (item de skill)
//  4. densidade de narrativa inglesa: 8+ ocorrências de palavras de
//     instrução/narrativa do SDK (cobre variações não mapeadas)
func detectSystemPromptLeak(text string) bool {
	lower := strings.ToLower(text)
	for _, s := range systemPromptLeakStrong {
		if strings.Contains(lower, s) {
			return true
		}
	}
	for _, s := range systemPromptLeakStrong2 {
		if strings.Contains(lower, s) {
			return true
		}
	}
	// vocabulario exclusivo do SDK em ingles (fragmentos curtos de vazamento):
	// 1 ocorrencia basta — nunca aparecem em resposta util PT-BR.
	for _, s := range sdkPromptLeakWords {
		if strings.Contains(lower, s) {
			return true
		}
	}
	hits := 0
	for _, s := range systemPromptLeakSignals {
		if strings.Contains(lower, s) {
			hits++
		}
	}
	for _, s := range internalNarrationSignals {
		if strings.Contains(lower, s) {
			hits++
		}
	}
	// FIX 16/08 (opcao A): limiar subiu de 2 para 3+ para reduzir falso
	// positivo em respostas com 1-2 sinais fracos acidentais (ex: narrativa
	// "the user wants me to" + "let me check" em comando echo simples).
	// Respostas com 3+ sinais distintos ainda sao bloqueadas (vazamento real).
	if hits >= 3 {
		return true
	}
	// estrutural: 2+ linhas "- nome: descricao" (sem espaco no nome, sem **);
	// OU 1 linha com "/" no nome (skills internas do SDK sempre tem "/",
	// ex: /agents, /workflows, /codeflow) — sinal forte de lista vazada
	skillLines := 0
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if len(line) > 4 && strings.HasPrefix(line, "- ") {
			rest := line[2:]
			idx := strings.Index(rest, ":")
			if idx > 0 && idx < 40 && !strings.Contains(rest[:idx], "*") {
				hasSpace := strings.Contains(rest[:idx], " ")
				hasSlash := strings.Contains(rest[:idx], "/")
				if hasSlash {
					return true
				}
				if !hasSpace {
					skillLines++
				}
			}
		}
		if skillLines >= 2 {
			return true
		}
	}
	// densidade de narrativa inglesa de agente. FIX 20/08: portao de densidade
// de ingles — so conta como vazamento se o texto tiver ingles DENS0
// (3+ stopwords inglesas como palavra inteira) E 5+ palavras de narrativa/
// instrucao do SDK. Sem o portao, respostas tecnicas uteis em PT-BR com
// emprestimos isolados ("default", "debug", "/fs/list", "/agent/task")
// eram bloqueadas por engano. Narracao curta de tool-use em ingles
// ("I'll list the files...") tem 1-2 stopwords e passa; regurgitacao do
// system prompt tem paragrafos ingleses densos e bloqueia.
narrationHits := 0
	for i := range agentNarrationWords {
		if agentNarrationRe[i].MatchString(lower) {
			narrationHits++
		}
	}
	return englishStopwordCount(lower) >= 3 && narrationHits >= 5
}

// englishStopwordRe: stopwords inglesas comuns (sem homografos do PT como
// "a", "as", "do", "de") — palavra inteira, case-insensitive. Resposta util
// PT-BR tem 0-1; vazamento real do SDK tem paragrafos em ingles com dezenas.
var englishStopwordRe = regexp.MustCompile(`(?i)\b(the|and|to|of|for|with|that|this|you|your|will|can|should|must|using|when|then|from|are|is|not|but|have|has|been|would|could|may|might|than|into|about|which|what|how|there|their|they|them|its|it's|on|in|at|by|or|an|if|so|we|our|out|up|over|under|again|once|here|where|why|because|until|while|be|being|does|did|against|both)\b`)

func englishStopwordCount(lower string) int {
	return len(englishStopwordRe.FindAllStringIndex(lower, -1))
}

// agentNarrationRe: regexes de palavra inteira (case-insensitive) para a
// camada de densidade de narrativa inglesa — compiladas uma vez no init.
var agentNarrationRe = func() []*regexp.Regexp {
	re := make([]*regexp.Regexp, len(agentNarrationWords))
	for i, w := range agentNarrationWords {
		re[i] = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(w) + `\b`)
	}
	return re
}()

func isClaudeCodeTask(msg string) bool {
	keywords := []string{
		"edite o arquivo", "edita o arquivo", "editar arquivo",
		"leia o arquivo", "leia esse arquivo", "leia este arquivo",
		"refatore", "refatorar",
		"revise o codigo", "revise o código", "revisao de codigo", "revisão de código",
		"corrija o bug", "corrigir o bug", "conserte o bug",
		"implemente", "implementar",
		"modifique o arquivo", "modificar arquivo",
		"rode o comando", "roda o comando", "execute o comando",
		"analise o repositorio", "analise o repositório",
	}
	lower := strings.ToLower(msg)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}
func buildClaudeCodePrompt(msg string, req ClientRequest) string {
	return msg
}
func describeClaudeCodeAction(prompt string) string {
	p := strings.TrimSpace(prompt)
	if len(p) > 200 {
		p = p[:200] + "..."
	}
	return fmt.Sprintf("Vou executar via Claude Code (com acesso a arquivos/bash): \"%s\"", p)
}

// isOpenCodeTask detecta quando uma mensagem deve rotear para o motor OpenCode
// (quarto engine). Hoje: uso explicito da palavra "opencode", ou ForceOpenCode no request.
func isOpenCodeTask(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	return strings.Contains(lower, "opencode") || strings.HasPrefix(lower, "opencode:")
}

// describeOpenCodeAction descreve a acao para exibicao ao usuario (antes da aprovacao),
// paralela a describeClaudeCodeAction. Inclui o modelo ativo para transparencia.
func describeOpenCodeAction(prompt string) string {
	p := strings.TrimSpace(prompt)
	if len(p) > 200 {
		p = p[:200] + "..."
	}
	return fmt.Sprintf("Vou executar via OpenCode (modelo %s, com acesso a arquivos/bash): \"%s\"", activeModelTag(), p)
}

type claudeStreamEvent struct {
	Type    string `json:"type"`
	Message struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
}

func callClaudeCode(ctx context.Context, prompt string) (string, error) {
	prompt = ensureInlineContent(prompt)
	return runClaudeCodeCLI(ctx, prompt, false, false, false)
}

// callClaudeCodePlan — GATE PLAN (28/08): roda o CLI claude no modo plan
// NATIVO (--permission-mode plan) — o modelo descreve o plano e NÃO executa
// ferramentas. Nunca usa --dangerously-skip-permissions.
func callClaudeCodePlan(ctx context.Context, prompt string) (string, error) {
	prompt = ensureInlineContent(prompt)
	return runClaudeCodeCLI(ctx, prompt, false, true, false)
}

// callClaudeCodeAutonomous — GATE AUTÔNOMO (29/08): roda o CLI claude com o
// modo nativo "auto" (executa ferramentas sem pedir aprovação por ação),
// sob o usuário restrito claudeAgentUser (defesa em profundidade — mesmo
// tratamento do fluxo aprovado) e com a blocklist Hokma validada ANTES pelo
// caller. Nunca usa --dangerously-skip-permissions.
func callClaudeCodeAutonomous(ctx context.Context, prompt string) (string, error) {
	prompt = ensureInlineContent(prompt)
	return runClaudeCodeCLI(ctx, prompt, false, false, true)
}
func callClaudeCodeApproved(ctx context.Context, prompt string) (string, error) {
	prompt = ensureInlineContent(prompt)
	return runClaudeCodeCLI(ctx, prompt, true, false, false)
}

// FIX 16/08 (inline-content): quando o prompt pede para analisar/ler um
// arquivo especifico e o arquivo e conhecido e razoavelmente pequeno,
// injeta o conteudo direto no prompt — evitando a tool Read do SDK, que
// por tras do CLI claude (modo reasoning) demora >90-120s e estourava o
// limite do Cloudflare (~100s) -> 524. Medido: codigo inline 26.7s vs
// tool Read >120s. Limite conservador: 100KB.
const inlineFileMaxBytes = 100 * 1024

// arquivos sensiveis nunca sao injetados inline (defesa em profundidade)
var inlineBlockedPrefixes = []string{
	".env", ".ssh", "id_rsa", ".pem", ".keys", "memory.db",
	"config/", "credentials", "secrets",
}

func ensureInlineContent(prompt string) string {
	lower := strings.TrimSpace(prompt)
	if lower == "" {
		return prompt
	}
	// detecta "arquivo <path>" ou "arquivo: <path>" (PT) e "file <path>"
	re := regexp.MustCompile(`(?i)\b(arquivo|file)\s*[:\-]?\s+([^\s,;."']+\.(?:go|ts|tsx|js|jsx|py|yaml|yml|json|md|sh|sql|h|hpp|txt))`)
	m := re.FindStringSubmatch(prompt)
	if m == nil {
		return prompt
	}
	path := m[2]
	// resolve caminho relativo ao cwd do backend (/root/hokma/backend)
	candidates := []string{path}
	if !strings.HasPrefix(path, "/") {
		candidates = append(candidates, "/root/hokma/backend/"+path, "/root/hokma/"+path)
	}
	var data []byte
	var okPath string
	for _, c := range candidates {
		if b, err := os.ReadFile(c); err == nil {
			path = c
			data = b
			okPath = c
			break
		}
	}
	if data == nil {
		return prompt
	}
	// sensivel? nunca inline
	lp := strings.ToLower(okPath)
	for _, b := range inlineBlockedPrefixes {
		if strings.Contains(lp, strings.ToLower(b)) {
			return prompt
		}
	}
	if len(data) > inlineFileMaxBytes {
		return prompt
	}
	// marca o conteudo com fences e instrui principio de nao usar tools
	head := "INSTRUCAO: o conteudo do arquivo ja esta disponivel abaixo (entre as fences). " +
		"NAO use a tool de leitura/Read para este arquivo — analise o texto inline e responda.\n\n" +
		"=== CONTEUDO DO ARQUIVO: " + okPath + " ===\n"
	foot := "\n\n=== FIM DO CONTEUDO ===\n\n" + prompt
	return head + "```" + filepath.Ext(okPath)[1:] + "\n" + string(data) + "\n```" + foot
}

// claudeModelTag devolve a tag curta pro log do modelo do claude (modelA/modelB/outro).
func claudeModelTag(model string) string {
	if model == ModelA {
		return "modelA"
	}
	if model == ModelB {
		return "modelB"
	}
	return model
}

// claudeCLIArgs monta os argumentos do CLI claude.
// FIX 16/08 (Opcao A): modo --bare reduz drasticamente o startup do CLI
// (medido 7.0s -> 1.8s: pula hooks, LSP, plugins, auto-memory, CLAUDE.md
// discovery, keychain e prefetch). Requer --verbose (licenca do
// stream-json). Env/credenciais (openrouter via settings.json) intactos.
// claudeCLIArgs monta os argumentos do CLI claude.
// FIX 16/08 (Opcao A): modo --bare reduz drasticamente o startup do CLI
// (medido 7.0s -> 1.8s: pula hooks, LSP, plugins, auto-memory, CLAUDE.md
// discovery, keychain e prefetch). Requer --verbose (licenca do
// stream-json). Env/credenciais (openrouter via settings.json) intactos.
// GATE PLAN (28/08): em planMode usa o modo plan NATIVO do CLI
// (--permission-mode plan) — o claude NÃO executa ferramentas, só descreve
// o plano; nunca combina com --dangerously-skip-permissions.
func claudeCLIArgs(prompt string, skipPermissions bool, planMode bool, autonomousMode bool) []string {
	args := []string{"-p", prompt, "--output-format", "stream-json", "--verbose", "--bare"}
	if planMode {
		args = append(args, "--permission-mode", "plan")
	} else if autonomousMode {
		// GATE AUTÔNOMO (29/08): modo nativo 'auto' — o claude executa
		// ferramentas sem pedir aprovação por ação (a blocklist Hokma
		// valida o prompt ANTES de chegar aqui).
		args = append(args, "--permission-mode", "auto")
	} else if skipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args
}

func runClaudeCodeCLI(ctx context.Context, prompt string, skipPermissions bool, planMode bool, autonomousMode bool) (string, error) {
	// FASE 2b: sudo direto continua proibido mesmo no fluxo approved — o resto
	// (edicao de arquivos, git, bash sem sudo) executa normalmente.
	if (skipPermissions || autonomousMode) && strings.Contains(strings.ToLower(prompt), "sudo") {
		return "", claudeCodeBlocked()
	}
	// modelA primeiro (DeepSeek, gratuito/zen); fallback automatico para modelB
	// (Gemini 2.5 Flash) apenas se a chamada falhar com erro recuperavel.
	out, err := runClaudeCodeWithModel(ctx, prompt, skipPermissions, planMode, autonomousMode, getActiveModel())
	if err == nil {
		return out, nil
	}
	// sofoca a troca de modelo em erros transitórios (rate-limit/indisponibilidade/timeout)
	if isRecoverableClaudeError(err) {
		log.Printf("⚠️ claude_code modelA falhou (%v) — reexecutando com modelB=%s", err, ModelB)
		return runClaudeCodeWithModel(ctx, prompt, skipPermissions, planMode, autonomousMode, ModelB)
	}
	logModelIncompatibility("claude_code", getActiveModel(), err)
	return "", err
}

// isRecoverableClaudeError decide quando vale a pena tentar o fallback de modelo.
// Timeout/empty/erro de exit sao considerados recuperaveis (o modelo/proxy falhou),
// enquanto vazamento de system prompt e bloqueios de seguranca NAO sao.
func isRecoverableClaudeError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "resposta vazia") ||
		strings.Contains(msg, "exit error") ||
		strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate")
}

// runClaudeCodeWithModel roda o CLI claude sobrescrevendo ANTHROPIC_MODEL no env
// (proxy OpenRouter via ~/.claude/settings.json). Permite fallback entre modelA/modelB.
// O modelo do catalogo e' normalizado para o id aceito pelo proxy OpenRouter
// (remove o sufixo -free: "deepseek-v4-flash-free" → "deepseek-v4-flash").
//
// FIX 20/08 (root): o CLI claude recusa --dangerously-skip-permissions quando
// o processo roda como root ("cannot be used with root/sudo privileges for
// security reasons"). O fluxo aprovado (skipPermissions=true) executa o CLI
// via runuser como usuario dedicado sem privilegios (hokma-agent), cujo
// ~/.claude/settings.json e' mantido em sincronia pela propagacao de modelo.
// O fluxo read-only (skipPermissions=false) segue direto como root, sem a
// flag (o guard so se aplica ao bypass).
// ORPHAN KILL (29/08): ctx é o request context — quando o cliente HTTP
// desconecta, ctx.Done() dispara e o exec.CommandContext mata o claude CLI
// em vez de deixar o job rodando até o fim (e desperdiçando tokens).
func runClaudeCodeWithModel(parentCtx context.Context, prompt string, skipPermissions bool, planMode bool, autonomousMode bool, model string) (string, error) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithTimeout(parentCtx, claudeCodeTimeout)
	defer cancel()
	args := claudeCLIArgs(prompt, skipPermissions, planMode, autonomousMode)
	var cmd *exec.Cmd
	if skipPermissions || autonomousMode {
		cmd = exec.CommandContext(ctx, runuserBin, append([]string{"-u", claudeAgentUser, "--", "claude"}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, "claude", args...)
	}
	// override do modelo ATIVO via env (o proxy OpenRouter aceita ANTHROPIC_MODEL)
	cmd.Env = append(os.Environ(), "ANTHROPIC_MODEL="+normalizeModelSlugForAPI(model))
	stdout, pipeErr := cmd.StdoutPipe()
	if pipeErr != nil {
		return "", fmt.Errorf("claude code: erro ao abrir stdout: %v", pipeErr)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if startErr := cmd.Start(); startErr != nil {
		return "", fmt.Errorf("claude code: erro ao iniciar: %v — stderr: %s", startErr, stderr.String())
	}
	logTag := "claude_code_invoke:" + claudeModelTag(model)
	if skipPermissions || autonomousMode {
		logTag = "claude_code_invoke_approved:" + claudeModelTag(model)
	}

	// FIX 16/08 (Opcao A): leitura incremental do stream com deteccao de
	// vazamento a cada chunk. Se a narrativa interna do SDK aparece, mata
	// o processo IMEDIATAMENTE e retorna errSystemPromptLeak — antes o
	// cmd.Run() esperava o CLI terminar inteiro (~100s+), estourando o
	// timeout do Cloudflare (524) sem nenhuma resposta util pro usuario.
	text, leaked, _ := processClaudeStream(stdout)
	if leaked {
		log.Printf("⚠️ claude_code: vazamento DETECTADO DURANTE stream, matando processo (%s)", logTag)
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s system_prompt_leak_blocked_early", logTag),
		)
		return "", errSystemPromptLeak
	}
	runErr := cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s timeout", logTag),
		)
		return "", fmt.Errorf("claude code: timeout apos %s", claudeCodeTimeout)
	}
	if runErr != nil && text == "" {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s fail", logTag),
		)
		return "", fmt.Errorf("claude code: exit error: %v — stderr: %s", runErr, stderr.String())
	}
	if text == "" {
		sqliteExecParams(
			`INSERT INTO logs (event, level, source) VALUES (?, 'WARN', 'claude_code_client');`,
			fmt.Sprintf("%s empty", logTag),
		)
		return "", fmt.Errorf("claude code: resposta vazia")
	}
	sqliteExecParams(
		`INSERT INTO logs (event, level, source) VALUES (?, 'INFO', 'claude_code_client');`,
		fmt.Sprintf("%s ok", logTag),
	)
	return text, nil
}

// processClaudeStream le linhas NDJSON do stream do CLI, acumula o texto
// do assistente e verifica vazamento de system prompt A CADA chunk.
// Retorna (textoAcumulado, vazou, err). Separado em funcao propria para
// permitir teste unitario dos dois cenarios (vazamento no meio do stream
// e stream limpo) sem invocar o binario claude.
func processClaudeStream(r io.Reader) (string, bool, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	var out strings.Builder
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, `"type":"assistant"`) {
			continue
		}
		var event claudeStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event.Type != "assistant" {
			continue
		}
		for _, c := range event.Message.Content {
			if c.Type == "text" && c.Text != "" {
				out.WriteString(c.Text)
			}
		}
		if out.Len() > 0 && detectSystemPromptLeak(out.String()) {
			return out.String(), true, nil
		}
	}
	return out.String(), false, scanner.Err()
}

// === FASE 2b: Bloqueio de execucao sudo direta ===
func claudeCodeBlocked() error {
	return fmt.Errorf("EXECUCAO BLOQUEADA: uso de sudo direto foi desativado. Use o agent loop bash_exec (mutantTools) em vez de callClaudeCode direto. O comando sera roteado pelo gate de aprovacao com diff visual.")
}
