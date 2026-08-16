package main

import (
	"strings"
	"testing"
)

// Fix 16/08 item B — vazamento de system prompt/skills do SDK do Claude
// via engine claude_code. O filtro precisa pegar o padrão real da sessão
// fc8c97d7 (16/08, "Hok?") e NÃO acusar falso positivo em conversa normal.
const leakedSystemPromptSample = `
. Run when there's a pending security request, flag as suspicious, or upon request. Run it before pushing, publishing, or merging security-relevant changes.
- still-silent: Code silently\s, session start detection. Does not create session tokens -> produces valid but unused stack Impostor
- /dev-discord-webhook: Send a "webhook" Discord message to a channel with a specific "id" for dev purposes
- /agents: List agents — use when tracking state or coordination of multiple subagents
- /workflows: Watch live progress of running workflows.

Remember you must ALREADY WANT to invoke a skill (e.g., to refactor, to review, or because revival is in progress). A skill description here is a prompt; this just describes what a skill exists to do, and by no means auto-invokes a project, admin, alphabet, it comes, or anything else.

The system reminder says to respond in Portuguese (pt-BR). The user just said "Hok?" which is a greeting/call. Let me respond appropriately.

The user greeted me with "Hok?" - a shorthand for "Hokma?". I should respond in Portuguese. Let me be brief and ask what they want.Hok! Aqui no backend Hokma. Em que posso ajudar?
`

// Segundo vazamento real observado (16/08, "mostre o conteudo do arquivo
// go.mod" com ForceClaudeCode): skills personalizadas do projeto Hokma
// configuradas no claude CLI, regurgitadas antes da resposta util.
const leakedProjectSkillsSample = `
- perform_scan: Import & scan an artifact in the "gophish" or "genai-pentest" workspace (run GenAI/LLM pentest scenarios against a tool's built-in scaffolding).
- CodeOfConduct: Reports misbehavior by any occupant of the microcontroller interface
- login: An official login flow. The memorized account to log in with is: hokma. This account is part of the "hokma" team, and it's the primary account for the whole Hokma system.
- keychain: Store and retrieve secrets (API keys, tokens, passwords) once you've logged in. Note: the service is UC-only (US-only?) and is only available to US-based users. If you haven't logged in yet, Hokma does not know about your session it (keys). Login instructions in keychain skill.
- conversational: Handle the killer-content or content-graph conversations with memory RAG.

These skills are available, but I'm not in their path context. I'd recommend running from the project directory and not relative to it.

Commands: /help, exit, /worktree, /control
Note: Speed is not shown in this mode by default. Set speed to true via /control to see it.
**Note: Save analysis in your own memory too I might forget details.**

The user wants me to show the contents of go.mod file. Let me read it.

Aqui está o conteúdo do go.mod:
module hokma_backend
go 1.26.3
`

// Terceiro vazamento real (16/08, mesma pergunta): o modelo regurgitou
// outra parte da lista de skills — meio da skill "security-review" e
// "attack-surface-reduction" — seguido de narrativa interna.
const leakedProjectSkillsSample3 = `
and report back, not applying any changes. Reports a security review across a flag(s) and reports back on the passed-in flags. To-Do items / flags for the future: report findings then implement.
- attack-surface-reduction: Reduce the attack surface of the project's data plane by auditing the threat model, reducing the rules surface allowed by Bash/Agent tools and MCP servers, and by building software controls onto the (BusyBox) container runtime.

The user wants to see the content of the go.mod file. Let me read it.

Conteúdo do arquivo /root/hokma/backend/go.mod:
module hokma_backend
`

// Quarto/quinto vazamentos reais coletados em produção (16/08, mesmo
// prompt "mostre o conteudo do arquivo go.mod"): narrativa interna + 
// fragmentos do system prompt antes da resposta util.
const leakedProjectSkillsSample4 = `
There is also a secure multi-agent facility - "Workflow" - but you must only invoke it with explicit opt-in as described in the working notes.

The user has a question about the Go module dependencies. Let me just read the go.mod file. This is a simple read operation, so let me use the Read tool directly.

Actually, I should respond in Portuguese (pt-BR) per the project instructions.Vou mostrar o conteúdo do arquivo go.mod.
`

const leakedProjectSkillsSample5 = `
based on the current repository _only_, with git-based and prompt-injection concerns. It runs against the git state only (no internet), reviewing the current diff plus the prior committed state, and produces a written report in the discussions channel.

To operate the project, you're at:
/root/hokma/backend

Run go build -o hokma_test . to build the backend. Note: running go build on this backend requires passing SECURITY: by default the harness attaches a security agent that demands an expl
`

// Samples coletados em produção que ESCAPARAM do filtro v1 (16/08, mesmo
// prompt "mostre o conteudo do arquivo go.mod") — camada de densidade de
// narrativa inglesa precisa pegar todos.
const leakedEscapeRun1 = `
, comparing against known vulnerability patterns, and produce a report. Focus on the working
`

const leakedEscapeRun4 = `
against a security checklist. Runs a review against both staged and unstaged changes locat
`

const leakedEscapeRun5 = `
- /codeflow, new-ish: Review the current diff for production readiness — correctness, scal
`

// Escape real recente: skill "local tasks" com narrativa de invocação.
const leakedEscapeLocalTasks = `
- file local tasks.LocalTasks, parse /local-tasks.txt to run local tasks. The skills list: has local tasks available.

Note: this task is available as a skill invocation. 完整地执行任务 (It is available as a skill invocation. Do the task completely.)

Note: this task is available as a skill invocation. Note: this task is available as a skill invocation.

Conteúdo de /root/hokma/backend/go.mod:
module hokma_backend
`

// Escape real: skill "debug" isolada com narrativa de diff.
const leakedEscapeDebug = `
or the recent diff
- debug: debug a problem, task or project
`

func TestDetectSystemPromptLeak_catchesRealLeak(t *testing.T) {
	if !detectSystemPromptLeak(leakedSystemPromptSample) {
		t.Fatal("filtro NAO detectou o vazamento real da sessao fc8c97d7")
	}
	if !detectSystemPromptLeak(leakedProjectSkillsSample) {
		t.Fatal("filtro NAO detectou o vazamento real de skills do projeto (go.mod)")
	}
	if !detectSystemPromptLeak(leakedProjectSkillsSample3) {
		t.Fatal("filtro NAO detectou o vazamento real variante 3 (security-review)")
	}
	if !detectSystemPromptLeak(leakedProjectSkillsSample4) {
		t.Fatal("filtro NAO detectou o vazamento real variante 4 (workflow/working notes)")
	}
	if !detectSystemPromptLeak(leakedProjectSkillsSample5) {
		t.Fatal("filtro NAO detectou o vazamento real variante 5 (git state/harness)")
	}
	if !detectSystemPromptLeak(leakedEscapeRun1) {
		t.Fatal("filtro NAO detectou o escape run 1 (vulnerability patterns)")
	}
	if !detectSystemPromptLeak(leakedEscapeRun4) {
		t.Fatal("filtro NAO detectou o escape run 4 (security checklist)")
	}
	if !detectSystemPromptLeak(leakedEscapeRun5) {
		t.Fatal("filtro NAO detectou o escape run 5 (/codeflow production readiness)")
	}
	if !detectSystemPromptLeak(leakedEscapeLocalTasks) {
		t.Fatal("filtro NAO detectou o escape local tasks (skill invocation)")
	}
	if !detectSystemPromptLeak(leakedEscapeDebug) {
		t.Fatal("filtro NAO detectou o escape debug (recent diff)")
	}
}

func TestDetectSystemPromptLeak_noFalsePositive(t *testing.T) {
	normal := []string{
		"Hok! Aqui no backend Hokma. Em que posso ajudar?",
		"Sim, o deploy foi concluído com sucesso. O nginx foi recarregado e o serviço está ativo.",
		"Vou analisar o log para investigar o erro no workflow do n8n.",
		"Os agentes configurados estão rodando normalmente. Veja os status abaixo:",
		"O sistema reminder do n8n foi atualizado para responder em pt-BR.",
	}
	for _, msg := range normal {
		if detectSystemPromptLeak(msg) {
			t.Errorf("falso positivo: %q", msg)
		}
	}
}

// Fix 16/08 item C — ForceClaudeCode só deve forçar o engine claude_code
// em perguntas que precisam de ferramentas/ações reais. Triviais devem
// voltar pro engine chat.
func TestNeedsRealTools_triviaisVaoProChat(t *testing.T) {
	triviais := []string{
		"Hok?",
		"Oi",
		"Olá",
		"Tudo bem?",
		"Bom dia",
		"O que você sabe fazer?",
		"Qual é a capital do Brasil?",
		"Me conta uma piada",
		"Obrigado",
	}
	for _, msg := range triviais {
		if needsRealTools(msg) {
			t.Errorf("trivial deveria ir pro chat normal: %q", msg)
		}
	}
}

func TestNeedsRealTools_acoesVaoProClaudeCode(t *testing.T) {
	acoes := []string{
		"edite o arquivo config.yaml",
		"refatore o smart_chat.go",
		"rode o comando go build",
		"faça deploy do backend",
		"git push da branch main",
		"crie um workflow no n8n",
		"corrija o bug no log de erro",
		"instale o pacote via bash",
		"analise o repositório",
		"execute o script de migração do banco",
	}
	for _, msg := range acoes {
		if !needsRealTools(msg) {
			t.Errorf("acao deveria ir pro claude_code: %q", msg)
		}
	}
}

// Gate combinado: ForceClaudeCode + trivial => chat; ForceClaudeCode + ação => claude_code.
func TestClassifyEngine_forceClaudeCodeGate(t *testing.T) {
	req := ClientRequest{ForceClaudeCode: true}
	if got := classifyEngine("Hok?", req); got == "claude_code" {
		t.Error("Hok? com ForceClaudeCode NAO deve ir pro claude_code")
	}
	if got := classifyEngine("edite o arquivo main.go", req); got != "claude_code" {
		t.Errorf("acao com ForceClaudeCode deveria ir pro claude_code, foi: %q", got)
	}
	reqSemForce := ClientRequest{}
	if got := classifyEngine("Hok?", reqSemForce); got == "claude_code" {
		t.Error("Hok? sem force nao deve ir pro claude_code")
	}
}

// O texto vazado que o runClaudeCodeCLI retornaria: garantir que o check
// inline (text != "" && detectSystemPromptLeak) não deixe passar.
func TestLeakBlock_roundTrip(t *testing.T) {
	text := strings.TrimSpace(leakedSystemPromptSample)
	if text == "" {
		t.Fatal("sample vazio")
	}
	if detectSystemPromptLeak(text) != true {
		t.Fatal("filtro deveria bloquear o sample")
	}
	if err := errSystemPromptLeak; err == nil || !strings.Contains(err.Error(), "vazamento") {
		t.Fatal("errSystemPromptLeak deveria existir com mensagem descritiva")
	}
}