package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"os"
	"regexp"
)

type PendingAction struct {
	ID          string    `json:"id"`
	ToolName    string    `json:"tool_name"`
	ArgsJSON    string    `json:"-"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	ActionType  string    `json:"action_type"` // "n8n" | "self_mod"
	TenantID    string    `json:"tenant_id"`
	DiffPreview string    `json:"diff_preview,omitempty"`
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
func tenantIdFromRequest(r *http.Request) string {
	tokenStr := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	tokenStr = strings.TrimSpace(tokenStr)
	if tokenStr == "" {
		return "owner"
	}
	claims, err := parseJWT(tokenStr)
	if err != nil {
		return "owner"
	}
	tid, _ := claims["tenant_id"].(string)
	if tid == "" {
		return "owner"
	}
	return tid
}

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
// generateSelfModDiff tenta prever o que vai mudar no arquivo alvo
// antes de aprovar uma automodificacao. Retorna diff em texto plano.
func generateSelfModDiff(toolName, argsJSON string) string {
	if toolName != "bash_exec" {
		return ""
	}
	var args map[string]interface{}
	json.Unmarshal([]byte(argsJSON), &args)
	cmd, _ := args["cmd"].(string)
	
	// Detectar arquivo alvo
	var targetFile string
	parts := strings.Fields(cmd)
	for i, p := range parts {
		if strings.HasPrefix(p, "/") && (strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx") || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".json") || strings.HasSuffix(p, ".md") || strings.HasSuffix(p, ".sh")) {
			targetFile = p
			break
		}
		if i > 0 && (parts[i-1] == ">" || parts[i-1] == ">>" || parts[i-1] == "tee" || parts[i-1] == "mv" || parts[i-1] == "cp") {
			targetFile = p
			break
		}
	}
	if targetFile == "" {
		return "[AVISO] Nao consegui identificar o arquivo alvo do comando. Aprovar com cautela."
	}
	
	// Ler conteudo atual
	before, err := os.ReadFile(targetFile)
	if err != nil {
		return "[AVISO] Arquivo alvo nao encontrado ou nao legivel: " + targetFile
	}
	
	// Simular mudanca para comandos comuns
	var after []byte
	var explanation string
	
	// sed -i
	if strings.Contains(cmd, "sed -i") || strings.Contains(cmd, "sed -e") {
		m := regexp.MustCompile(`s/([^/]+)/([^/]+)/g?`).FindStringSubmatch(cmd)
		if len(m) >= 3 {
			pattern, replacement := m[1], m[2]
			after = []byte(strings.ReplaceAll(string(before), pattern, replacement))
			explanation = fmt.Sprintf("Substituir '%s' por '%s'", pattern, replacement)
		} else {
			return "[AVISO] Comando sed detectado mas padrao nao reconhecido: " + cmd
		}
	} else if strings.Contains(cmd, "cat >") || strings.Contains(cmd, "cat >>") {
		if idx := strings.Index(cmd, "<<"); idx != -1 {
			return "[AVISO] Comando 'cat' com heredoc detectado. O arquivo sera sobrescrito ou anexado. Verifique o comando com atencao."
		}
		if strings.HasPrefix(cmd, `echo "`) || strings.HasPrefix(cmd, "echo '") {
			quote := cmd[5]
			endIdx := strings.Index(cmd[6:], string(quote))
			if endIdx != -1 {
				content := cmd[6 : 6+endIdx]
				if strings.Contains(cmd, ">>") {
					after = append(before, []byte(content+"\n")...)
					explanation = "Anexar ao final do arquivo"
				} else {
					after = []byte(content + "\n")
					explanation = "Sobrescrever arquivo completamente"
				}
			}
		}
	} else if strings.Contains(cmd, "python3") || strings.Contains(cmd, "python ") {
		return "[AVISO] Script Python detectado. Nao e possivel prever diff estaticamente. Aprovar com cautela."
	} else {
		return "[AVISO] Comando nao reconhecido para diff automatico: " + cmd[:min(len(cmd), 80)]
	}
	
	// Gerar diff simples
	beforeLines := strings.Split(string(before), "\n")
	afterLines := strings.Split(string(after), "\n")
	var diff strings.Builder
	diff.WriteString(fmt.Sprintf("=== ARQUIVO: %s ===\n", targetFile))
	if explanation != "" {
		diff.WriteString(fmt.Sprintf("=== ACAO: %s ===\n\n", explanation))
	}
	
	maxLines := len(beforeLines)
	if len(afterLines) > maxLines {
		maxLines = len(afterLines)
	}
	changed := false
	for i := 0; i < maxLines; i++ {
		b := ""
		if i < len(beforeLines) {
			b = beforeLines[i]
		}
		a := ""
		if i < len(afterLines) {
			a = afterLines[i]
		}
		if b != a {
			if !changed {
				diff.WriteString("--- LINHAS ALTERADAS ---\n")
				changed = true
			}
			diff.WriteString(fmt.Sprintf("- %s\n", b))
			diff.WriteString(fmt.Sprintf("+ %s\n", a))
		}
	}
	if !changed {
		diff.WriteString("[Nenhuma mudanca detectada — o padrao pode nao ter sido encontrado no arquivo]\n")
	}
	return diff.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

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
			diff := generateSelfModDiff(name, argsJSON)
			if diff != "" {
				return fmt.Sprintf("[AUTOMODIFICACAO — REVISE O DIFF]\n\nComando: %s\n\n%s\n\nConfirma esta alteracao no codigo?", cmd, diff)
			}
			return fmt.Sprintf("[AUTOMODIFICACAO] Vou executar no servidor: %s", cmd)
		}
		return fmt.Sprintf("Vou rodar o comando no servidor: %s", cmd)
	default:
		return fmt.Sprintf("Vou executar a acao '%s'.", name)
	}
}

func setPendingAction(convId, tenantID, toolName, argsJSON, description string) *PendingAction {
	if convId == "" {
		convId = defaultConvId
	}
	actionType := "n8n"
	if detectSelfModification(toolName, argsJSON) {
		actionType = "self_mod"
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	diffPreview := ""
	if actionType == "self_mod" {
		diffPreview = generateSelfModDiff(toolName, argsJSON)
	}
	pa := &PendingAction{
		ID: time.Now().Format("20060102150405"), ToolName: toolName,
		ArgsJSON: argsJSON, Description: description, CreatedAt: time.Now(),
		ActionType: actionType, TenantID: tenantID, DiffPreview: diffPreview,
	}
	key := tenantID + ":" + convId
	pendingActionMap[key] = pa
	return pa
}

func getPendingAction(convId, tenantID string) *PendingAction {
	if convId == "" {
		convId = defaultConvId
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	key := tenantID + ":" + convId
	return pendingActionMap[key]
}

func clearPendingAction(convId, tenantID string) {
	if convId == "" {
		convId = defaultConvId
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	key := tenantID + ":" + convId
	delete(pendingActionMap, key)
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

func resolvePendingAction(convId, tenantID string, approve bool) string {
	pa := getPendingAction(convId, tenantID)
	if pa == nil {
		return "Nao ha nenhuma acao pendente no momento."
	}
	clearPendingAction(convId, tenantID)
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
	tenantID := tenantIdFromRequest(r)
	reply := resolvePendingAction(convId, tenantID, true)
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
	tenantID := tenantIdFromRequest(r)
	reply := resolvePendingAction(convId, tenantID, false)
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
