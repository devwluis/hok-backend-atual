package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

// handleImportSkillsZip — BLOCO 2 (03/09): importa um .zip de skills (como o
// upload de pastas de Skills do n8n Agents). Aceita multipart "file"; extrai
// arquivos .md/.json e salva em SKILLS_DIR com prefixo "import_<ts>_" para
// evitar colisão. Retorna a lista de skills importadas.
func handleImportSkillsZip(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "POST esperado", http.StatusMethodNotAllowed)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "multipart: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	zr, err := zip.NewReader(file, r.ContentLength)
	if err != nil {
		http.Error(w, "zip invalido: "+err.Error(), http.StatusBadRequest)
		return
	}

	ensureSkillsDir()
	imported := []map[string]string{}
	count := 0
	for _, zf := range zr.File {
		if zf.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(zf.Name)
		if !strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".json") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			continue
		}
		// sanitiza nome: só basename alfanumérico/underscore/hífen
		clean := strings.Map(func(rn rune) rune {
			if (rn >= 'a' && rn <= 'z') || (rn >= 'A' && rn <= 'Z') ||
				(rn >= '0' && rn <= '9') || rn == '_' || rn == '-' {
				return rn
			}
			return '_'
		}, strings.TrimSuffix(name, filepath.Ext(name)))
		if clean == "" {
			continue
		}
		skillName := clean
		dest := filepath.Join(SKILLS_DIR, skillName+".md")
		if _, err := os.Stat(dest); err == nil {
			skillName = fmt.Sprintf("import_%d_%s", time.Now().UnixNano()%1_000_000, clean)
			dest = filepath.Join(SKILLS_DIR, skillName+".md")
		}
		if err := os.WriteFile(dest, data, 0644); err != nil {
			continue
		}
		count++
		imported = append(imported, map[string]string{"name": skillName, "file": dest})
	}

	log.Printf("[AUDIT] skills importadas via zip: %d arquivos", count)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   "ok",
		"imported": count,
		"skills":   imported,
	})
}
