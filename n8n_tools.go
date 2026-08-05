package main

// n8n_tools.go
//
// Ferramentas que dão ao Hok acesso direto à API REST do n8n:
// criar, corrigir, ativar, executar workflows e ler erros de execução.
//
// IMPORTANTE: a chamada HTTP de baixo nível (n8nRequest) já existe em
// automation.go com esta assinatura:
//   func n8nRequest(method, url, apiKey string, body []byte) ([]byte, error)
// Este arquivo NÃO redeclara essa função — só a reaproveita.
//
// Segurança:
// - N8N_API_KEY nunca é logada nem devolvida no resultado da tool.
// - Antes de qualquer PATCH ou ACTIVATE, o workflow atual é salvo em backup
//   local (/root/hokma/backend/n8n_backups/) para permitir rollback manual
//   ou automático caso o Hok quebre algo em produção.
// - n8nRequest original não expõe o status HTTP, então erros de negócio do
//   n8n (ex: workflow inválido) aparecem dentro do próprio corpo da resposta
//   (campo "message" ou "error"). Cada tool abaixo checa isso.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const n8nBaseURLDefault = "https://n8n.imoveischaves.com/api/v1"

// ---------- infraestrutura interna ----------

func n8nBaseURL() string {
	if v := os.Getenv("N8N_BASE_URL"); v != "" {
		return v
	}
	return n8nBaseURLDefault
}

func n8nAPIKeyFromEnv() string {
	return os.Getenv("N8N_API_KEY")
}

// n8nCall monta a URL completa, serializa o body em JSON e chama a n8nRequest
// já existente em automation.go. Devolve o corpo bruto da resposta.
func n8nCall(method, path string, payload any) ([]byte, error) {
	key := n8nAPIKeyFromEnv()
	if key == "" {
		return nil, fmt.Errorf("N8N_API_KEY não configurada no ambiente")
	}

	var body []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("erro ao serializar payload: %w", err)
		}
		body = b
	}

	url := n8nBaseURL() + path
	respBody, err := n8nRequest(method, url, key, body)
	if err != nil {
		return nil, fmt.Errorf("erro na chamada ao n8n: %w", err)
	}

	// n8nRequest não expõe status HTTP, então checamos erro de negócio no corpo.
	var probe struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(respBody, &probe) == nil {
		if probe.Message != "" {
			return respBody, fmt.Errorf("n8n retornou erro: %s", probe.Message)
		}
		if probe.Error != "" {
			return respBody, fmt.Errorf("n8n retornou erro: %s", probe.Error)
		}
	}

	return respBody, nil
}

// errJSON formata um erro como resultado de tool em JSON (nunca inclui a API key).
func errJSON(msg string) string {
	out, err := json.Marshal(map[string]string{"status": "error", "error": msg})
	if err != nil || len(out) == 0 {
		return `{"status":"error","error":"falha ao gerar mensagem de erro"}`
	}
	return string(out)
}

func okJSON(result []byte) string {
	log.Printf("[n8n_tools] resposta crua do n8n (%d bytes): %s", len(result), truncateForLog(result))
	if json.Valid(result) {
		out, err := json.Marshal(map[string]any{
			"status": "ok",
			"result": json.RawMessage(result),
		})
		if err == nil {
			return string(out)
		}
		log.Printf("[n8n_tools] falha ao serializar resposta valida: %v", err)
	} else {
		log.Printf("[n8n_tools] resposta do n8n NAO e JSON valido")
	}
	// Fallback seguro: nunca devolve string vazia para o agent loop.
	out, _ := json.Marshal(map[string]any{
		"status": "ok",
		"raw":    string(result),
	})
	if len(out) == 0 {
		return `{"status":"error","error":"resposta vazia ou invalida do n8n"}`
	}
	return string(out)
}

func truncateForLog(b []byte) string {
	s := string(b)
	if len(s) > 300 {
		return s[:300] + "...(truncado)"
	}
	return s
}

// ---------- backup / rollback ----------

const n8nBackupDir = "/root/hokma/backend/n8n_backups"

// n8nMaxBackupsPerWorkflow define quantos backups manter por workflow antes
// de apagar os mais antigos.
const n8nMaxBackupsPerWorkflow = 5

// n8nBackupWorkflow salva o JSON atual de um workflow em disco antes de qualquer alteração,
// e mantém só os N backups mais recentes desse workflow (rotação automática).
// Best-effort: se falhar, não trava a operação principal.
func n8nBackupWorkflow(workflowID string) {
	body, err := n8nCall("GET", "/workflows/"+workflowID, nil)
	if err != nil || body == nil {
		return
	}
	if err := os.MkdirAll(n8nBackupDir, 0o755); err != nil {
		return
	}
	fname := fmt.Sprintf("%s_%d.json", workflowID, time.Now().Unix())
	if err := os.WriteFile(filepath.Join(n8nBackupDir, fname), body, 0o644); err != nil {
		return
	}
	n8nPruneOldBackups(workflowID)
}

// n8nPruneOldBackups apaga os backups mais antigos de um workflow, mantendo
// só os n8nMaxBackupsPerWorkflow mais recentes. Best-effort.
func n8nPruneOldBackups(workflowID string) {
	entries, err := os.ReadDir(n8nBackupDir)
	if err != nil {
		return
	}
	prefix := workflowID + "_"
	var matches []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), prefix) {
			matches = append(matches, e.Name())
		}
	}
	if len(matches) <= n8nMaxBackupsPerWorkflow {
		return
	}
	sort.Strings(matches) // timestamp no nome garante ordem cronologica
	toDelete := matches[:len(matches)-n8nMaxBackupsPerWorkflow]
	for _, f := range toDelete {
		_ = os.Remove(filepath.Join(n8nBackupDir, f))
	}
}

// ---------- filtro de payload ----------

// n8nCleanPayload mantém só os campos que a API do n8n aceita em create/update.
func n8nCleanPayload(raw map[string]any) map[string]any {
	allowed := []string{"name", "nodes", "connections", "settings", "staticData"}
	clean := map[string]any{}
	for _, k := range allowed {
		if v, ok := raw[k]; ok {
			clean[k] = v
		}
	}
	return clean
}

// validateNodesArray verifica que nodes é um array de objetos válidos.
// Retorna erro com detalhes se a estrutura estiver inválida.
func validateNodesArray(nodes any) error {
	nodesArr, ok := nodes.([]any)
	if !ok {
		return fmt.Errorf("nodes deve ser um array, got %T", nodes)
	}
	if len(nodesArr) == 0 {
		return fmt.Errorf("nodes não pode estar vazio")
	}
	for i, n := range nodesArr {
		nodeMap, ok := n.(map[string]any)
		if !ok {
			return fmt.Errorf("nodes[%d] deve ser um objeto, got %T", i, n)
		}
		if nodeMap["name"] == nil {
			return fmt.Errorf("nodes[%d] precisa ter campo 'name'", i)
		}
		if nodeMap["type"] == nil {
			return fmt.Errorf("nodes[%d] precisa ter campo 'type'", i)
		}
	}
	return nil
}

// n8nSummarizeWorkflowResponse resume a resposta de create/update/activate
// (que normalmente devolve o workflow inteiro, com nodes/credenciais/versoes)
// para caber no limite de tokens por minuto do modelo.
func n8nSummarizeWorkflowResponse(body []byte) string {
	var w struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		Active    bool   `json:"active"`
		UpdatedAt string `json:"updatedAt"`
	}
	if err := json.Unmarshal(body, &w); err != nil {
		log.Printf("[n8n_tools] falha ao resumir resposta de workflow, devolvendo bruto: %v", err)
		return okJSON(body)
	}
	out, err := json.Marshal(map[string]any{
		"status":    "ok",
		"id":        w.ID,
		"name":      w.Name,
		"active":    w.Active,
		"updatedAt": w.UpdatedAt,
	})
	if err != nil || len(out) == 0 {
		return `{"status":"error","error":"falha ao montar resumo do workflow"}`
	}
	return string(out)
}

// n8nDiagnoseWorkflow analisa um workflow em busca de problemas conhecidos:
//   - credenciais referenciadas que nao existem mais no servidor (comum apos migracao)
//   - nodes com "parameters": {} vazio em tipos que normalmente tem configuracao
//     (sinal de corrupcao/incompatibilidade de versao, como visto em nodes Telegram
//     salvos com typeVersion nao suportada pela instancia atual)
//
// args (JSON): { "workflowId": "..." }
func n8nDiagnoseWorkflow(args string) string {
	var raw map[string]string
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args invalidos: " + err.Error())
	}
	id := raw["workflowId"]
	if id == "" {
		return errJSON("workflowId e obrigatorio")
	}

	wfBody, err := n8nCall("GET", "/workflows/"+id, nil)
	if err != nil {
		return errJSON("falha ao buscar workflow: " + err.Error())
	}

	var wf struct {
		Name  string `json:"name"`
		Nodes []struct {
			Name        string                 `json:"name"`
			Type        string                 `json:"type"`
			TypeVersion float64                `json:"typeVersion"`
			Parameters  map[string]interface{} `json:"parameters"`
			Credentials map[string]struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"credentials"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(wfBody, &wf); err != nil {
		return errJSON("falha ao interpretar workflow: " + err.Error())
	}

	// nodes que legitimamente tem parameters vazio (nao sao sinal de corrupcao)
	knownEmptyOK := map[string]bool{
		"n8n-nodes-base.manualTrigger": true,
		"n8n-nodes-base.merge":         true,
		"n8n-nodes-base.noOp":          true,
	}

	// busca credenciais existentes no servidor para cruzar referencia
	existingCredIDs := map[string]bool{}
	credBody, credErr := n8nCall("GET", "/credentials", nil)
	if credErr == nil {
		var credList struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if json.Unmarshal(credBody, &credList) == nil {
			for _, c := range credList.Data {
				existingCredIDs[c.ID] = true
			}
		}
	}

	type finding struct {
		Node   string `json:"node"`
		Type   string `json:"type"`
		Issue  string `json:"issue"`
		Detail string `json:"detail"`
	}
	var findings []finding

	for _, node := range wf.Nodes {
		// checagem 1: parametros vazios em node que normalmente tem configuracao
		if len(node.Parameters) == 0 && !knownEmptyOK[node.Type] {
			findings = append(findings, finding{
				Node:   node.Name,
				Type:   node.Type,
				Issue:  "parametros_vazios",
				Detail: "Node sem nenhuma configuracao salva - possivel corrupcao ou incompatibilidade de versao (typeVersion " + fmt.Sprintf("%.1f", node.TypeVersion) + ")",
			})
		}
		// checagem 2: credencial referenciada mas ausente no servidor
		if len(existingCredIDs) > 0 {
			for credType, cred := range node.Credentials {
				if cred.ID != "" && !existingCredIDs[cred.ID] {
					findings = append(findings, finding{
						Node:   node.Name,
						Type:   node.Type,
						Issue:  "credencial_ausente",
						Detail: fmt.Sprintf("Credencial \"%s\" (tipo %s, id %s) nao existe mais no servidor", cred.Name, credType, cred.ID),
					})
				}
			}
		}
	}

	status := "saudavel"
	if len(findings) > 0 {
		status = "problemas_encontrados"
	}

	out, err := json.Marshal(map[string]any{
		"status":       "ok",
		"workflow":     wf.Name,
		"diagnostico":  status,
		"total_issues": len(findings),
		"findings":     findings,
	})
	if err != nil || len(out) == 0 {
		return `{"status":"error","error":"falha ao montar diagnostico"}`
	}
	return string(out)
}

// ===================== TOOLS =====================

// n8nWorkflowSummary e uma versao enxuta do workflow, sem nodes/credenciais/
// versoes antigas, para nao estourar o limite de tokens por minuto da Groq
// quando o resultado da tool volta para o modelo.
type n8nWorkflowSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Active    bool   `json:"active"`
	UpdatedAt string `json:"updatedAt"`
	CreatedAt string `json:"createdAt"`
}

// n8nListWorkflows lista todos os workflows existentes, de forma resumida
// (id, nome, status, datas) para caber no limite de tokens do modelo.
// args (JSON): {} (sem parâmetros por enquanto)
func n8nListWorkflows(args string) string {
	body, err := n8nCall("GET", "/workflows", nil)
	if err != nil {
		return errJSON(err.Error())
	}

	var parsed struct {
		Data []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Active    bool   `json:"active"`
			UpdatedAt string `json:"updatedAt"`
			CreatedAt string `json:"createdAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Printf("[n8n_tools] falha ao resumir lista de workflows, devolvendo bruto: %v", err)
		return okJSON(body)
	}

	summaries := make([]n8nWorkflowSummary, 0, len(parsed.Data))
	for _, w := range parsed.Data {
		summaries = append(summaries, n8nWorkflowSummary{
			ID:        w.ID,
			Name:      w.Name,
			Active:    w.Active,
			UpdatedAt: w.UpdatedAt,
			CreatedAt: w.CreatedAt,
		})
	}

	out, err := json.Marshal(map[string]any{
		"status":    "ok",
		"total":     len(summaries),
		"workflows": summaries,
	})
	if err != nil || len(out) == 0 {
		return `{"status":"error","error":"falha ao montar resumo dos workflows"}`
	}
	return string(out)
}

// n8nGetWorkflowDetail busca o workflow completo e devolve um resumo dos
// nodes com seus parametros (sem o ruido de connections/settings/pinData),
// pra permitir diagnostico de configuracao (ex: URL de um HTTP Request,
// headers, credenciais referenciadas) sem precisar de bash_exec.
func n8nGetWorkflowDetail(args string) string {
	var raw map[string]string
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args invalidos: " + err.Error())
	}
	id := raw["workflowId"]
	if id == "" {
		return errJSON("workflowId e obrigatorio")
	}

	body, err := n8nCall("GET", "/workflows/"+id, nil)
	if err != nil {
		return errJSON(err.Error())
	}

	var parsed struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Nodes []struct {
			Name       string         `json:"name"`
			Type       string         `json:"type"`
			Parameters map[string]any `json:"parameters"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Printf("[n8n_tools] falha ao resumir detalhe do workflow, devolvendo bruto: %v", err)
		return okJSON(body)
	}

	type nodeSummary struct {
		Name       string `json:"name"`
		Type       string `json:"type"`
		Parameters string `json:"parameters,omitempty"`
	}
	const maxParamsLen = 500
	nodes := make([]nodeSummary, 0, len(parsed.Nodes))
	for _, n := range parsed.Nodes {
		paramsJSON := ""
		if len(n.Parameters) > 0 {
			if b, err := json.Marshal(n.Parameters); err == nil {
				paramsJSON = string(b)
				if len(paramsJSON) > maxParamsLen {
					paramsJSON = paramsJSON[:maxParamsLen] + "...(truncado, use bash_exec ou n8n_get_workflow_detail com foco no node especifico para ver completo)"
				}
			}
		}
		nodes = append(nodes, nodeSummary{
			Name:       n.Name,
			Type:       n.Type,
			Parameters: paramsJSON,
		})
	}

	out, err := json.Marshal(map[string]any{
		"status": "ok",
		"id":     parsed.ID,
		"name":   parsed.Name,
		"nodes":  nodes,
	})
	if err != nil || len(out) == 0 {
		return "{\"status\":\"error\",\"error\":\"falha ao montar detalhe do workflow\"}"
	}
	return string(out)
}

// n8nCreateWorkflow cria um novo workflow.
// args (JSON): { "name": "...", "nodes": [...], "connections": {...}, "settings": {...}, "staticData": {...} }
func n8nCreateWorkflow(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args inválidos: " + err.Error())
	}
	payload := n8nCleanPayload(raw)
	if payload["name"] == nil {
		return errJSON("payload precisa ter 'name'")
	}
	if payload["nodes"] == nil {
		return errJSON("payload precisa ter 'nodes'")
	}
	// Validação: nodes deve ser array de objetos
	if err := validateNodesArray(payload["nodes"]); err != nil {
		return errJSON(err.Error())
	}
	payload = n8nRepairConnections(payload)

	body, err := n8nCall("POST", "/workflows", payload)
	if err != nil {
		return errJSON(err.Error())
	}
	return n8nSummarizeWorkflowResponse(body)
}

// n8nUpdateWorkflow corrige um workflow existente (PATCH). Faz backup antes de sobrescrever.
// args (JSON): { "workflowId": "...", "name": "...", "nodes": [...], "connections": {...}, ... }
func n8nUpdateWorkflow(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args inválidos: " + err.Error())
	}
	id, ok := raw["workflowId"].(string)
	if !ok || id == "" {
		return errJSON("workflowId é obrigatório")
	}

	n8nBackupWorkflow(id)

	// Busca o workflow completo atual para preservar campos obrigatorios
	// (position, typeVersion, etc.) que a API PUT exige mas o chamador
	// (LLM) normalmente nao envia ao pedir uma mudanca pontual num node.
	currentBody, cerr := n8nCall("GET", "/workflows/"+id, nil)
	if cerr != nil {
		return errJSON("falha ao buscar workflow atual antes do update: " + cerr.Error())
	}
	var current map[string]any
	if err := json.Unmarshal(currentBody, &current); err != nil {
		return errJSON("falha ao parsear workflow atual: " + err.Error())
	}

	payload := n8nCleanPayload(raw)

	// Normaliza o formato de credentials em cada node: o LLM as vezes
	// manda so a string do ID ({"telegramApi": "abc123"}) mas o n8n exige
	// um objeto {"id": "abc123", "name": "..."}. Busca o nome real no
	// mapa de credenciais existentes no proprio workflow atual, se possivel,
	// senao usa so o id (o n8n aceita name vazio/omitido em alguns casos).
	if nodesRaw, ok := payload["nodes"].([]any); ok {
		for _, n := range nodesRaw {
			nodeMap, ok := n.(map[string]any)
			if !ok {
				continue
			}
			// Corrige o erro comum do LLM de usar "credential" (singular)
			// em vez de "credentials" (plural, com o tipo como chave).
			if singularCred, hasSingular := nodeMap["credential"].(map[string]any); hasSingular {
				credType, _ := singularCred["type"].(string)
				if credType != "" {
					if nodeMap["credentials"] == nil {
						nodeMap["credentials"] = map[string]any{}
					}
					if credsMap, ok := nodeMap["credentials"].(map[string]any); ok {
						credsMap[credType] = map[string]any{
							"id":   singularCred["id"],
							"name": singularCred["name"],
						}
					}
				}
				delete(nodeMap, "credential")
			}

			credsRaw, ok := nodeMap["credentials"].(map[string]any)
			if !ok {
				continue
			}
			for credType, credVal := range credsRaw {
				if idStr, isString := credVal.(string); isString {
					credsRaw[credType] = map[string]any{"id": idStr}
				}
			}
		}
	}

	// Se o chamador enviou "nodes", faz merge por nome: cada node novo
	// sobrescreve so os campos informados, mantendo o resto do node atual
	// (position, typeVersion, credentials, etc.) intacto.
	if newNodesRaw, ok := payload["nodes"]; ok {
		newNodes, _ := newNodesRaw.([]any)
		currentNodes, _ := current["nodes"].([]any)

		currentByName := map[string]map[string]any{}
		for _, cn := range currentNodes {
			cnMap, ok := cn.(map[string]any)
			if !ok {
				continue
			}
			name, _ := cnMap["name"].(string)
			if name != "" {
				currentByName[name] = cnMap
			}
		}

		merged := make([]any, 0, len(currentNodes))
		updatedNames := map[string]bool{}
		for _, nn := range newNodes {
			nnMap, ok := nn.(map[string]any)
			if !ok {
				continue
			}
			name, _ := nnMap["name"].(string)
			if base, exists := currentByName[name]; exists {
				// merge raso: sobrescreve so as chaves informadas pelo chamador
				for k, v := range nnMap {
					base[k] = v
				}
				merged = append(merged, base)
			} else {
				// node novo (nao existia antes) - usa como veio
				merged = append(merged, nnMap)
			}
			if name != "" {
				updatedNames[name] = true
			}
		}
		// adiciona os nodes que nao foram mencionados na chamada, sem alteracao
		for name, cnMap := range currentByName {
			if !updatedNames[name] {
				merged = append(merged, cnMap)
			}
		}
		payload["nodes"] = merged
	} else {
		// nenhum node informado - preserva os nodes atuais como estao
		if cn, ok := current["nodes"]; ok {
			payload["nodes"] = cn
		}
	}

	// connections nunca deve vir do modelo: e uma estrutura interna do n8n
	// que o LLM tende a reconstruir errado (formato objeto em vez de array
	// de arrays). Esta tool e para edicoes pontuais em nodes, nao para
	// reescrever o grafo - por isso sempre usa o valor real do workflow atual.
	if c, ok := current["connections"]; ok {
		payload["connections"] = c
	}
	if _, ok := payload["settings"]; !ok {
		if s, ok := current["settings"]; ok {
			payload["settings"] = s
		}
	}
	if _, ok := payload["name"]; !ok {
		if n, ok := current["name"]; ok {
			payload["name"] = n
		}
	}

	body, err := n8nCall("PUT", "/workflows/"+id, payload)
	if err != nil {
		return errJSON(err.Error())
	}
	return n8nSummarizeWorkflowResponse(body)
}

// n8nActivateWorkflow publica/ativa um workflow em produção. Faz backup antes.
// args (JSON): { "workflowId": "..." }
func n8nActivateWorkflow(args string) string {
	var raw map[string]string
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args inválidos: " + err.Error())
	}
	id := raw["workflowId"]
	if id == "" {
		return errJSON("workflowId é obrigatório")
	}

	n8nBackupWorkflow(id)

	body, err := n8nCall("POST", "/workflows/"+id+"/activate", nil)
	if err != nil {
		return errJSON(err.Error())
	}
	return n8nSummarizeWorkflowResponse(body)
}

// n8nExecuteWorkflow executa um workflow imediatamente e devolve o resultado.
// args (JSON): { "workflowId": "...", "payload": {...} } (payload é opcional, dados de input)
func n8nExecuteWorkflow(args string) string {
	var raw map[string]any
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args inválidos: " + err.Error())
	}
	id, ok := raw["workflowId"].(string)
	if !ok || id == "" {
		return errJSON("workflowId é obrigatório")
	}

	var payload any
	if p, ok := raw["payload"]; ok {
		payload = p
	}

	body, err := n8nCall("POST", "/workflows/"+id+"/run", payload)
	if err != nil {
		return errJSON(err.Error())
	}
	return okJSON(body)
}

// n8nGetExecutionErrors lê as execuções com erro mais recentes de um workflow —
// usado pelo Hok no loop de auto-correção para saber o que consertar.
// args (JSON): { "workflowId": "..." }
func n8nGetExecutionErrors(args string) string {
	var raw map[string]string
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args inválidos: " + err.Error())
	}
	id := raw["workflowId"]
	if id == "" {
		return errJSON("workflowId é obrigatório")
	}

	path := fmt.Sprintf("/executions?workflowId=%s&status=error&limit=10", id)
	body, err := n8nCall("GET", path, nil)
	if err != nil {
		return errJSON(err.Error())
	}

	var parsed struct {
		Data []struct {
			ID         string `json:"id"`
			WorkflowID string `json:"workflowId"`
			Status     string `json:"status"`
			StartedAt  string `json:"startedAt"`
			StoppedAt  string `json:"stoppedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Printf("[n8n_tools] falha ao resumir execucoes com erro, devolvendo bruto: %v", err)
		return okJSON(body)
	}

	type execSummary struct {
		ID           string `json:"id"`
		WorkflowID   string `json:"workflowId"`
		Status       string `json:"status"`
		StartedAt    string `json:"startedAt"`
		StoppedAt    string `json:"stoppedAt"`
		ErrorMessage string `json:"errorMessage,omitempty"`
		ErrorNode    string `json:"errorNode,omitempty"`
	}
	summaries := make([]execSummary, 0, len(parsed.Data))
	for i, e := range parsed.Data {
		// Busca o detalhe completo (com o erro) so da execucao mais recente,
		// pra nao estourar de chamadas: a API do n8n so traz o campo de erro
		// com includeData=true, e isso nao vem na listagem em lote.
		if i == 0 {
			detailPath := fmt.Sprintf("/executions/%s?includeData=true", e.ID)
			detailBody, derr := n8nCall("GET", detailPath, nil)
			if derr == nil {
				var detail struct {
					Data struct {
						ResultData struct {
							Error struct {
								Message string `json:"message"`
								Node    struct {
									Name string `json:"name"`
								} `json:"node"`
							} `json:"error"`
						} `json:"resultData"`
					} `json:"data"`
				}
				jerr := json.Unmarshal(detailBody, &detail)
				if jerr == nil {
					e2 := execSummary{
						ID:           e.ID,
						WorkflowID:   e.WorkflowID,
						Status:       e.Status,
						StartedAt:    e.StartedAt,
						StoppedAt:    e.StoppedAt,
						ErrorMessage: detail.Data.ResultData.Error.Message,
						ErrorNode:    detail.Data.ResultData.Error.Node.Name,
					}
					summaries = append(summaries, e2)
					continue
				}
				log.Printf("[n8n_tools] falha ao parsear detalhe da execucao %s: %v", e.ID, jerr)
			} else {
				log.Printf("[n8n_tools] falha ao buscar detalhe da execucao %s: %v", e.ID, derr)
			}
		}
		summaries = append(summaries, execSummary{
			ID:         e.ID,
			WorkflowID: e.WorkflowID,
			Status:     e.Status,
			StartedAt:  e.StartedAt,
			StoppedAt:  e.StoppedAt,
		})
	}

	out, err := json.Marshal(map[string]any{
		"status":     "ok",
		"total":      len(summaries),
		"executions": summaries,
	})
	if err != nil || len(out) == 0 {
		return `{"status":"error","error":"falha ao montar resumo das execucoes"}`
	}
	return string(out)
}

// n8nRepairConnections cobre o caso em que o modelo monta os nodes
// mas esquece o campo 'connections' (bug observado no MiniMax via
// OpenRouter). Se vier ausente/vazio e houver 2+ nodes, monta uma
// cadeia linear simples ligando os nodes na ordem em que aparecem.
// Nao mexe se ja existir alguma conexao definida.
func n8nRepairConnections(payload map[string]any) map[string]any {
	if _, ok := payload["settings"]; !ok {
		payload["settings"] = map[string]any{}
	}
	existing, hasConn := payload["connections"].(map[string]any)
	if hasConn && len(existing) > 0 {
		return payload
	}
	nodesRaw, ok := payload["nodes"].([]any)
	if !ok || len(nodesRaw) < 2 {
		if _, exists := payload["connections"]; !exists {
			payload["connections"] = map[string]any{}
		}
		return payload
	}
	var names []string
	for _, n := range nodesRaw {
		nodeMap, ok := n.(map[string]any)
		if !ok {
			return payload
		}
		name, ok := nodeMap["name"].(string)
		if !ok || name == "" {
			return payload
		}
		names = append(names, name)
	}
	conns := map[string]any{}
	for i := 0; i < len(names)-1; i++ {
		conns[names[i]] = map[string]any{
			"main": [][]map[string]any{
				{
					{"node": names[i+1], "type": "main", "index": 0},
				},
			},
		}
	}
	payload["connections"] = conns
	log.Printf("[n8n_tools] connections ausente/vazio — reparo automatico aplicado (cadeia linear: %v)", names)
	return payload
}

// n8nDeleteWorkflow deleta um workflow do n8n. Faz backup antes, ja que a
// exclusao pela API do n8n e irreversivel.
// args (JSON): { "workflowId": "..." }
func n8nDeleteWorkflow(args string) string {
	var raw map[string]string
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args inválidos: " + err.Error())
	}
	id := raw["workflowId"]
	if id == "" {
		return errJSON("workflowId é obrigatório")
	}
	n8nBackupWorkflow(id)
	if _, err := n8nCall("DELETE", "/workflows/"+id, nil); err != nil {
		return errJSON(err.Error())
	}
	return fmt.Sprintf(`{"ok":true,"deleted":"%s","backup":"criado antes de deletar"}`, id)
}

// n8nTestWorkflow valida estaticamente um workflow (nodes, connections, uso
// de $env em Code node) sem executa-lo no n8n de fato. Reaproveita
// validateWorkflowJSON — a mesma logica que ja roda em handleAutomationTest.
// args (JSON): { "workflowId": "..." } OU { "workflow_json": {...} }
func n8nTestWorkflow(args string) string {
	var raw struct {
		WorkflowID   string                 `json:"workflowId"`
		WorkflowJSON map[string]interface{} `json:"workflow_json"`
	}
	if err := json.Unmarshal([]byte(args), &raw); err != nil {
		return errJSON("args inválidos: " + err.Error())
	}
	var wfJSON map[string]interface{}
	switch {
	case raw.WorkflowID != "":
		body, err := n8nCall("GET", "/workflows/"+raw.WorkflowID, nil)
		if err != nil {
			return errJSON(err.Error())
		}
		if err := json.Unmarshal(body, &wfJSON); err != nil {
			return errJSON("falha ao decodificar workflow: " + err.Error())
		}
	case raw.WorkflowJSON != nil:
		wfJSON = raw.WorkflowJSON
	default:
		return errJSON("workflowId ou workflow_json é obrigatório")
	}
	b, _ := json.Marshal(validateWorkflowJSON(wfJSON))
	return string(b)
}
