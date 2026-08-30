package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
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
	DiffPreview string    `json:"diff_preview,omitempty"`
}

var (
	pendingActionMu  sync.Mutex
	pendingActionMap = map[string]*PendingAction{}
)

const defaultConvId = "default"

// approvedCommandTimeout limita a duracao de comandos aprovados
// (ExecuteApprovedCommand / resolveTaskAgentPendingAction) para evitar
// hangs infinitos apos a aprovacao (varredura 12/08, item 10).
const approvedCommandTimeout = 120 * time.Second

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

// userIdFromRequest extracts user_id (sub) from JWT.
func userIdFromRequest(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		if parts := strings.SplitN(auth, " ", 2); len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
			if claims, err := parseJWT(parts[1]); err == nil {
				if sub, ok := claims["sub"].(string); ok && sub != "" {
					return sub
				}
			}
		}
	}
	return "anonymous"
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

func setPendingAction(convId, tenantID, userID, toolName, argsJSON, description string) *PendingAction {
	if convId == "" {
		convId = defaultConvId
	}
	if userID == "" {
		userID = "anonymous"
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
		// FIX 16/08: ID unico por acao (UnixNano) — antes era timestamp de
		// 1s de granularidade: duas acoes no mesmo segundo colidiam e uma
		// sobrescrevia o staging (pendingExecCommands) da outra.
		ID: fmt.Sprintf("%s_%d", time.Now().Format("20060102150405"), time.Now().UnixNano()), ToolName: toolName,
		ArgsJSON: argsJSON, Description: description, CreatedAt: time.Now(),
		ActionType: actionType, TenantID: tenantID, DiffPreview: diffPreview,
	}
	key := tenantID + ":" + userID + ":" + convId
	pendingActionMap[key] = pa
	savePendingAction(key, pa)
	return pa
}

func getPendingAction(convId, tenantID, userID string) *PendingAction {
	if convId == "" {
		convId = defaultConvId
	}
	if userID == "" {
		userID = "anonymous"
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	key := tenantID + ":" + userID + ":" + convId
	pa := pendingActionMap[key]
	// TTL: expire after 30 minutes
	if pa != nil && time.Since(pa.CreatedAt) > 30*time.Minute {
		delete(pendingActionMap, key)
		log.Printf("[AUDIT] PendingAction expired actionID=%s key=%s", pa.ID, key)
		return nil
	}
	return pa
}

func clearPendingAction(convId, tenantID, userID string) {
	if convId == "" {
		convId = defaultConvId
	}
	if userID == "" {
		userID = "anonymous"
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	key := tenantID + ":" + userID + ":" + convId
	delete(pendingActionMap, key)
	deletePendingActionDB(key)
}

// consumePendingAction le e REMOVE a pending action de forma ATOMICA
// (unica aquisicao do lock). FIX 16/08 (race TOCTOU): antes, get e clear
// eram operacoes separadas — duas mensagens de aprovacao quase simultaneas
// podiam passar o gate e executar o mesmo comando DUAS vezes.
func consumePendingAction(convId, tenantID, userID string) *PendingAction {
	if convId == "" {
		convId = defaultConvId
	}
	if userID == "" {
		userID = "anonymous"
	}
	pendingActionMu.Lock()
	defer pendingActionMu.Unlock()
	key := tenantID + ":" + userID + ":" + convId
	pa := pendingActionMap[key]
	if pa == nil {
		return nil
	}
	if time.Since(pa.CreatedAt) > 30*time.Minute {
		delete(pendingActionMap, key)
		log.Printf("[AUDIT] PendingAction expired actionID=%s key=%s", pa.ID, key)
		return nil
	}
	delete(pendingActionMap, key)
	deletePendingActionDB(key)
	return pa
}

func isApprovalText(msg string) bool {
	l := strings.ToLower(strings.TrimSpace(msg))
	// FIX 16/08 (falso positivo): exige a PALAVRA de aprovacao exata,
	// com pontuacao simples opcional. Mensagens conversacionais que
	// apenas COMECAM com "ok "/"sim " (ex.: "ok, mas antes me explica X")
	// NAO aprovam mais nada por engano.
	l = strings.TrimRight(l, "!.,;?")
	l = strings.TrimSpace(l)
	for _, w := range []string{"sim", "confirma", "confirmo", "pode", "aprova", "aprovado", "aprovada", "aprovo", "ok", "manda", "vai"} {
		if l == w {
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

func resolvePendingAction(ctx context.Context, convId, tenantID, userID string, approve bool) string {
	// FIX 16/08: consume atômico (get+clear) — elimina dupla execucao
	// quando a mesma aprovacao chega 2-3x quase simultanea.
	pa := consumePendingAction(convId, tenantID, userID)
	if pa == nil {
		return "Nao ha nenhuma acao pendente no momento."
	}

	if !approve {
		log.Printf("[AUDIT] Acao REJEITADA actionID=%s tool=%s tenant=%s conv=%s",
			pa.ID, pa.ToolName, tenantID, convId)
		// Permissão do opencode serve: o reject também responde a permission
		// no servidor (senão a tool ficaria pendente até o TTL).
		if pa.ToolName == "opencode_serve_perm" {
			return resolveOpenCodeServePermPendingAction(pa, convId, tenantID, userID, false)
		}
		return "Acao cancelada: " + pa.Description
	}

	log.Printf("[AUDIT] Acao APROVADA actionID=%s tool=%s tenant=%s conv=%s",
		pa.ID, pa.ToolName, tenantID, convId)

	if pa.ActionType == "self_mod" {
		return executeSelfMod(pa)
	}

	switch pa.ToolName {
	case "fs_exec", "bash_exec":
		return resolveFsExecPendingAction(pa)
	case "claude_code":
		return resolveClaudeCodePendingAction(ctx, pa)
	case "opencode":
		return resolveOpenCodePendingAction(ctx, pa, convId, tenantID, userID)
	case "opencode_serve":
		return resolveOpenCodeServePendingAction(pa, convId, tenantID, userID)
	case "opencode_serve_perm":
		return resolveOpenCodeServePermPendingAction(pa, convId, tenantID, userID, true)
	case "self_mod":
		return executeSelfMod(pa)
	case "skill_save":
		return resolveSkillSavePendingAction(pa)
	case "task_agent":
		return resolveTaskAgentPendingAction(pa)
	case "autopatch":
		return resolveAutopatchPendingAction(pa)
	default:
		result := executeTool(ctx, pa.ToolName, pa.ArgsJSON)
		return "Executado: " + pa.Description + "\n\nResultado:\n" + result
	}
}

// === Gate de seguranca: Skill com bash — grava apenas apos aprovacao ===
func resolveSkillSavePendingAction(pa *PendingAction) string {
	var args struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(pa.ArgsJSON), &args); err != nil {
		return fmt.Sprintf("Erro ao decodificar dados da skill: %v", err)
	}
	if err := saveSkillToDisk(args.Name, args.Content); err != nil {
		log.Printf("[AUDIT] ERRO ao salvar skill aprovada name=%s err=%v", args.Name, err)
		return fmt.Sprintf("Erro ao salvar skill: %v", err)
	}
	log.Printf("[AUDIT] Skill salva apos aprovacao: name=%s", args.Name)
	return fmt.Sprintf("Skill '%s' salva com sucesso apos aprovacao.", args.Name)
}

func resolveTaskAgentPendingAction(pa *PendingAction) string {
	action := pa.ArgsJSON
	ctx, cancel := context.WithTimeout(context.Background(), approvedCommandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "bash", "-c", action).CombinedOutput()
	output := string(out)
	success := err == nil
	if !success && output == "" {
		output = err.Error()
	}
	log.Printf("[AUDIT] task_agent aprovado e executado — action_id=%s success=%v\n%s", pa.ID, success, output)
	if success {
		return fmt.Sprintf("Comando executado com sucesso.\n\n%s", output)
	}
	return fmt.Sprintf("Comando falhou.\n\n%s", output)
}

func resolveAutopatchPendingAction(pa *PendingAction) string {
	var req AutopatchReq
	if err := json.Unmarshal([]byte(pa.ArgsJSON), &req); err != nil {
		return fmt.Sprintf("Erro ao decodificar request de autopatch: %v", err)
	}
	log.Printf("[AUDIT] autopatch aprovado - action_id=%s task=%q files=%v", pa.ID, req.Task, req.Files)
	return executeAutopatch(req)
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
	userID := userIdFromRequest(r)
	reply := resolvePendingAction(r.Context(), convId, tenantID, userID, true)
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
	userID := userIdFromRequest(r)
	reply := resolvePendingAction(r.Context(), convId, tenantID, userID, false)
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
	"n8n_create_workflow":   {"name", "nodes"},
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
	if toolName == "n8n_create_workflow" || toolName == "n8n_update_workflow" {
		// name deve ser string nao vazia (o modelo as vezes manda numero)
		if nameVal, exists := args["name"]; exists && nameVal != nil {
			if s, isStr := nameVal.(string); !isStr || strings.TrimSpace(s) == "" {
				return fmt.Errorf(
					"campo 'name' para %s deve ser uma string nao vazia, recebido tipo invalido — "+
						"acao NAO foi enviada para aprovacao, tool sera re-executada",
					toolName,
				)
			}
		}
		// connections/settings/staticData devem ser objetos; o minimax-m3
		// as vezes gera strings de espaco/tab no lugar (visto em producao)
		for _, f := range []string{"connections", "settings", "staticData"} {
			if v, exists := args[f]; exists && v != nil {
				if _, isObj := v.(map[string]interface{}); !isObj {
					return fmt.Errorf(
						"campo '%s' para %s deve ser um objeto, recebido tipo invalido — "+
							"acao NAO foi enviada para aprovacao, tool sera re-executada",
						f, toolName,
					)
				}
			}
		}
		if nodesRaw, exists := args["nodes"]; exists {
			nodesArr, isArr := nodesRaw.([]interface{})
			if !isArr {
				return fmt.Errorf(
					"campo 'nodes' para %s deve ser uma lista (array) de objetos, recebido tipo invalido — "+
						"acao NAO foi enviada para aprovacao, tool sera re-executada",
					toolName,
				)
			}
			if len(nodesArr) == 0 {
				return fmt.Errorf(
					"campo 'nodes' para %s esta vazio — um workflow precisa de pelo menos 1 node — "+
						"acao NAO foi enviada para aprovacao, tool sera re-executada",
					toolName,
				)
			}
			for idx, item := range nodesArr {
				nodeMap, isObj := item.(map[string]interface{})
				if !isObj {
					return fmt.Errorf(
						"campo 'nodes[%d]' para %s deve ser um objeto, recebido tipo invalido — "+
							"nodes deve ser uma lista de objetos, nao strings ou outros tipos — "+
							"acao NAO foi enviada para aprovacao, tool sera re-executada",
						idx, toolName,
					)
				}
				// cada node precisa de name e type como strings nao vazias,
				// senao o n8n rejeita o payload inteiro na criacao
				nodeName, _ := nodeMap["name"].(string)
				nodeType, _ := nodeMap["type"].(string)
				if strings.TrimSpace(nodeName) == "" || strings.TrimSpace(nodeType) == "" {
					return fmt.Errorf(
						"node 'nodes[%d]' para %s precisa de 'name' e 'type' como strings nao vazias — "+
							"acao NAO foi enviada para aprovacao, tool sera re-executada",
						idx, toolName,
					)
				}
				// typeVersion deve ser numero (o minimax-m3 as vezes gera string
				// tipo "1.1" — visto em producao); o n8n aceita float/int.
				if tv, exists := nodeMap["typeVersion"]; exists && tv != nil {
					if _, isNum := tv.(float64); !isNum {
						return fmt.Errorf(
							"campo 'typeVersion' do node 'nodes[%d]' para %s deve ser um numero, "+
								"recebido tipo invalido (%T) — "+
								"acao NAO foi enviada para aprovacao, tool sera re-executada",
							idx, toolName, tv,
						)
					}
				}
				// position deve ser um array de 2 numeros [x, y] (o minimax-m3
				// as vezes gera um objeto {"item": ["0","0"]} — visto em producao).
				if pos, exists := nodeMap["position"]; exists && pos != nil {
					posArr, isArr := pos.([]interface{})
					if !isArr || len(posArr) != 2 {
						return fmt.Errorf(
							"campo 'position' do node 'nodes[%d]' para %s deve ser um array de 2 numeros [x, y], "+
								"recebido tipo invalido — "+
								"acao NAO foi enviada para aprovacao, tool sera re-executada",
							idx, toolName,
						)
					}
					for _, c := range posArr {
						if _, isNum := c.(float64); !isNum {
							return fmt.Errorf(
								"campo 'position' do node 'nodes[%d]' para %s deve conter apenas numeros, "+
									"recebido valor nao numerico — "+
									"acao NAO foi enviada para aprovacao, tool sera re-executada",
								idx, toolName,
							)
						}
					}
				}
			}
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

// === FASE 2b: Execucao Bash Aprovada ===
var pendingExecCommands = make(map[string]string)
var pendingExecMu sync.Mutex

func ExecuteApprovedCommand(actionID string, command string) (string, error) {
	pendingExecMu.Lock()
	pendingExecCommands[actionID] = command
	pendingExecMu.Unlock()
	log.Printf("[AUDIT] Executando comando aprovado actionID=%s cmd=%q", actionID, command)
	ctx, cancel := context.WithTimeout(context.Background(), approvedCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()
	result := string(output)
	if err != nil {
		log.Printf("[AUDIT] ERRO actionID=%s err=%v output=%q", actionID, err, result)
		return result, fmt.Errorf("exec error: %w | output: %s", err, result)
	}
	log.Printf("[AUDIT] SUCESSO actionID=%s output_len=%d", actionID, len(result))
	return result, nil
}

// === FASE 2b: Resolver PendingAction de FS Exec ===
func resolveFsExecPendingAction(action *PendingAction) string {
	// Seguranca (Opcao A'): bash_exec proposto pelo AGENTE so roda chave da
	// allowlist com argv fixo, via exec.Command SEM shell. Nao aceita comando
	// arbitrário: chave fora da lista -> fail-closed, nada e executado.
	// fs_exec (/fs/exec, comando digitado pelo HUMANO na UI) mantem o fluxo
	// bruto aprovado; self-mod nao passa por aqui (vai a executeSelfMod).
	if action.ToolName == "bash_exec" {
		var args struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal([]byte(action.ArgsJSON), &args); err != nil {
			return "❌ bash_exec: argumentos invalidos."
		}
		return bashExecAllowlisted(args.Cmd)
	}

	// FIX 16/08 (bug critico): fallback cmd=action.Description REMOVIDO —
	// nunca executar descricao/mensagem como shell. Fonte do comando:
	// 1) staging em memoria (pendingExecCommands)  2) ArgsJSON persistido
	// (sobrevive a restart)  3) fail-closed.
	pendingExecMu.Lock()
	cmd, ok := pendingExecCommands[action.ID]
	pendingExecMu.Unlock()
	if !ok {
		var args struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal([]byte(action.ArgsJSON), &args); err == nil && strings.TrimSpace(args.Cmd) != "" {
			cmd = args.Cmd
		} else {
			log.Printf("[AUDIT] fs_exec fail-closed actionID=%s: comando staged indisponivel", action.ID)
			return "❌ A acao expirou ou o comando original nao esta mais disponivel (ex.: reinicio do servico). Refaça o pedido."
		}
	}
	output, err := ExecuteApprovedCommand(action.ID, cmd)
	if err != nil {
		return fmt.Sprintf("❌ Erro na execucao: %v\nOutput: %s", err, output)
	}
	return fmt.Sprintf("✅ Comando executado com sucesso.\n\nOutput:\\n%s", output)
}

// === FASE 2b: Resolver PendingAction de Claude Code ===
// FIX 16/08 (bug critico): a acao claude_code SEMPRE executa o prompt
// original armazenado em ArgsJSON via callClaudeCodeApproved (CLI com
// --dangerously-skip-permissions). Nunca usa a mensagem de aprovacao do
// usuario e nunca roda o prompt como bash (ExecuteApprovedCommand). Se o
// prompt nao estiver mais disponivel (ArgsJSON perdido), falha fechado
// (fail-closed) e pede pro usuario refazer o pedido.
func resolveClaudeCodePendingAction(ctx context.Context, action *PendingAction) string {
	var args struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(action.ArgsJSON), &args); err != nil || strings.TrimSpace(args.Prompt) == "" {
		log.Printf("[AUDIT] claude_code fail-closed actionID=%s: prompt original indisponivel", action.ID)
		return "❌ A acao de Claude Code expirou ou o prompt original nao esta mais disponivel. Refaça o pedido."
	}
	log.Printf("[AUDIT] claude_code aprovado actionID=%s prompt_len=%d", action.ID, len(args.Prompt))
	result, err := callClaudeCodeApproved(ctx, args.Prompt)
	if err != nil {
		return fmt.Sprintf("❌ Erro ao executar Claude Code: %v", err)
	}
	return result
}

// === FASE OpenCode: resolver PendingAction de OpenCode ===
// Mesmo padrao do resolveClaudeCodePendingAction: SEMPRE executa o prompt original
// armazenado em ArgsJSON via callOpenCodeApproved (CLI com --auto). NUNCA usa a
// mensagem de aprovacao do usuario como prompt (fail-closed).
func resolveOpenCodePendingAction(ctx context.Context, action *PendingAction, convId, tenantID, userID string) string {
	var args struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal([]byte(action.ArgsJSON), &args); err != nil || strings.TrimSpace(args.Prompt) == "" {
		log.Printf("[AUDIT] opencode fail-closed actionID=%s: prompt original indisponivel", action.ID)
		return "❌ A acao de OpenCode expirou ou o prompt original nao esta mais disponivel. Refaça o pedido."
	}
	log.Printf("[AUDIT] opencode aprovado actionID=%s prompt_len=%d conv=%s tenant=%s",
		action.ID, len(args.Prompt), convId, tenantID)
	result, err := callOpenCodeApproved(ctx, args.Prompt, convId, tenantID, userID)
	if err != nil {
		return fmt.Sprintf("❌ Erro ao executar OpenCode: %v", err)
	}
	return result
}

// resolveOpenCodeServePermPendingAction — responde a permission do opencode
// serve após a decisão do usuário (card de aprovação). Aprovado: once → a
// tool executa e o resultado é devolvido (polling do histórico). Rejeitado:
// reject → a tool é negada.
func resolveOpenCodeServePermPendingAction(action *PendingAction, convId, tenantID, userID string, approve bool) string {
	var args openCodeServePermissionAsked
	if err := json.Unmarshal([]byte(action.ArgsJSON), &args); err != nil || args.ID == "" || args.SessionID == "" {
		log.Printf("[AUDIT] opencode_serve_perm fail-closed actionID=%s: dados da permission indisponiveis", action.ID)
		return "❌ A solicitação de permissão expirou ou não está mais disponível. Refaça o pedido."
	}
	if opencodeServePassword() == "" {
		return "❌ opencode serve não configurado (OPENCODE_SERVE_PASSWORD ausente)."
	}
	c := newOpenCodeServeClient()
	reply := "reject"
	if approve {
		reply = "once"
	}
	if err := c.replyPermission(args.SessionID, args.ID, reply); err != nil {
		return fmt.Sprintf("❌ Erro ao responder a permissão: %v", err)
	}
	verb := "negada"
	if approve {
		verb = "aprovada"
		openCodeServeMarkApproved(args.SessionID) // permissions subsequentes da mesma execução → once automático
	}
	log.Printf("[AUDIT] opencode_serve_perm %s actionID=%s perm=%s cmd=%q conv=%s",
		verb, action.ID, args.Permission, args.Command, convId)
	if !approve {
		return "Permissão negada: `" + args.Command + "`."
	}
	text, err := openCodeServeWaitResult(c, args.SessionID, openCodeServeCardTTL)
	if err != nil {
		return "Permissão aprovada, mas o resultado demorou demais: " + err.Error()
	}
	return text
}
