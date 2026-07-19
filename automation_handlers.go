package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

type AutoTestReq struct {
	PendingID    string                 `json:"pending_id"`
	WorkflowJSON map[string]interface{} `json:"workflow_json"`
}

type AutoTestResp struct {
	Valid     bool     `json:"valid"`
	NodeCount int      `json:"node_count"`
	NodeNames []string `json:"node_names"`
	Errors    []string `json:"errors"`
	Warnings  []string `json:"warnings"`
}

func handleAutomationTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, 405)
		return
	}
	if r.Header.Get("X-Hok-Token") != os.Getenv("HOK_TOKEN") {
		http.Error(w, `{"error":"unauthorized"}`, 401)
		return
	}

	var req AutoTestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid body"}`, 400)
		return
	}

	var wfJSON map[string]interface{}

	if req.PendingID != "" {
		pendingMu.Lock()
		p, ok := pendingAutomations[req.PendingID]
		pendingMu.Unlock()
		if !ok {
			http.Error(w, `{"error":"pending não encontrado"}`, 404)
			return
		}
		wfJSON = p.WorkflowJSON
	} else if req.WorkflowJSON != nil {
		wfJSON = req.WorkflowJSON
	} else {
		http.Error(w, `{"error":"pending_id ou workflow_json obrigatório"}`, 400)
		return
	}

	result := validateWorkflowJSON(wfJSON)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
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
