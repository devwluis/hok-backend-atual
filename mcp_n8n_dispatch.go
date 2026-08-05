package main

import (
	"encoding/json"
	"log"
	"fmt"
)

var mcpClient = newMCPN8nClient()

// n8nExpertLookupDispatch roteia a tool n8n_expert_lookup para a acao MCP
// correta (search_nodes, get_node, validate_node). Se o MCP estiver fora
// do ar, cai de volta no conhecimento estatico local (n8n_expert.md).
func n8nExpertLookupDispatch(args map[string]interface{}) string {
	action, _ := args["action"].(string)

	var mcpArgs map[string]interface{}
	var toolName string

	switch action {
	case "search_nodes":
		query, _ := args["query"].(string)
		if query == "" {
			return "erro: parametro 'query' e obrigatorio para search_nodes"
		}
		toolName = "search_nodes"
		mcpArgs = map[string]interface{}{"query": query, "includeExamples": true}

	case "get_node":
		nodeType, _ := args["nodeType"].(string)
		if nodeType == "" {
			return "erro: parametro 'nodeType' e obrigatorio para get_node"
		}
		toolName = "get_node"
		mcpArgs = map[string]interface{}{"nodeType": nodeType, "detail": "standard", "includeExamples": true}

	case "validate_node":
		nodeType, _ := args["nodeType"].(string)
		if nodeType == "" {
			return "erro: parametro 'nodeType' e obrigatorio para validate_node"
		}
		config, _ := args["config"].(map[string]interface{})
		if config == nil {
			config = map[string]interface{}{}
		}
		toolName = "validate_node"
		mcpArgs = map[string]interface{}{"nodeType": nodeType, "config": config, "mode": "minimal"}

	default:
		return fmt.Sprintf("erro: action '%s' invalida. Use search_nodes, get_node ou validate_node", action)
	}

	log.Printf("[mcp-n8n] chamando tool=%s args=%+v", toolName, mcpArgs)
	result, err := mcpClient.CallTool(toolName, mcpArgs)
	if err != nil {
		log.Printf("[mcp-n8n] FALHOU, caindo no fallback estatico: %v", err)
		return fmt.Sprintf("MCP indisponivel (%v). Use o conhecimento estatico de n8n_expert.md para essa consulta.", err)
	}
	log.Printf("[mcp-n8n] sucesso, %d bytes de resposta", len(result))

	// resultado do MCP vem em result.content[0].text (as vezes com structuredContent junto)
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal([]byte(result), &parsed); err == nil && len(parsed.Content) > 0 {
		return parsed.Content[0].Text
	}

	return result
}
