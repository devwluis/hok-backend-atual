package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const SKILLS_DIR = "/root/hokma/backend/skills"

type Skill struct {
	Name      string `json:"name"`
	Content   string `json:"content"`
	UpdatedAt int64  `json:"updated_at"`
}

func ensureSkillsDir() {
	os.MkdirAll(SKILLS_DIR, 0755)
}

func listSkills() ([]Skill, error) {
	ensureSkillsDir()
	entries, err := os.ReadDir(SKILLS_DIR)
	if err != nil {
		return nil, err
	}
	var skills []Skill
	for _, e := range entries {
		name := e.Name()
		fullPath := filepath.Join(SKILLS_DIR, name)
		info, _ := e.Info()
		if strings.HasSuffix(name, ".json") {
			data, err := os.ReadFile(fullPath)
			if err != nil {
				continue
			}
			var s Skill
			if json.Unmarshal(data, &s) == nil && s.Name != "" {
				if s.UpdatedAt == 0 {
					s.UpdatedAt = info.ModTime().Unix()
				}
				skills = append(skills, s)
			}
		} else if strings.HasSuffix(name, ".md") {
			data, _ := os.ReadFile(fullPath)
			skills = append(skills, Skill{
				Name:      strings.TrimSuffix(name, ".md"),
				Content:   string(data),
				UpdatedAt: info.ModTime().Unix(),
			})
		}
	}
	return skills, nil
}

func handleListSkills(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	skills, err := listSkills()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if skills == nil {
		skills = []Skill{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"skills": skills})
}

// saveSkillToDisk grava a skill de fato — usado tanto pelo caminho direto
// (skill sem bash) quanto pelo resolver de pending_action (skill com bash,
// apos aprovacao).
func saveSkillToDisk(name, content string) error {
	ensureSkillsDir()
	path := filepath.Join(SKILLS_DIR, name+".md")
	return os.WriteFile(path, []byte(content), 0644)
}

func handleSaveSkill(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	var body struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name e content obrigatorios", 400)
		return
	}

	// Gate: skill com bloco bash exige aprovacao antes de gravar no disco.
	// Motivo: skills sao disparadas sem supervisao humana via triggers.go
	// (runTriggerLoop) e via pipeline/flow (por nome) — nao ha momento de
	// revisao na execucao, entao a gravacao precisa ser o ponto de controle.
	if strings.Contains(body.Content, "```bash") {
		tenantID := tenantIdFromRequest(r)
		convID := r.Header.Get("X-Conversation-Id")
		if convID == "" {
			convID = "default"
		}
		argsJSON, _ := json.Marshal(map[string]string{
			"name":    body.Name,
			"content": body.Content,
		})
		diff := fmt.Sprintf("[NOVA SKILL: %s]\n\n%s", body.Name, extractBashFromContent(body.Content))
		pa := setPendingAction(convID, tenantID, "", "skill_save", string(argsJSON),
			fmt.Sprintf("Salvar skill '%s' com bloco bash executavel", body.Name))
		pa.ActionType = "skill_bash"
		pa.DiffPreview = diff
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":       "pending_approval",
			"pending_id":   pa.ID,
			"diff_preview": diff,
			"note":         "skill contem bash — aprovacao necessaria antes de gravar (POST /actions/approve)",
		})
		return
	}

	if err := saveSkillToDisk(body.Name, body.Content); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": body.Name})
}
