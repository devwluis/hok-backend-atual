package main
import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
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

func handleGetSkill(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/skills/")
	name = strings.Split(name, "/")[0]
	switch r.Method {
	case "GET":
		data, err := os.ReadFile(filepath.Join(SKILLS_DIR, name+".md"))
		if err != nil {
			http.Error(w, "skill nao encontrada", 404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Skill{Name: name, Content: string(data), UpdatedAt: time.Now().Unix()})
	case "DELETE":
		os.Remove(filepath.Join(SKILLS_DIR, name+".md"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, "method not allowed", 405)
	}
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
	ensureSkillsDir()
	path := filepath.Join(SKILLS_DIR, body.Name+".md")
	if err := os.WriteFile(path, []byte(body.Content), 0644); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "name": body.Name})
}

func handleRunSkill(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	parts := strings.Split(r.URL.Path, "/")
	var name string
	for i, p := range parts {
		if p == "skills" && i+2 < len(parts) {
			name = parts[i+1]
			break
		}
	}
	data, err := os.ReadFile(filepath.Join(SKILLS_DIR, name+".md"))
	if err != nil {
		http.Error(w, "skill nao encontrada", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"skill":   name,
		"content": string(data),
		"status":  "ok",
		"note":    fmt.Sprintf("skill %s lida com sucesso", name),
	})
}

func handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/skills/")
	name = strings.Split(name, "/")[0]
	os.Remove(filepath.Join(SKILLS_DIR, name+".md"))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "deleted": name})
}
