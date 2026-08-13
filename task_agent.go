package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

var termuxSkills = map[string]bool{
	"Sintetizador de Voz": true,
	"Lanterna":            true,
	"Vibrar Dispositivo":  true,
	"Visao Hokma":         true,
	"Varrer Presenca GPS": true,
	"Sincronizar Corpo":   true,
}

type TaskRequest struct {
	Task  string `json:"task"`
	OrKey string `json:"or_key"`
}

type TaskResponse struct {
	Task      string `json:"task"`
	SkillUsed string `json:"skill_used"`
	Output    string `json:"output"`
	Source    string `json:"source"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
}

func handleTaskAgent(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Task == "" {
		http.Error(w, `{"error":"task obrigatoria"}`, 400)
		return
	}

	skills, err := listSkills()
	if err != nil || len(skills) == 0 {
		respondJSON(w, TaskResponse{Task: req.Task, Success: false, Message: "nenhuma skill disponivel no sistema"})
		return
	}

	var skillList strings.Builder
	for _, s := range skills {
		content := s.Content
		if idx := strings.Index(content, "\n## Acao"); idx > 0 {
			content = content[:idx]
		}
		if idx := strings.Index(content, "\n## Quando usar"); idx >= 0 {
			section := content[idx:]
			if end := strings.Index(section[1:], "\n## "); end >= 0 {
				section = section[:end+1]
			}
			content = section
		} else if len(content) > 300 {
			content = content[:300]
		}
		skillList.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", s.Name, strings.TrimSpace(content)))
	}

	// Seleção de skill via OpenRouter (DeepSeek v4 flash) — sem Groq
	decisionPrompt := fmt.Sprintf(`Voce e o cerebro do HOK OS. O usuario quer: "%s"

Skills (com descricao, quando usar e prompts de ativacao):
%s
Prefira a skill cujos "Prompts que ativam esta skill" correspondem ao pedido. Responda APENAS com JSON valido sem markdown:
{"skill": "nome_exato_da_skill", "reason": "motivo"}
Se nenhuma skill for adequada: {"skill": "", "reason": "explicacao"}`, req.Task, skillList.String())

	chosen, reason, err := askModelForSkill("", decisionPrompt)
	if err != nil || chosen == "" {
		respondJSON(w, TaskResponse{Task: req.Task, Success: false, Message: fmt.Sprintf("nenhuma skill adequada: %s", reason)})
		return
	}

	var selectedSkill *Skill
	for i, s := range skills {
		if strings.EqualFold(s.Name, chosen) || strings.EqualFold(strings.ReplaceAll(s.Name, "_", " "), strings.ReplaceAll(chosen, "_", " ")) {
			selectedSkill = &skills[i]
			break
		}
	}
	if selectedSkill == nil {
		respondJSON(w, TaskResponse{Task: req.Task, Success: false, Message: fmt.Sprintf("skill '%s' nao encontrada", chosen)})
		return
	}

	action := strings.ReplaceAll(extractBashFromContent(selectedSkill.Content), "DESCRICAO_DO_USUARIO", req.Task)
	if action == "" {
		respondJSON(w, TaskResponse{Task: req.Task, SkillUsed: chosen, Success: false, Message: "skill sem bloco bash executavel"})
		return
	}

	isTermux := termuxSkills[selectedSkill.Name]
	if isTermux {
		id, ch := enqueueDeviceCommand(chosen, action)
		select {
		case <-ch:
			result, _ := getDeviceResult(id)
			success := result.Error == ""
			output := result.Output
			if !success {
				output = result.Error
			}
			respondJSON(w, TaskResponse{Task: req.Task, SkillUsed: chosen, Output: output, Source: "device", Success: success, Message: reason})
		case <-time.After(30 * time.Second):
			respondJSON(w, TaskResponse{Task: req.Task, SkillUsed: chosen, Source: "device", Success: false, Message: "timeout — bridge offline"})
		}
	} else {
		diff := fmt.Sprintf("Executar skill '%s' via task agent para: %s\n\n$ %s", chosen, req.Task, action)
		pa := setPendingAction(convIdFromRequest(r), tenantIdFromRequest(r), "", "task_agent", action, diff)
		pa.ActionType = "task_agent_bash"
		pa.DiffPreview = diff
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":       "pending_approval",
			"action_id":    pa.ID,
			"diff_preview": diff,
		})
	}
}

func askModelForSkill(_, prompt string) (string, string, error) {
	orKey := OR_KEY
	if orKey == "" {
		orKey = os.Getenv("OR_KEY")
	}
	if orKey == "" {
		return "", "", fmt.Errorf("OR_KEY nao configurada")
	}

	payload := map[string]interface{}{
		"model": defaultChatModel,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.1,
		"max_tokens":  200,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", OR_URL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+orKey)

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer res.Body.Close()

	resBody, _ := io.ReadAll(res.Body)

	var orResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(resBody, &orResp)

	if len(orResp.Choices) == 0 {
		return askModelForSkillOR(prompt)
	}

	raw := orResp.Choices[0].Message.Content
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var decision struct {
		Skill  string `json:"skill"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		return "", "", fmt.Errorf("parse falhou: %s", raw)
	}
	return decision.Skill, decision.Reason, nil
}

// askModelForSkillOR — fallback OR para skill selection
func askModelForSkillOR(prompt string) (string, string, error) {
	orKey := OR_KEY
	if orKey == "" {
		orKey = os.Getenv("OR_KEY")
	}
	if orKey == "" {
		return "", "", fmt.Errorf("OR_KEY nao configurada")
	}
	payload := map[string]interface{}{
		"model":       "nousresearch/hermes-3-llama-3.1-70b",
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0.1,
		"max_tokens":  200,
	}
	body2, _ := json.Marshal(payload)
	req2, _ := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Authorization", "Bearer "+orKey)
	client2 := &http.Client{Timeout: 30 * time.Second}
	res2, err2 := client2.Do(req2)
	if err2 != nil {
		return "", "", err2
	}
	defer res2.Body.Close()
	b2, _ := io.ReadAll(res2.Body)
	var resp2 struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(b2, &resp2)
	if len(resp2.Choices) == 0 {
		return "", "", fmt.Errorf("OR error: %s", resp2.Error.Message)
	}
	raw2 := strings.TrimSpace(resp2.Choices[0].Message.Content)
	raw2 = strings.TrimPrefix(raw2, "```json")
	raw2 = strings.TrimPrefix(raw2, "```")
	raw2 = strings.TrimSuffix(raw2, "```")
	raw2 = strings.TrimSpace(raw2)
	var dec struct {
		Skill  string `json:"skill"`
		Reason string `json:"reason"`
	}
	if err3 := json.Unmarshal([]byte(raw2), &dec); err3 != nil {
		return "", "", fmt.Errorf("parse OR: %s", raw2)
	}
	return dec.Skill, dec.Reason, nil
}

// FIX BUG 2: "\n" (newline real) em vez de "\\n" (literal)
func extractBashFromContent(content string) string {
	lines := strings.Split(content, "\n")
	inBash := false
	var cmd strings.Builder
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if t == "```bash" {
			inBash = true
			continue
		}
		if inBash && t == "```" {
			break
		}
		if inBash {
			cmd.WriteString(line + "\n")
		}
	}
	return strings.TrimSpace(cmd.String())
}

// isSmallTalkOrIdentity — conversa casual, saudação ou pergunta sobre o próprio
// HOK: nunca deve disparar skill nem pedir aprovação.
func isSmallTalkOrIdentity(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	smallTalk := []string{
		"oi", "olá", "ola", "e aí", "e ai", "opa", "bom dia", "boa tarde", "boa noite",
		"tudo bem", "como você está", "como esta", "como vai", "blz", "beleza",
		"obrigado", "obrigada", "valeu", "vlw", "tchau", "até mais", "ate mais", "flw",
		"quem é você", "quem e voce", "o que você faz", "o que voce faz", "o que você é",
		"o que voce e", "me conte sobre você", "me conte sobre voce", "fale sobre você",
		"quem é vc", "q vc faz", "voce e quem", "você é quem", "quem e vc",
		"me apresenta", "me fala de você", "me fala sobre voce",
	}
	for _, t := range smallTalk {
		if lower == t || strings.HasPrefix(lower, t+" ") || strings.HasPrefix(lower, t+"?") ||
			strings.HasPrefix(lower, t+",") || strings.HasPrefix(lower, t+"!") || strings.HasPrefix(lower, t+".") {
			return true
		}
	}
	// Mensagens curtas sem verbo de ação → conversa normal, não skill
	if len([]rune(lower)) <= 45 {
		actionWords := []string{"ver", "lista", "listar", "cria", "criar", "roda", "rodar",
			"executa", "executar", "checa", "checar", "monitora", "monitorar", "reinicia",
			"reiniciar", "instala", "instalar", "limpa", "limpar", "atualiza", "atualizar",
			"edita", "editar", "mostra", "mostrar", "consulta", "consultar", "gera", "gerar",
			"abre", "abrir", "fecha", "fechar", "pausa", "pausar", "configura", "configurar",
			"testa", "testar", "analisa", "analisar", "diagnostica", "diagnosticar", "busca",
			"buscar", "pesquisa", "pesquisar", "salva", "salvar", "baixa", "baixar", "envia",
			"enviar", "deleta", "deletar", "apaga", "apagar", "remove", "remover", "docker",
			"nginx", "redis", "git ", "cpu", "ram", "disco", "status", "health", "uptime",
			"porta", "log", "memória", "memoria", "backup", "ssl", "firewall", "crédito",
			"credito", "bateria", "celular", "gps", "workflow", "n8n", "skill"}
		for _, a := range actionWords {
			if strings.Contains(lower, a) {
				return false
			}
		}
		return true
	}
	return false
}

// trySkillForMessage — chamado pelo /chat para tentar executar uma skill antes do LLM
func trySkillForMessage(userMsg, convId, tenantID, userID string) (string, string, bool) {
	if isSmallTalkOrIdentity(userMsg) {
		return "", "", false
	}
	skills, err := listSkills()
	if err != nil || len(skills) == 0 {
		return "", "", false
	}
	var skillList strings.Builder
	for _, s := range skills {
		content := s.Content
		if idx := strings.Index(content, "\n## Acao"); idx > 0 {
			content = content[:idx]
		}
		if idx2 := strings.Index(content, "\n## Quando usar"); idx2 >= 0 {
			section := content[idx2:]
			if end := strings.Index(section[1:], "\n## "); end >= 0 {
				section = section[:end+1]
			}
			content = section
		} else if len(content) > 300 {
			content = content[:300]
		}
		skillList.WriteString(fmt.Sprintf("=== %s ===\n%s\n\n", s.Name, strings.TrimSpace(content)))
	}
	decisionPrompt := fmt.Sprintf(`Voce e o cerebro do HOK OS. O usuario quer: "%s"
Skills disponíveis:
%s
REGRAS:
- Conversa casual, saudações e perguntas sobre o proprio HOK (quem e voce, o que faz) NUNCA sao skills — retorne {"skill": ""}.
- Skills sao apenas ACOES de infraestrutura/monitoramento/ferramentas que o usuario quer EXECUTAR agora.
- Nao escolha skill so porque a pergunta menciona um tema parecido; escolha apenas se houver intencao clara de executar a acao.
Responda APENAS com JSON sem markdown:
{"skill": "nome_exato_da_skill", "reason": "motivo"}
Se nenhuma skill for adequada: {"skill": "", "reason": "explicacao"}`, userMsg, skillList.String())
	chosen, _, err := askModelForSkill("", decisionPrompt)
	if err != nil || chosen == "" {
		return "", "", false
	}
	var selectedSkill *Skill
	for i, s := range skills {
		if strings.EqualFold(s.Name, chosen) || strings.EqualFold(strings.ReplaceAll(s.Name, "_", " "), strings.ReplaceAll(chosen, "_", " ")) {
			selectedSkill = &skills[i]
			break
		}
	}
	if selectedSkill == nil {
		return "", "", false
	}
	action := strings.ReplaceAll(extractBashFromContent(selectedSkill.Content), "DESCRICAO_DO_USUARIO", userMsg)
	if action == "" {
		return "", "", false
	}
	diff := fmt.Sprintf("Executar skill '%s' via chat para: %s\n\n$ %s", chosen, userMsg, action)
	pa := setPendingAction(convId, tenantID, userID, "task_agent", action, diff)
	pa.ActionType = "task_agent_bash"
	pa.DiffPreview = diff
	return fmt.Sprintf("⏸️ Skill '%s' identificada. Aprovar execução?\n\n%s\n\n(responda 'sim' para confirmar ou 'não' para cancelar)", chosen, diff), chosen, true
}
