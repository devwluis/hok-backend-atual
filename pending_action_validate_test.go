package main

import (
	"strings"
	"testing"
)

// TestValidateArgsBeforePendingExtended cobre os casos de corrupcao do
// minimax-m3 observados em producao (nodes: [""], settings/staticData como
// string de espaco, name numerico) e garante que a validacao estendida
// bloqueia antes do gate de aprovacao, forcando o retry do agent-loop.
func TestValidateArgsBeforePendingExtended(t *testing.T) {
	validNode := `{"name":"Webhook","type":"n8n-nodes-base.webhook","typeVersion":2,
		"parameters":{"path":"meu-webhook","httpMethod":"POST"},"position":[250,300]}`

	cases := []struct {
		name     string
		args     string
		wantFail bool
	}{
		// ── casos corruptos (DEVEM ser bloqueados) ──
		{
			name:     "nodes com string vazia (bug minimax-m3)",
			args:     `{"name":"FixTest","nodes":[""]}`,
			wantFail: true,
		},
		{
			name:     "nodes vazio",
			args:     `{"name":"DsTest","nodes":[]}`,
			wantFail: true,
		},
		{
			name:     "settings como string de espacos (bug minimax-m3)",
			args:     `{"name":"DsFix","nodes":[` + validNode + `],"settings":"   "}`,
			wantFail: true,
		},
		{
			name:     "staticData como string de tab (bug minimax-m3)",
			args:     `{"name":"DsFix","nodes":[` + validNode + `],"staticData":"\t"}`,
			wantFail: true,
		},
		{
			name:     "connections como string",
			args:     `{"name":"DsFix","nodes":[` + validNode + `],"connections":"x"}`,
			wantFail: true,
		},
		{
			name:     "name numerico",
			args:     `{"name":123,"nodes":[` + validNode + `]}`,
			wantFail: true,
		},
		{
			name:     "node sem type",
			args:     `{"name":"Wf","nodes":[{"name":"Webhook"}]}`,
			wantFail: true,
		},
		{
			name:     "node sem name",
			args:     `{"name":"Wf","nodes":[{"type":"n8n-nodes-base.webhook"}]}`,
			wantFail: true,
		},
		{
			name:     "argsJSON invalido",
			args:     `{"name":"Wf","nodes":[`,
			wantFail: true,
		},
		{
			name:     "name ausente",
			args:     `{"nodes":[` + validNode + `]}`,
			wantFail: true,
		},
		{
			name:     "nodes nao e array",
			args:     `{"name":"Wf","nodes":{"a":1}}`,
			wantFail: true,
		},
		// ── casos validos (NAO devem ser bloqueados) ──
		{
			name:     "payload completo valido",
			args:     `{"name":"Wf","nodes":[` + validNode + `],"connections":{},"settings":{"executionOrder":"v1"},"staticData":{}}`,
			wantFail: false,
		},
		{
			name:     "apenas name+nodes",
			args:     `{"name":"Wf","nodes":[` + validNode + `]}`,
			wantFail: false,
		},
		{
			name:     "varios nodes validos",
			args:     `{"name":"Wf","nodes":[` + validNode + `,{"name":"Responder","type":"n8n-nodes-base.respondToWebhook","typeVersion":1,"parameters":{},"position":[450,300]}]}`,
			wantFail: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateArgsBeforePending("n8n_create_workflow", tc.args)
			if tc.wantFail && err == nil {
				t.Fatalf("esperava falha de validacao, mas passou: args=%s", tc.args)
			}
			if !tc.wantFail && err != nil {
				t.Fatalf("esperava passar, mas falhou: %v (args=%s)", err, tc.args)
			}
		})
	}
}

// TestValidationRetryHint garante que o erro devolvido ao modelo no retry
// ensina o formato correto de nodes (reduz iteracoes ate o teto do loop).
func TestValidationRetryHint(t *testing.T) {
	hint := validationRetryHint("n8n_create_workflow", "campo 'nodes[0]' invalido")
	if !containsStr(hint, "Formato correto de 'nodes'") || !containsStr(hint, "typeVersion") {
		t.Fatalf("hint sem instrucao de formato: %q", hint)
	}
	plain := validationRetryHint("bash_exec", "erro qualquer")
	if plain != "erro qualquer" {
		t.Fatalf("hint nao deveria enriquecer tool fora de workflow: %q", plain)
	}
}

func containsStr(s, sub string) bool {
	return len(sub) > 0 && (len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})())
}

// TestN8nCreateWorkflowPipelineOffline simula o cenario de criacao de
// workflow SEM tocar em producao: payload valido atravessa todas as camadas
// internas (validateNodesArray, repairs, guard XML, validacao estatica) e so
// falha na chamada REST por ausencia de N8N_API_KEY; payload corrupto e
// rejeitado com erro JSON (que volta ao modelo como tool result).
func TestN8nCreateWorkflowPipelineOffline(t *testing.T) {
	t.Setenv("N8N_API_KEY", "")
	t.Setenv("N8N_MCP_URL", "http://127.0.0.1:1/mcp")
	t.Setenv("N8N_BASE_URL", "http://127.0.0.1:1")

	valid := `{"name":"TesteOffline","nodes":[{"name":"Webhook","type":"n8n-nodes-base.webhook","typeVersion":2,"parameters":{"path":"teste-offline","httpMethod":"POST"},"position":[250,300]}]}`
	res := n8nCreateWorkflow(valid)
	if !strings.Contains(res, "N8N_API_KEY") {
		t.Fatalf("payload valido deveria chegar ate a chamada REST e falhar so na API key; resposta: %s", res)
	}

	corrupt := `{"name":"Corrupto","nodes":[""]}`
	res2 := n8nCreateWorkflow(corrupt)
	if !strings.Contains(res2, "error") {
		t.Fatalf("payload corrupto deveria ser rejeitado com erro; resposta: %s", res2)
	}

	corrupt2 := `{"name":123,"nodes":[{"name":"W","type":"n8n-nodes-base.webhook"}]}`
	res3 := n8nCreateWorkflow(corrupt2)
	if !strings.Contains(res3, "error") {
		t.Fatalf("name numerico deveria ser rejeitado; resposta: %s", res3)
	}
}