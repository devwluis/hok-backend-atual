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
}

var (
	pendingActionMu  sync.Mutex
	pendingActionCur *PendingAction
)

var mutantTools = map[string]bool{
	"n8n_create_workflow":   true,
	"n8n_update_workflow":   true,
	"n8n_activate_workflow": true,
	"n8n_execute_workflow":  true,
	"n8n_delete_workflow":   true,
	"bash_exec":             true,
}

func isMutantTool(name string) bool { return mutantTools[name] }

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
		return fmt.Sprintf("Vou rodar o comando no servidor: %v", args["cmd"])
	default:
		return fmt.Sprintf("Vou executar a acao '%s'.", name)
	}
}

func setPendingAction(toolName, argsJSON, description string) *PendingAction {
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	pendingActionCur = &PendingAction{
		ID: time.Now().Format("20060102150405"), ToolName: toolName,
		ArgsJSON: argsJSON, Description: description, CreatedAt: time.Now(),
	}
	return pendingActionCur
}

func getPendingAction() *PendingAction {
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	return pendingActionCur
}

func clearPendingAction() {
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	pendingActionCur = nil
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

func resolvePendingAction(approve bool) string {
	pa := getPendingAction()
	if pa == nil {
		return "Nao ha nenhuma acao pendente no momento."
	}
	clearPendingAction()
	if !approve {
		return "Acao cancelada: " + pa.Description
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
	reply := resolvePendingAction(true)
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
	reply := resolvePendingAction(false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"reply": reply})
}
