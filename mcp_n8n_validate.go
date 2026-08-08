package main

import "fmt"

// n8nValidateWorkflowViaMCP valida um payload de workflow antes de
// criar/atualizar no n8n. Tenta primeiro o servidor MCP (validate_workflow);
// se o MCP estiver indisponivel ou a tool nao existir, cai de volta na
// validacao estatica local (validateWorkflowJSON), que ja roda em
// n8nTestWorkflow/handleAutomationTest e e testada em producao.
// Retorna (valido, relatorio_texto).
func n8nValidateWorkflowViaMCP(payload map[string]any) (bool, string) {
	client := newMCPN8nClient()
	result, err := client.CallTool("validate_workflow", map[string]interface{}{
		"workflow": payload,
	})
	if err == nil && result != "" {
		return true, result
	}

	// Fallback: validacao estatica local (mesma logica de n8nTestWorkflow)
	resp := validateWorkflowJSON(payload)
	if resp.Valid {
		return true, fmt.Sprintf("[MCP indisponivel, fallback local] OK — %d node(s): %v",
			resp.NodeCount, resp.NodeNames)
	}
	report := fmt.Sprintf("[MCP indisponivel (%v), fallback local] validacao falhou:\n", err)
	for _, e := range resp.Errors {
		report += "- " + e + "\n"
	}
	for _, w := range resp.Warnings {
		report += "(aviso) " + w + "\n"
	}
	return false, report
}
