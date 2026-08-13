package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ── N8N Helper (extraído de automation.go ao arquivar o módulo automation, 10/08) ──
// n8nRequest faz a chamada HTTP e devolve o corpo junto com o status code real,
// para o chamador distinguir erro de transporte de erro de negócio (ex: 404).
func n8nRequest(method, url, apiKey string, body []byte) ([]byte, int, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-N8N-API-KEY", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return nil, resp.StatusCode, rerr
	}
	return data, resp.StatusCode, nil
}

// ── Workflow JSON validation (extraído de automation_handlers.go ao arquivar o módulo automation, 10/08) ──
type AutoTestResp struct {
	Valid     bool     `json:"valid"`
	NodeCount int      `json:"node_count"`
	NodeNames []string `json:"node_names"`
	Errors    []string `json:"errors"`
	Warnings  []string `json:"warnings"`
}

func validateWorkflowJSON(wfJSON map[string]interface{}) AutoTestResp {
	result := AutoTestResp{
		Valid:    true,
		Errors:   []string{},
		Warnings: []string{},
	}
	rawNodes, ok := wfJSON["nodes"].([]interface{})
	if !ok || len(rawNodes) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "workflow sem nodes ou campo 'nodes' ausente/malformado")
		return result
	}
	result.NodeCount = len(rawNodes)
	nodeNames := map[string]bool{}
	for i, raw := range rawNodes {
		node, ok := raw.(map[string]interface{})
		if !ok {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("node #%d não é um objeto válido", i))
			continue
		}
		name, _ := node["name"].(string)
		nodeType, _ := node["type"].(string)
		if name == "" {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("node #%d sem campo 'name'", i))
		} else {
			nodeNames[name] = true
			result.NodeNames = append(result.NodeNames, name)
		}
		if nodeType == "" {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("node '%s' sem campo 'type'", name))
		}
		if nodeType == "n8n-nodes-base.code" {
			params, _ := node["parameters"].(map[string]interface{})
			if params != nil {
				if jsCode, ok := params["jsCode"].(string); ok && strings.Contains(jsCode, "$env") {
					result.Warnings = append(result.Warnings, fmt.Sprintf(
						"node '%s' (Code) usa $env diretamente - pode ser bloqueado pelo sandbox do n8n; prefira credentials nativas", name))
				}
			}
		}
		if _, ok := node["id"]; !ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"node '%s' sem campo 'id' - o n8n gera um id novo e conexoes podem quebrar; prefira ids estaveis", name))
		}
		if _, ok := node["typeVersion"]; !ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"node '%s' sem 'typeVersion' - o servidor resolve para a versao default do node", name))
		}
		if _, ok := node["position"]; !ok {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"node '%s' sem 'position' (canvas) - sera posicionado automaticamente", name))
		}
	}
	if conns, ok := wfJSON["connections"].(map[string]interface{}); ok {
		for sourceName, targets := range conns {
			if !nodeNames[sourceName] {
				result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf(
					"connections referencia node de origem '%s' que não existe em 'nodes'", sourceName))
			}
			walkConnectionTargets(targets, nodeNames, sourceName, &result)
		}
	} else {
		result.Warnings = append(result.Warnings, "workflow sem campo 'connections' - nodes podem estar desconectados")
	}
	return result
}

func walkConnectionTargets(targets interface{}, nodeNames map[string]bool, sourceName string, result *AutoTestResp) {
	byType, ok := targets.(map[string]interface{})
	if !ok {
		return
	}
	for _, branches := range byType {
		branchList, ok := branches.([]interface{})
		if !ok {
			continue
		}
		for _, branch := range branchList {
			connList, ok := branch.([]interface{})
			if !ok {
				continue
			}
			for _, connRaw := range connList {
				conn, ok := connRaw.(map[string]interface{})
				if !ok {
					continue
				}
				targetName, _ := conn["node"].(string)
				if targetName != "" && !nodeNames[targetName] {
					result.Valid = false
					result.Errors = append(result.Errors, fmt.Sprintf(
						"connections de '%s' aponta para node '%s' que não existe em 'nodes'", sourceName, targetName))
				}
			}
		}
	}
}
