package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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

	// FIX BUG 1: usar GROQ_KEY, não OR_KEY
	groqKey := req.OrKey
	if groqKey == "" {
		groqKey = os.Getenv("GROQ_KEY")
	}
	if groqKey == "" {
		// fallback: ler do .env
		if kb, err := os.ReadFile("/root/hokma/.env"); err == nil {
			for _, line := range strings.Split(string(kb), "\n") {
				if strings.HasPrefix(line, "GROQ_KEY=") {
					groqKey = strings.TrimSpace(strings.TrimPrefix(line, "GROQ_KEY="))
					break
				}
			}
		}
	}

	decisionPrompt := fmt.Sprintf(`Voce e o cerebro do HOK OS. O usuario quer: "%s"

Skills (com descricao, quando usar e prompts de ativacao):
%s
Prefira a skill cujos "Prompts que ativam esta skill" correspondem ao pedido. Responda APENAS com JSON valido sem markdown:
{"skill": "nome_exato_da_skill", "reason": "motivo"}
Se nenhuma skill for adequada: {"skill": "", "reason": "explicacao"}`, req.Task, skillList.String())

	chosen, reason, err := askModelForSkill(groqKey, decisionPrompt)
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
		out, err := exec.Command("bash", "-c", action).CombinedOutput()
		success := err == nil
		output := string(out)
		if !success && output == "" {
			output = err.Error()
		}
		respondJSON(w, TaskResponse{Task: req.Task, SkillUsed: chosen, Output: output, Source: "vps", Success: success, Message: reason})
	}
}

func askModelForSkill(groqKey, prompt string) (string, string, error) {
	if groqKey == "" {
		return "", "", fmt.Errorf("GROQ_KEY nao configurada")
	}

	payload := map[string]interface{}{
		"model": "llama-3.3-70b-versatile",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"temperature": 0.1,
		"max_tokens":  200,
	}
	body, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+groqKey)

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

// trySkillForMessage — chamado pelo /chat para tentar executar uma skill antes do LLM
func trySkillForMessage(userMsg string) (string, string, bool) {
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
	groqKey := GROQ_KEY
	if groqKey == "" {
		if kb, err := os.ReadFile("/root/hokma/.env"); err == nil {
			for _, line := range strings.Split(string(kb), "\n") {
				if strings.HasPrefix(line, "GROQ_KEY=") {
					groqKey = strings.TrimSpace(strings.TrimPrefix(line, "GROQ_KEY="))
					break
				}
			}
		}
	}
	decisionPrompt := fmt.Sprintf(`Voce e o cerebro do HOK OS. O usuario quer: "%s"
Skills disponíveis:
%s
Responda APENAS com JSON sem markdown:
{"skill": "nome_exato_da_skill", "reason": "motivo"}
Se nenhuma skill for adequada: {"skill": "", "reason": "explicacao"}`, userMsg, skillList.String())
	chosen, _, err := askModelForSkill(groqKey, decisionPrompt)
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
	out, err := exec.Command("bash", "-c", action).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("Skill '%s' executada com erro: %s", chosen, string(out)), chosen, true
	}
	return fmt.Sprintf("✅ Skill '%s' executada:\n%s", chosen, strings.TrimSpace(string(out))), chosen, true
}
