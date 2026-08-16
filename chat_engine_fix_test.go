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

func TestDetectSystemPromptLeak_catchesRealLeak(t *testing.T) {
	if !detectSystemPromptLeak(leakedSystemPromptSample) {
		t.Fatal("filtro NAO detectou o vazamento real da sessao fc8c97d7")
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