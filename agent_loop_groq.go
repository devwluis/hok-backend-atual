package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

type chatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []toolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type toolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type toolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Parameters  interface{} `json:"parameters"`
	} `json:"function"`
}

type groqRequest struct {
	Model      string        `json:"model"`
	Messages   []chatMessage `json:"messages"`
	Tools      []toolDef     `json:"tools,omitempty"`
	ToolChoice interface{}   `json:"tool_choice,omitempty"`
}

type groqResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func agentTools() []toolDef {
	readFile := toolDef{Type: "function"}
	readFile.Function.Name = "read_file"
	readFile.Function.Description = "Le o conteudo de um arquivo de texto no servidor pelo caminho absoluto."
	readFile.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Caminho absoluto do arquivo, ex: /root/hokma/backend/main.go",
			},
		},
		"required": []string{"path"},
	}

	bashExec := toolDef{Type: "function"}
	bashExec.Function.Name = "bash_exec"
	bashExec.Function.Description = "Executa um comando bash no servidor e retorna stdout+stderr. Usar com cautela."
	bashExec.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"cmd": map[string]interface{}{
				"type":        "string",
				"description": "Comando shell a ser executado",
			},
		},
		"required": []string{"cmd"},
	}

	n8nList := toolDef{Type: "function"}
	n8nList.Function.Name = "n8n_list_workflows"
	n8nList.Function.Description = "Lista todos os workflows existentes no n8n."
	n8nList.Function.Parameters = map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}

	n8nCreate := toolDef{Type: "function"}
	n8nCreate.Function.Name = "n8n_create_workflow"
	n8nCreate.Function.Description = "Cria um novo workflow no n8n. Envie name, nodes, connections, settings e staticData conforme o formato de exportacao do n8n."
	n8nCreate.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"name":        map[string]interface{}{"type": "string", "description": "Nome do workflow"},
			"nodes":       map[string]interface{}{"type": "array", "description": "Lista de nodes do workflow (formato n8n)"},
			"connections": map[string]interface{}{"type": "object", "description": "Conexoes entre os nodes (formato n8n)"},
			"settings":    map[string]interface{}{"type": "object", "description": "Configuracoes do workflow"},
			"staticData":  map[string]interface{}{"type": "object", "description": "Dados estaticos do workflow"},
		},
		"required": []string{"name", "nodes"},
	}

	n8nUpdate := toolDef{Type: "function"}
	n8nUpdate.Function.Name = "n8n_update_workflow"
	n8nUpdate.Function.Description = "Corrige/atualiza um workflow existente no n8n pelo workflowId. Faz backup automatico antes de sobrescrever."
	n8nUpdate.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId":  map[string]interface{}{"type": "string", "description": "ID do workflow a corrigir"},
			"name":        map[string]interface{}{"type": "string", "description": "Nome do workflow"},
			"nodes":       map[string]interface{}{"type": "array", "description": "Lista de nodes do workflow (formato n8n)"},
			"connections": map[string]interface{}{"type": "object", "description": "Conexoes entre os nodes (formato n8n)"},
			"settings":    map[string]interface{}{"type": "object", "description": "Configuracoes do workflow"},
			"staticData":  map[string]interface{}{"type": "object", "description": "Dados estaticos do workflow"},
		},
		"required": []string{"workflowId"},
	}

	n8nActivate := toolDef{Type: "function"}
	n8nActivate.Function.Name = "n8n_activate_workflow"
	n8nActivate.Function.Description = "Ativa/publica um workflow em producao no n8n. Faz backup automatico antes de ativar."
	n8nActivate.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId": map[string]interface{}{"type": "string", "description": "ID do workflow a ativar"},
		},
		"required": []string{"workflowId"},
	}

	n8nExecute := toolDef{Type: "function"}
	n8nExecute.Function.Name = "n8n_execute_workflow"
	n8nExecute.Function.Description = "EXECUÇÃO REAL de um workflow n8n — dispara o workflow de verdade no n8n, com efeitos colaterais reais (envia mensagens, grava dados, chama APIs externas). Use quando o usuário pedir explicitamente para executar de verdade, rodar agora, disparar ou ativar e rodar. NUNCA use apenas porque o usuário disse 'testar' ou 'testa' — essas palavras sozinhas indicam n8n_test_workflow (validação estática, sem efeito colateral)."
	n8nExecute.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId": map[string]interface{}{"type": "string", "description": "ID do workflow a executar"},
			"payload":    map[string]interface{}{"type": "object", "description": "Dados de entrada opcionais para a execucao"},
		},
		"required": []string{"workflowId"},
	}

	n8nErrors := toolDef{Type: "function"}
	n8nErrors.Function.Name = "n8n_get_execution_errors"
	n8nErrors.Function.Description = "Le as execucoes com erro mais recentes de um workflow no n8n. Use apos executar para diagnosticar falhas antes de corrigir."
	n8nErrors.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId": map[string]interface{}{"type": "string", "description": "ID do workflow a diagnosticar"},
		},
		"required": []string{"workflowId"},
	}

	n8nDiagnose := toolDef{Type: "function"}
	n8nDiagnose.Function.Name = "n8n_diagnose_workflow"
	n8nDiagnose.Function.Description = "Analisa um workflow do n8n em busca de problemas conhecidos: credenciais que nao existem mais no servidor, e nodes com configuracao vazia/corrompida (comum apos migracao de servidor ou incompatibilidade de versao). Use antes de dizer que um workflow esta ok, ou quando o usuario perguntar se ha algum problema."
	n8nDiagnose.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId": map[string]interface{}{"type": "string", "description": "ID do workflow a diagnosticar"},
		},
		"required": []string{"workflowId"},
	}

	n8nDetail := toolDef{Type: "function"}
	n8nDetail.Function.Name = "n8n_get_workflow_detail"
	n8nDetail.Function.Description = "Busca o workflow completo no n8n e devolve a lista de nodes com seus parametros (URL, headers, method, etc). Use quando precisar ver a configuracao exata de um node especifico, como qual URL um HTTP Request esta chamando ou qual header esta configurado, sem precisar de bash_exec."
	n8nDetail.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId": map[string]interface{}{"type": "string", "description": "ID do workflow a inspecionar"},
		},
		"required": []string{"workflowId"},
	}
	n8nExpertLookup := toolDef{Type: "function"}
	n8nExpertLookup.Function.Name = "n8n_expert_lookup"
	n8nExpertLookup.Function.Description = "Consulta a base de conhecimento viva de nodes do n8n via MCP: busca nodes, schema de propriedades, e valida configuracao antes de criar/atualizar workflow. Use antes de criar ou corrigir workflow com nodes incertos (terceiros, Slack, Discord, bancos). Se MCP indisponivel, cai no conhecimento estatico local."
	n8nExpertLookup.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action":   map[string]interface{}{"type": "string", "enum": []string{"search_nodes", "get_node", "validate_node"}, "description": "search_nodes: busca por palavra-chave. get_node: schema completo. validate_node: valida config."},
			"query":    map[string]interface{}{"type": "string", "description": "Termo de busca (search_nodes)"},
			"nodeType": map[string]interface{}{"type": "string", "description": "Tipo exato do node, ex: n8n-nodes-base.slack"},
			"config":   map[string]interface{}{"type": "object", "description": "Config a validar (validate_node)"},
		},
		"required": []string{"action"},
	}
	envDiagnose := toolDef{Type: "function"}
	envDiagnose.Function.Name = "env_diagnose_config"
	envDiagnose.Function.Description = "Analisa o arquivo .env do backend em busca de problemas conhecidos: chaves duplicadas (causa autenticacao inconsistente) e variaveis de URL apontando para localhost/127.0.0.1 (pode sobrescrever endpoints externos silenciosamente). NUNCA expoe valores reais de credenciais, apenas mascarados. Use quando o usuario reportar comportamento inconsistente de autenticacao ou conexao, ou pedir para checar a configuracao do ambiente."
	envDiagnose.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string", "description": "Caminho do arquivo .env (opcional, default /root/hokma/backend/.env)"},
		},
	}

	addImovel := toolDef{Type: "function"}
	addImovel.Function.Name = "add_imovel"
	addImovel.Function.Description = "Adiciona um novo imovel na planilha do Google Sheets (base de conhecimento do CRM), a partir de uma descricao solta fornecida pelo usuario. Use quando o usuario descrever um imovel (nome, bairro, preco, condicoes) pedindo para cadastrar/adicionar/salvar na base."
	addImovel.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"nome":           map[string]interface{}{"type": "string", "description": "Nome do empreendimento/imovel"},
			"bairro":         map[string]interface{}{"type": "string", "description": "Bairro/localizacao"},
			"dormitorios":    map[string]interface{}{"type": "string", "description": "Numero de dormitorios/quartos"},
			"metragem":       map[string]interface{}{"type": "string", "description": "Metragem em m2"},
			"preco_a_partir": map[string]interface{}{"type": "string", "description": "Preco a partir de quanto"},
			"entrega":        map[string]interface{}{"type": "string", "description": "Previsao/data de entrega"},
			"diferenciais":   map[string]interface{}{"type": "string", "description": "Diferenciais e condicoes (financiamento, desconto, lazer, etc)"},
		},
		"required": []string{"nome", "bairro", "preco_a_partir"},
	}
	n8nDelete := toolDef{Type: "function"}
	n8nDelete.Function.Name = "n8n_delete_workflow"
	n8nDelete.Function.Description = "Deleta um workflow do n8n permanentemente. Faz backup automatico antes, mas a exclusao na API do n8n e irreversivel. Use so quando o usuario pedir explicitamente pra remover um workflow."
	n8nDelete.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId": map[string]interface{}{"type": "string", "description": "ID do workflow a deletar"},
		},
		"required": []string{"workflowId"},
	}
	n8nTest := toolDef{Type: "function"}
	n8nTest.Function.Name = "n8n_test_workflow"
	n8nTest.Function.Description = "VALIDAÇÃO ESTÁTICA do JSON de um workflow n8n. NÃO executa nada no n8n. NÃO tem efeito colateral nenhum. Use quando: 'testar', 'testa', 'validar', 'checar estrutura', 'revisar nodes', 'analisar antes de ativar', 'verificar antes de rodar'. NUNCA use quando o usuário quer rodar, disparar ou executar o workflow de verdade."
	n8nTest.Function.Parameters = map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"workflowId":    map[string]interface{}{"type": "string", "description": "ID de um workflow existente no n8n para validar"},
			"workflow_json": map[string]interface{}{"type": "object", "description": "JSON completo de um workflow ainda nao salvo, para validar antes de criar"},
		},
		"required": []string{},
	}
	return []toolDef{readFile, bashExec, n8nList, n8nCreate, n8nUpdate, n8nActivate, n8nExecute, n8nDelete, n8nTest, n8nErrors, n8nDiagnose, n8nDetail, n8nExpertLookup, envDiagnose, addImovel}
}

// IMPORTANTE: bashExecTool abaixo e standalone para este prototipo. No HOK
// real, troque o corpo dela por uma chamada direta a funcao interna que ja
// existe atras do requireHokAuth em fs_routes.go, em vez de duplicar a
// logica de exec - assim mantem uma fonte unica de verdade.

func executeTool(name string, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("erro: argumentos invalidos: %v", err)
	}

	switch name {
	case "read_file":
		path, _ := args["path"].(string)
		return readFileTool(path)
	case "bash_exec":
		cmd, _ := args["cmd"].(string)
		return bashExecTool(cmd)
	case "n8n_list_workflows":
		return n8nListWorkflows(argsJSON)
	case "n8n_create_workflow":
		return n8nCreateWorkflow(argsJSON)
	case "n8n_update_workflow":
		return n8nUpdateWorkflow(argsJSON)
	case "n8n_activate_workflow":
		return n8nActivateWorkflow(argsJSON)
	case "n8n_execute_workflow":
		return n8nExecuteWorkflow(argsJSON)
	case "n8n_get_execution_errors":
		return n8nGetExecutionErrors(argsJSON)
	case "n8n_diagnose_workflow":
		return n8nDiagnoseWorkflow(argsJSON)
	case "n8n_get_workflow_detail":
		return n8nGetWorkflowDetail(argsJSON)
	case "n8n_expert_lookup":
		return n8nExpertLookupDispatch(args)
	case "env_diagnose_config":
		return envDiagnoseConfig(argsJSON)
	case "n8n_delete_workflow":
		return n8nDeleteWorkflow(argsJSON)
	case "n8n_test_workflow":
		return n8nTestWorkflow(argsJSON)
	case "add_imovel":
		nome, _ := args["nome"].(string)
		bairro, _ := args["bairro"].(string)
		dormitorios, _ := args["dormitorios"].(string)
		metragem, _ := args["metragem"].(string)
		preco, _ := args["preco_a_partir"].(string)
		entrega, _ := args["entrega"].(string)
		diferenciais, _ := args["diferenciais"].(string)
		result, err := addImovelToSheet(ImovelData{
			Nome: nome, Bairro: bairro, Dormitorios: dormitorios,
			Metragem: metragem, PrecoAPartir: preco,
			Entrega: entrega, Diferenciais: diferenciais,
		})
		if err != nil {
			return fmt.Sprintf("erro ao adicionar imovel: %v", err)
		}
		return result

	case "claude_code":
		prompt, _ := args["prompt"].(string)
		result, err := callClaudeCodeApproved(prompt)
		if err != nil {
			return fmt.Sprintf("erro ao executar claude code: %v", err)
		}
		return result
	case "agent_loop_edit":
		return execAgentLoopEdit(argsJSON)
	default:
		return fmt.Sprintf("erro: ferramenta desconhecida: %s", name)
	}
}

func readFileTool(path string) string {
	if path == "" {
		return "erro: path vazio"
	}

	lowerPath := strings.ToLower(path)
	blockedPaths := []string{".env", ".keys", "memory.db", "id_rsa", ".pem", "credentials", "secrets", ".ssh/"}
	for _, b := range blockedPaths {
		if strings.Contains(lowerPath, b) {
			logReadFileAttempt(path, "BLOCKED")
			return "erro: leitura bloqueada por politica de seguranca (arquivo sensivel)"
		}
	}
	logReadFileAttempt(path, "READ")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("erro ao ler %s: %v", path, err)
	}
	const maxLen = 8000
	out := string(data)
	if len(out) > maxLen {
		out = out[:maxLen] + "\n...[truncado]"
	}
	return out
}

func bashExecTool(cmdStr string) string {
	if cmdStr == "" {
		return "erro: cmd vazio"
	}

	blocked := []string{
		"rm -rf /",
		"rm -rf /*",
		"> /dev/sd",
		"dd if=",
		"mkfs",
		".env",
		".keys",
		"memory.db",
		":(){ :|:& };:",
	}
	lower := strings.ToLower(cmdStr)
	for _, b := range blocked {
		if strings.Contains(lower, strings.ToLower(b)) {
			logBashExecAttempt(cmdStr, "BLOCKED")
			return "erro: comando bloqueado por politica de seguranca (padrao suspeito detectado)"
		}
	}

	logBashExecAttempt(cmdStr, "EXEC")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", cmdStr)
	cmd.Dir = "/root/hokma/backend"
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
	}
	out, err := cmd.CombinedOutput()
	result := string(out)
	if err != nil {
		result += fmt.Sprintf("\n[exit error: %v]", err)
	}
	const maxLen = 6000
	if len(result) > maxLen {
		result = result[:maxLen] + "\n...[truncado]"
	}
	return result
}

func logReadFileAttempt(path, status string) {
	safe := sanitizeForSQLiteShell(path)
	if len(safe) > 200 {
		safe = safe[:200] + "...[truncado]"
	}
	level := "INFO"
	if status == "BLOCKED" {
		level = "WARN"
	}
	sqliteExecParams(
		`INSERT INTO logs (event, level, source) VALUES (?, ?, 'agent_loop_groq');`,
		fmt.Sprintf("agent_loop read_file [%s]: %s", status, safe), level,
	)
}

func logBashExecAttempt(cmdStr, status string) {
	safe := sanitizeForSQLiteShell(cmdStr)
	if len(safe) > 200 {
		safe = safe[:200] + "...[truncado]"
	}
	level := "INFO"
	if status == "BLOCKED" {
		level = "WARN"
	}
	sqliteExecParams(
		`INSERT INTO logs (event, level, source) VALUES (?, ?, 'agent_loop_groq');`,
		fmt.Sprintf("agent_loop bash_exec [%s]: %s", status, safe), level,
	)
}

func sanitizeForSQLiteShell(s string) string {
	replacer := strings.NewReplacer(
		"\"", "",
		"'", "",
		"`", "",
		"$", "",
		"\\", "",
		"\n", " ",
		"\r", " ",
	)
	return replacer.Replace(s)
}

const (
	groqEndpoint  = "https://openrouter.ai/api/v1/chat/completions"
	maxAgentSteps = 10
)

// CLASSIFICADOR DE INTENCAO - unifica nome literal + heuristica NL.
// Cobre somente tools de leitura/baixo risco. Nunca forca tools de
// mutacao (update/create/delete/activate/execute) porque forcar
// tool_choice so escolhe QUAL tool, nao os argumentos.
// Regras de nome literal (prefixo "literal_") tem maxima precisao
// e vem primeiro na lista - cobre o caso "usa a tool X" nomeado
// direto no prompt (Bug 1). Regras heuristicas de linguagem natural
// vem depois, como fallback.
// Adicionado em 26/07/2026.
type intentRule struct {
	name     string
	keywords []string
	tool     string
	exclude  []string
}

var intentRules = []intentRule{
	// ── Nome literal — maxima precisao ──
	{
		name:     "literal_n8n_list_workflows",
		keywords: []string{"n8n_list_workflows"},
		tool:     "n8n_list_workflows",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "literal_n8n_test_workflow",
		keywords: []string{"n8n_test_workflow"},
		tool:     "n8n_test_workflow",
		exclude:  []string{"n8n_execute_workflow", "bash_exec"},
	},
	{
		name:     "literal_n8n_diagnose_workflow",
		keywords: []string{"n8n_diagnose_workflow"},
		tool:     "n8n_diagnose_workflow",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "literal_n8n_get_execution_errors",
		keywords: []string{"n8n_get_execution_errors"},
		tool:     "n8n_get_execution_errors",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "literal_n8n_get_workflow_detail",
		keywords: []string{"n8n_get_workflow_detail"},
		tool:     "n8n_get_workflow_detail",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "literal_env_diagnose_config",
		keywords: []string{"env_diagnose_config"},
		tool:     "env_diagnose_config",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "literal_n8n_expert_lookup",
		keywords: []string{"n8n_expert_lookup"},
		tool:     "n8n_expert_lookup",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "consultar_node_desconhecido",
		keywords: []string{"que node uso", "qual node", "como configura o node", "schema do node", "propriedades do node"},
		tool:     "n8n_expert_lookup",
		exclude:  []string{"bash_exec"},
	},
	// ── Linguagem natural — fallback ──
	{
		name:     "criar_workflow",
		keywords: []string{"crie um workflow", "criar workflow", "cria um workflow", "criar um novo workflow", "novo workflow chamado", "workflow chamado"},
		tool:     "n8n_create_workflow",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "listar_workflows",
		keywords: []string{"lista", "listar", "quais workflows", "quantos workflows"},
		tool:     "n8n_list_workflows",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "testar_workflow",
		keywords: []string{"testar", "testa ", "validar", "valida "},
		tool:     "n8n_test_workflow",
		exclude:  []string{"n8n_execute_workflow", "bash_exec"},
	},
	{
		name:     "diagnosticar_workflow",
		keywords: []string{"diagnostica", "diagnostico", "algum problema", "esta quebrado"},
		tool:     "n8n_diagnose_workflow",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "erros_execucao",
		keywords: []string{"erros de execucao", "execucoes com erro", "falhas recentes", "por que falhou"},
		tool:     "n8n_get_execution_errors",
		exclude:  []string{"bash_exec"},
	},
	{
		name:     "atualizar_ou_corrigir_workflow",
		keywords: []string{"corrige", "corrigir", "atualiza a credencial", "atualizar a credencial", "troca a credencial", "trocar a credencial", "muda a credencial"},
		tool:     "n8n_get_workflow_detail",
		exclude:  []string{"bash_exec"},
	},
}

func classifyIntent(userPrompt string) *intentRule {
	l := strings.ToLower(userPrompt)

	// Guarda: pedido de criacao e sempre multi-etapa (criar workflow,
	// depois testar, depois ativar) -- nao forca nenhuma tool no
	// primeiro passo, deixa o modelo planejar livremente.
	creationSignals := []string{
		"cria um workflow", "cria workflow", "cria dois workflows",
		"criar workflow", "criar workflows", "novo workflow",
		"cria uma automacao", "criar automacao",
	}
	for _, sig := range creationSignals {
		if strings.Contains(l, sig) {
			// Nao forca nenhuma tool (deixa o modelo planejar livremente,
			// criacao e sempre multi-etapa), mas EXCLUI bash_exec do turno —
			// sem isso, toda intencao de criacao reabre a fuga para bash_exec
			// (ver ADDENDUM 2026-08-08 secao 5.1).
			return &intentRule{
				name:    "criacao_multi_etapa_livre",
				tool:    "",
				exclude: []string{"bash_exec"},
			}
		}
	}
	for i := range intentRules {
		r := &intentRules[i]
		for _, kw := range r.keywords {
			if strings.Contains(l, kw) {
				return r
			}
		}
	}
	return nil
}

// RunAgentLoop executa o ciclo: pergunta ao modelo -> ve se ele pediu
// ferramenta -> executa -> devolve resultado -> pergunta de novo, ate o
// modelo responder em texto puro (sem tool_calls) ou bater o teto de passos.
func RunAgentLoop(ctx context.Context, userPrompt string, mode string, history []Turn, conversationId string, tenantID string) (string, error) {
	if mode == "" {
		mode = "build"
	}
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("OPENROUTER_API_KEY nao definida")
	}
	model := os.Getenv("MINIMAX_AGENT_MODEL")
	if model == "" {
		model = "minimax/minimax-m3"
	}

	tools := agentTools()
	firstStepForcedToolPreview := ""
	if rule := classifyIntent(userPrompt); rule != nil {
		firstStepForcedToolPreview = rule.tool
	}
	baseSystemContent := "Voce e o agente autonomo do HOK. Use as ferramentas " +
		"disponiveis para investigar antes de responder. Quando " +
		"tiver certeza da resposta final, responda em texto puro " +
		"sem chamar nenhuma ferramenta."
	// Quando a tool forcada e a de conhecimento vivo do n8n, NAO injeta o
	// conhecimento estatico no prompt -- isso faz o modelo depender da tool
	// (MCP) em vez de responder direto com o texto ja disponivel no contexto.
	if firstStepForcedToolPreview != "n8n_expert_lookup" {
		baseSystemContent += n8nContextSuffix()
	}
	messages := []chatMessage{
		{
			Role:    "system",
			Content: baseSystemContent,
		},
	}
	for _, h := range history {
		messages = append(messages, chatMessage{Role: h.Role, Content: h.Content})
	}
	messages = append(messages, chatMessage{Role: "user", Content: userPrompt})
	firstStepForcedTool := ""
	promptLower := strings.ToLower(userPrompt)
	if rule := classifyIntent(userPrompt); rule != nil {
		firstStepForcedTool = rule.tool
		for _, ex := range rule.exclude {
			tools = filterOutTool(tools, ex)
		}
		log.Printf("[agent_loop] intent classificado: %s -> forcando tool %s", rule.name, rule.tool)
	}
	if firstStepForcedTool == "" && (strings.Contains(promptLower, "leia o arquivo") || strings.Contains(promptLower, "leia arquivo") || strings.Contains(promptLower, "ler o arquivo") || strings.Contains(promptLower, "mostra o conteudo") || strings.Contains(promptLower, "mostra o conteúdo") || strings.Contains(promptLower, "mostre o conteudo") || strings.Contains(promptLower, "mostre o conteúdo")) {
		firstStepForcedTool = "read_file"
	}
	forcedRetryUsed := false
	consecutiveValidationFails := map[string]int{}
	const maxConsecutiveValidationFails = 2

	for step := 1; step <= maxAgentSteps; step++ {
		var respMsg chatMessage
		var finishReason string
		var err error
		if step == 1 && firstStepForcedTool != "" {
			respMsg, finishReason, err = callGroqAgentLoopForced(ctx, apiKey, model, messages, tools, firstStepForcedTool)
		} else {
			respMsg, finishReason, err = callGroqAgentLoop(ctx, apiKey, model, messages, tools)
		}
		if err != nil {
			return "", fmt.Errorf("passo %d: %w", step, err)
		}

		logCodexVivo(step, respMsg, finishReason)
		saveAgentCheckpoint(step, messages)

		if len(respMsg.ToolCalls) == 0 {
			isEmptyReply := strings.TrimSpace(respMsg.Content) == ""
			if !forcedRetryUsed && (isEmptyReply || looksLikeUnexecutedAction(respMsg.Content)) {
				forcedRetryUsed = true
				forcedToolName := mapNarrationToToolName(respMsg.Content)
				forcedMsg, forcedFinish, forcedErr := callGroqAgentLoopForced(ctx, apiKey, model, messages, tools, forcedToolName)
				if forcedErr == nil && len(forcedMsg.ToolCalls) > 0 {
					respMsg = forcedMsg
					finishReason = forcedFinish
					logCodexVivo(step, respMsg, finishReason)
				} else {
					log.Printf("[agent_loop] retry forcado falhou (tool=%s): err=%v toolCalls=%d finishReason=%s", forcedToolName, forcedErr, len(forcedMsg.ToolCalls), forcedFinish)
					if strings.TrimSpace(respMsg.Content) == "" {
						return "Nao consegui gerar uma resposta para essa acao. Tente descrever o que deseja com mais detalhes.", nil
					}
					return respMsg.Content, nil
				}
			} else {
				return respMsg.Content, nil
			}
		}

		messages = append(messages, respMsg)

		for _, tc := range respMsg.ToolCalls {
			if isMutantTool(tc.Function.Name) {
				if verr := validateArgsBeforePending(tc.Function.Name, tc.Function.Arguments); verr != nil {
					consecutiveValidationFails[tc.Function.Name]++
					if consecutiveValidationFails[tc.Function.Name] >= maxConsecutiveValidationFails {
						log.Printf("[agent_loop] abortando apos %d falhas consecutivas de validacao em %s: %s",
							consecutiveValidationFails[tc.Function.Name], tc.Function.Name, verr.Error())
						return fmt.Sprintf("Nao consegui montar os argumentos corretos para %s depois de %d tentativas (ultimo erro: %s). Tente descrever o workflow com mais detalhes ou construa o JSON manualmente.",
							tc.Function.Name, consecutiveValidationFails[tc.Function.Name], verr.Error()), nil
					}
					messages = append(messages, chatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Name:       tc.Function.Name,
						Content:    validationRetryHint(tc.Function.Name, verr.Error()),
					})
					continue
				}
				consecutiveValidationFails[tc.Function.Name] = 0
				desc := describeMutantAction(tc.Function.Name, tc.Function.Arguments)
				if mode == "plan" {
					return desc + "\n\n(Modo planejar: nenhuma acao foi executada.)", nil
				}
				setPendingAction(conversationId, tenantID, "", tc.Function.Name, tc.Function.Arguments, desc)
				return desc + "\n\nConfirma? (responda sim/nao)", nil
			}
			result := executeTool(tc.Function.Name, tc.Function.Arguments)
			messages = append(messages, chatMessage{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Function.Name,
				Content:    result,
			})
		}
	}

	return "", fmt.Errorf("loop excedeu %d passos sem resposta final", maxAgentSteps)
}

func callGroqAgentLoop(ctx context.Context, apiKey, model string, messages []chatMessage, tools []toolDef) (chatMessage, string, error) {
	reqBody := groqRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return chatMessage{}, "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, groqEndpoint, bytes.NewReader(payload))
	if err != nil {
		return chatMessage{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return chatMessage{}, "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return chatMessage{}, "", err
	}

	var parsed groqResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return chatMessage{}, "", fmt.Errorf("resposta invalida do OpenRouter: %s", string(body))
	}
	if parsed.Error != nil {
		return chatMessage{}, "", fmt.Errorf("erro do OpenRouter: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return chatMessage{}, "", fmt.Errorf("OpenRouter nao retornou choices")
	}

	choice := parsed.Choices[0]
	return choice.Message, choice.FinishReason, nil
}

// Troque o corpo destas duas funcoes por chamadas reais ao seu Codex Vivo e
// ao storage de checkpoint (SHA1 + ESTADO_ANTERIOR) que ja existem no
// pipeline. Deixei como hooks separados pra nao acoplar este arquivo a
// nomes internos que eu nao tenho certeza de bater com o seu codigo.

func logCodexVivo(step int, msg chatMessage, finishReason string) {
	toolNames := make([]string, 0, len(msg.ToolCalls))
	for _, tc := range msg.ToolCalls {
		toolNames = append(toolNames, tc.Function.Name+"("+tc.Function.Arguments+")")
	}
	if len(toolNames) > 0 {
		log.Printf("[agent_loop] passo %d: finish_reason=%s tools=%v", step, finishReason, toolNames)
	} else {
		preview := msg.Content
		if len(preview) > 200 {
			preview = preview[:200] + "...(truncado)"
		}
		log.Printf("[agent_loop] passo %d: finish_reason=%s resposta_final=%q", step, finishReason, preview)
	}
}

func saveAgentCheckpoint(step int, messages []chatMessage) {
	// TODO: reaproveitar o storage SHA1-hash + ESTADO_ANTERIOR do pipeline
	_ = step
	_ = messages
}

type agentLoopToolsRequest struct {
	Prompt string `json:"prompt"`
}

// conversationId dessa rota vem do header/query, não do body —
// ver convIdFromRequest em pending_action.go.

type agentLoopToolsResponse struct {
	Reply string `json:"reply"`
}

func handleAgentLoopTools(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var req agentLoopToolsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Prompt == "" {
		http.Error(w, `{"error":"prompt obrigatorio"}`, 400)
		return
	}
	reply, err := RunAgentLoop(r.Context(), req.Prompt, "build", nil, convIdFromRequest(r), tenantIdFromRequest(r))
	if err != nil {
		respondJSON(w, agentLoopToolsResponse{Reply: "erro: " + err.Error()})
		return
	}
	respondJSON(w, agentLoopToolsResponse{Reply: reply})
}

func n8nContextSuffix() string {
	data, err := os.ReadFile("/root/hokma/backend/knowledge/n8n_expert.md")
	if err != nil || len(data) == 0 {
		return ""
	}
	return "\n\n--- BASE DE CONHECIMENTO N8N ---\n" + string(data)
}

func looksLikeUnexecutedAction(text string) bool {
	l := strings.ToLower(text)
	prefixes := []string{"vou criar", "vou ativar", "vou atualizar", "vou executar", "vou rodar", "vou deletar", "vou remover", "vou apagar"}
	for _, p := range prefixes {
		if strings.Contains(l, p) {
			return true
		}
	}
	approvalMarkers := []string{"aprova a cria", "aprova a ativa", "aprova a atualiza", "aprova a execu", "confirma a cria", "posso prosseguir", "aprova?", "confirma?", "responda sim/nao", "responda sim/não"}
	for _, m := range approvalMarkers {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

func callGroqAgentLoopForced(ctx context.Context, apiKey, model string, messages []chatMessage, tools []toolDef, forcedToolName string) (chatMessage, string, error) {
	forcedCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	reqBody := groqRequest{
		Model:      model,
		Messages:   messages,
		Tools:      tools,
		ToolChoice: buildToolChoice(forcedToolName),
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return chatMessage{}, "", err
	}
	req, err := http.NewRequestWithContext(forcedCtx, http.MethodPost, groqEndpoint, bytes.NewReader(payload))
	if err != nil {
		return chatMessage{}, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return chatMessage{}, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return chatMessage{}, "", err
	}
	var parsed groqResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return chatMessage{}, "", fmt.Errorf("resposta invalida (forced): %s", string(body))
	}
	if parsed.Error != nil {
		return chatMessage{}, "", fmt.Errorf("erro do OpenRouter (forced): %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return chatMessage{}, "", fmt.Errorf("OpenRouter nao retornou choices (forced)")
	}
	choice := parsed.Choices[0]
	return choice.Message, choice.FinishReason, nil
}

func filterOutTool(tools []toolDef, excludeName string) []toolDef {
	filtered := make([]toolDef, 0, len(tools))
	for _, t := range tools {
		if t.Function.Name != excludeName {
			filtered = append(filtered, t)
		}
	}
	return filtered
}

// validationRetryHint enriquece a mensagem de erro devolvida ao modelo no
// retry de validacao de tools de workflow, ensinando o formato correto de
// "nodes" — reduz as iteracoes do modelo ate o teto de maxAgentSteps quando
// a corrupcao do minimax-m3 (ex: nodes: [""]) se repete.
func validationRetryHint(toolName, errMsg string) string {
	if toolName != "n8n_create_workflow" && toolName != "n8n_update_workflow" {
		return errMsg
	}
	return errMsg +
		"\n\nFormato correto de 'nodes': lista de objetos JSON, cada um com \"name\" (string), " +
		"\"type\" (ex: \"n8n-nodes-base.webhook\"), \"typeVersion\" (numero) e \"parameters\" (objeto). " +
		"Exemplo: {\"name\": \"Webhook\", \"type\": \"n8n-nodes-base.webhook\", \"typeVersion\": 2, " +
		"\"parameters\": {\"path\": \"meu-webhook\", \"httpMethod\": \"POST\"}, \"position\": [250, 300]}"
}

func buildToolChoice(toolName string) interface{} {
	if toolName == "" {
		return "required"
	}
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name": toolName,
		},
	}
}

func mapNarrationToToolName(text string) string {
	l := strings.ToLower(text)
	switch {
	case strings.Contains(l, "criar"), strings.Contains(l, "ser criado"), strings.Contains(l, "criação"):
		return "n8n_create_workflow"
	case strings.Contains(l, "ativar"), strings.Contains(l, "ativação"):
		return "n8n_activate_workflow"
	case strings.Contains(l, "atualizar"), strings.Contains(l, "atualização"):
		return "n8n_update_workflow"
	case strings.Contains(l, "executar"), strings.Contains(l, "execução"), strings.Contains(l, "rodar"):
		return "n8n_execute_workflow"
	case strings.Contains(l, "deletar"), strings.Contains(l, "remover"), strings.Contains(l, "excluir"):
		return "n8n_delete_workflow"
	case strings.Contains(l, "testar"), strings.Contains(l, "validar"):
		return "n8n_test_workflow"
	default:
		return ""
	}
}
