package main

import (
	"encoding/json"
	"fmt"
	"log"
)

// n8nValidateWorkflowViaMCP valida um payload de workflow antes de
// criar/atualizar no n8n. Tenta primeiro o servidor MCP (validate_workflow);
// se o MCP estiver indisponivel ou a tool nao existir, cai de volta na
// validacao estatica local (validateWorkflowJSON), que ja roda em
// n8nTestWorkflow/handleAutomationTest e e testada em producao.
// Retorna (valido, relatorio_texto).
func n8nValidateWorkflowViaMCP(payload map[string]any) (bool, string) {
	// Guarda única de XML malicioso: bloqueia na origem antes de qualquer
	// encaminhamento ao MCP, impedindo XXE no downstream e a reflexão do
	// XML para o LLM (prompt injection).
	if findings := guardWorkflowXML(payload); len(findings) > 0 {
		return false, "[guard] " + formatGuardFindings(findings)
	}

	client := newMCPN8nClient()
	result, err := client.CallTool("validate_workflow", map[string]interface{}{
		"workflow": payload,
	})
	if err == nil && result != "" {
		if valid, report, ok := parseMCPWorkflowVerdict(result); ok {
			return valid, report
		}
		// resposta veio mas sem veredito parseavel — não confia no MCP e RODA o
		// fallback local de verdade (antes retornava false e bloqueava a criação
		// com mensagem enganosa de "fallback local" que nunca acontecia).
		log.Printf("[mcp-n8n] validate_workflow sem veredito parseavel (%s) — rodando validacao local", truncateForLog([]byte(result)))
		resp := validateWorkflowJSON(payload)
		if resp.Valid {
			return true, fmt.Sprintf("[MCP sem veredito parseavel, fallback local] OK — %d node(s): %v",
				resp.NodeCount, resp.NodeNames)
		}
		report := "[MCP sem veredito parseavel, fallback local] validacao falhou:\n"
		for _, e := range resp.Errors {
			report += "- " + e + "\n"
		}
		for _, w := range resp.Warnings {
			report += "(aviso) " + w + "\n"
		}
		return false, report
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

// parseMCPWorkflowVerdict extrai o veredito real do resultado do MCP
// validate_workflow. O servidor (n8n-documentation-mcp) devolve
// structuredContent.valid + errors/warnings; alguns clientes MCP só repassam
// o content[0].text com o mesmo JSON embutido — tenta os dois formatos.
func parseMCPWorkflowVerdict(raw string) (valid bool, report string, ok bool) {
	// formato 1: structuredContent direto (confirmado em producao)
	var env struct {
		StructuredContent json.RawMessage `json:"structuredContent"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err == nil && len(env.StructuredContent) > 0 {
		if v, r, k := decodeVerdict(env.StructuredContent); k {
			return v, "[MCP] " + r, true
		}
	}
	// formato 2: content[0].text contem o JSON do veredito
	var wrapped struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err == nil && len(wrapped.Content) > 0 {
		if v, r, k := decodeVerdict([]byte(wrapped.Content[0].Text)); k {
			return v, "[MCP] " + r, true
		}
	}
	return false, "", false
}

// decodeVerdict interpreta {"valid": bool, "summary": {...}, "errors": [...], "warnings": [...]}.
func decodeVerdict(data []byte) (valid bool, report string, ok bool) {
	var v struct {
		Valid bool `json:"valid"`
		Summary struct {
			ErrorCount   int `json:"errorCount"`
			WarningCount int `json:"warningCount"`
		} `json:"summary"`
		Errors   []struct{ Node, Message string } `json:"errors"`
		Warnings []struct{ Node, Message string } `json:"warnings"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return false, "", false
	}
	report = fmt.Sprintf("valid=%v (erros=%d, avisos=%d)", v.Valid, v.Summary.ErrorCount, v.Summary.WarningCount)
	if len(v.Errors) > 0 {
		for _, e := range v.Errors {
			if e.Node != "" {
				report += "\n- [erro] " + e.Node + ": " + e.Message
			} else {
				report += "\n- [erro] " + e.Message
			}
		}
	}
	if len(v.Warnings) > 0 {
		for _, w := range v.Warnings {
			if w.Node != "" {
				report += "\n(aviso) " + w.Node + ": " + w.Message
			} else {
				report += "\n(aviso) " + w.Message
			}
		}
	}
	return v.Valid, report, true
}
