package main

import (
	"context"
	"strings"
	"testing"
)

func TestGuardrailWorkflowId(t *testing.T) {
	cases := []struct {
		tool string
		args string
	}{
		{"n8n_update_workflow", `{}`},
		{"n8n_activate_workflow", `{}`},
		{"n8n_execute_workflow", `{}`},
		{"n8n_get_execution_errors", `{}`},
		{"n8n_diagnose_workflow", `{}`},
		{"n8n_get_workflow_detail", `{}`},
		{"n8n_delete_workflow", `{}`},
		{"n8n_test_workflow", `{}`},
	}

	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			result := executeTool(context.Background(), c.tool, c.args)
			lower := strings.ToLower(result)
			blocked := strings.Contains(lower, "obrigat")
			if !blocked {
				t.Errorf("GUARDRAIL FALHOU — %s aceitou args vazios sem bloquear.\nResposta: %s", c.tool, result)
			} else {
				t.Logf("OK %s bloqueou corretamente: %s", c.tool, result)
			}
		})
	}
}
