package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type PendingAction struct {
	ID          string    `json:"id"`
	ToolName    string    `json:"tool_name"`
	ArgsJSON    string    `json:"-"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	ActionType  string    `json:"action_type"` // "n8n" | "self_mod"
	TenantID    string    `json:"tenant_id"`
}

var (
	pendingActionMu  sync.Mutex
	pendingActionMap = map[string]*PendingAction{}
)

const defaultConvId = "default"

// convIdFromRequest extrai o id de conversa da requisição HTTP.
// Ordem de prioridade: header X-Conversation-Id -> query param
// conversation_id -> "default" (fallback pra não quebrar clientes
// que ainda não mandam o id — mas nesse caso volta a ter o mesmo
// risco de concorrência do bug antigo, então o objetivo é migrar
// TODOS os call sites do frontend pra mandar o header).
func convIdFromRequest(r *http.Request) string {
	if id := r.Header.Get("X-Conversation-Id"); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	if id := r.URL.Query().Get("conversation_id"); strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	return defaultConvId
}

var mutantTools = map[string]bool{
	"n8n_create_workflow":   true,
	"n8n_update_workflow":   true,
	"n8n_activate_workflow": true,
	"n8n_execute_workflow":  true,
	"n8n_delete_workflow":   true,
	"read_file":             true,
	"bash_exec":             true,
}

func isMutantTool(name string) bool { return mutantTools[name] }
func detectSelfModification(toolName, argsJSON string) bool {
	if toolName != "bash_exec" {
		return false
	}
	var args map[string]interface{}
	json.Unmarshal([]byte(argsJSON), &args)
	cmd, _ := args["cmd"].(string)
	file, _ := args["file"].(string)
	path, _ := args["path"].(string)
	check := cmd + file + path
	hokPaths := []string{
		"/root/hokma/backend", "/root/hokma/frontend",
		"/var/www/hok-os", "hokma/backend", "hok-os",
		"main.go", "routes.go", "agent_loop", "pending_action",
		"chat_agent", "n8n_tools", "crm_", "smart_chat",
	}
	for _, p := range hokPaths {
		if strings.Contains(check, p) {
			return true
		}
	}
	writeCmds := []string{"sed -i", "sed -e", "echo ", "cat >", "cat >>", "tee ", "mv ", "cp ", "chmod ", "patch "}
	for _, c := range writeCmds {
		if strings.Contains(cmd, c) {
			return true
		}
	}
	return false
}

func describeMutantAction(name, argsJSON string) string {
	var args map[string]interface{}
	json.Unmarshal([]byte(argsJSON), &args)
	switch name {
	case "n8n_activate_workflow":
		return fmt.Sprintf("Vou ativar o workflow %v no n8n.", args["workflowId"])
	case "n8n_create_workflow":
		return fmt.Sprintf("Vou criar um novo workflow no n8n: %v.", args["name"])
	case "n8n_update_workflow":
		return fmt.Sprintf("Vou atualizar o workflow %v no n8n.", args["workflowId"])
	case "n8n_execute_workflow":
		return fmt.Sprintf("Vou EXECUTAR o workflow %v no n8n agora (acao real, nao e so leitura).", args["workflowId"])
	case "n8n_delete_workflow":
		return fmt.Sprintf("Vou DELETAR o workflow %v no n8n (faco backup antes, mas a exclusao e irreversivel).", args["workflowId"])
	case "bash_exec":
		cmd := fmt.Sprintf("%v", args["cmd"])
		if detectSelfModification(name, argsJSON) {
			return fmt.Sprintf("[AUTOMODIFICACAO] Vou executar no servidor: %s", cmd)
		}
		return fmt.Sprintf("Vou rodar o comando no servidor: %s", cmd)
	default:
		return fmt.Sprintf("Vou executar a acao '%s'.", name)
	}
}

func setPendingAction(convId, toolName, argsJSON, description string) *PendingAction {
	if convId == "" {
		convId = defaultConvId
	}
	actionType := "n8n"
	if detectSelfModification(toolName, argsJSON) {
		actionType = "self_mod"
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	pa := &PendingAction{
		ID: time.Now().Format("20060102150405"), ToolName: toolName,
		ArgsJSON: argsJSON, Description: description, CreatedAt: time.Now(),
		ActionType: actionType, TenantID: "owner",
	}
	pendingActionMap[convId] = pa
	return pa
}

func getPendingAction(convId string) *PendingAction {
	if convId == "" {
		convId = defaultConvId
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	return pendingActionMap[convId]
}

func clearPendingAction(convId string) {
	if convId == "" {
		convId = defaultConvId
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	delete(pendingActionMap, convId)
}

func isApprovalText(msg string) bool {
	l := strings.ToLower(strings.TrimSpace(msg))
	for _, w := range []string{"sim", "confirma", "confirmo", "pode", "aprova", "aprovado", "aprovada", "aprovo", "ok", "manda", "vai"} {
		if l == w || strings.HasPrefix(l, w+" ") {
			return true
		}
	}
	return false
}

func isRejectionText(msg string) bool {
	l := strings.ToLower(strings.TrimSpace(msg))
	for _, w := range []string{"nao", "não", "cancela", "cancelar", "para", "rejeita"} {
		if l == w || strings.HasPrefix(l, w+" ") {
			return true
		}
	}
	return false
}

func resolvePendingAction(convId string, approve bool) string {
	pa := getPendingAction(convId)
	if pa == nil {
		return "Nao ha nenhuma acao pendente no momento."
	}
	clearPendingAction(convId)
	if !approve {
		return "Acao cancelada: " + pa.Description
	}
	if pa.ActionType == "self_mod" {
		return executeSelfMod(pa)
	}
	result := executeTool(pa.ToolName, pa.ArgsJSON)
	return "Executado: " + pa.Description + "\n\nResultado:\n" + result
}

func handleActionApprove(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	convId := convIdFromRequest(r)
	reply := resolvePendingAction(convId, true)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"reply": reply})
}

func handleActionReject(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	convId := convIdFromRequest(r)
	reply := resolvePendingAction(convId, false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"reply": reply})
}

// ─────────────────────────────────────────────────────────────────
// GUARDRAIL DETERMINÍSTICO — valida args obrigatórios ANTES de criar
// uma pending_action. Resolve bugs tipo "workflowId: <nil>" na raiz,
// sem depender do modelo "lembrar" de preencher o campo.
// Adicionado em 25/07/2026 apos 3 ocorrencias confirmadas de tool
// calls mutantes com argumentos ausentes/fabricados na mesma sessao.
// ─────────────────────────────────────────────────────────────────
var requiredArgsByTool = map[string][]string{
	"n8n_update_workflow":   {"workflowId"},
	"n8n_delete_workflow":   {"workflowId"},
	"n8n_activate_workflow": {"workflowId"},
	"n8n_test_workflow":     {}, // aceita workflowId OU workflow_json, checado à parte
	"n8n_execute_workflow":  {"workflowId"},
}

func validateArgsBeforePending(toolName, argsJSON string) error {
	required, known := requiredArgsByTool[toolName]
	if !known {
		return nil // tool sem regra definida — não bloqueia por padrão
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Errorf("argsJSON invalido para %s: %w", toolName, err)
	}
	var missing []string
	for _, field := range required {
		v, ok := args[field]
		if !ok || v == nil {
			missing = append(missing, field)
			continue
		}
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) == "" {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"campo(s) obrigatorio(s) ausente(s) ou vazio(s) para %s: %s — "+
				"acao NAO foi enviada para aprovacao, tool sera re-executada",
			toolName, strings.Join(missing, ", "),
		)
	}
	return nil
}
