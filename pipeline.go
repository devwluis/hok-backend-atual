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

const PIPELINES_DIR = "/root/hokma/backend/pipelines"

type PipelineStep struct {
	Skill     string `json:"skill"`
	Condition string `json:"condition,omitempty"` // "contains:X", "not_contains:X", "empty", "not_empty"
	OnError   string `json:"on_error,omitempty"`   // "stop" (default) ou "continue"
}

type Pipeline struct {
	Name      string         `json:"name"`
	Steps     []PipelineStep `json:"steps"`
	UpdatedAt int64          `json:"updated_at"`
}

type PipelineStepResult struct {
	Skill      string `json:"skill"`
	Output     string `json:"output"`
	Success    bool   `json:"success"`
	Skipped    bool   `json:"skipped"`
	SkipReason string `json:"skip_reason,omitempty"`
}

type PipelineRunResult struct {
	Pipeline  string                `json:"pipeline"`
	Steps     []PipelineStepResult  `json:"steps"`
	Success   bool                  `json:"success"`
	LatencyMs int64                 `json:"latency_ms"`
}

func ensurePipelinesDir() {
	os.MkdirAll(PIPELINES_DIR, 0755)
}

func listPipelines() ([]Pipeline, error) {
	ensurePipelinesDir()
	entries, err := os.ReadDir(PIPELINES_DIR)
	if err != nil {
		return nil, err
	}
	var pipelines []Pipeline
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(PIPELINES_DIR, e.Name()))
		if err != nil {
			continue
		}
		var p Pipeline
		if json.Unmarshal(data, &p) == nil && p.Name != "" {
			pipelines = append(pipelines, p)
		}
	}
	return pipelines, nil
}

func getPipeline(name string) (*Pipeline, error) {
	data, err := os.ReadFile(filepath.Join(PIPELINES_DIR, name+".json"))
	if err != nil {
		return nil, err
	}
	var p Pipeline
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func savePipeline(p Pipeline) error {
	ensurePipelinesDir()
	p.UpdatedAt = time.Now().Unix()
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(PIPELINES_DIR, p.Name+".json"), data, 0644)
}

// evaluateCondition checa uma condição simples contra a saída do step anterior
func evaluateCondition(condition, prevOutput string) (bool, string) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, ""
	}
	if strings.HasPrefix(condition, "contains:") {
		needle := strings.TrimPrefix(condition, "contains:")
		if strings.Contains(prevOutput, needle) {
			return true, ""
		}
		return false, fmt.Sprintf("condição 'contains:%s' não satisfeita", needle)
	}
	if strings.HasPrefix(condition, "not_contains:") {
		needle := strings.TrimPrefix(condition, "not_contains:")
		if !strings.Contains(prevOutput, needle) {
			return true, ""
		}
		return false, fmt.Sprintf("condição 'not_contains:%s' não satisfeita", needle)
	}
	if condition == "empty" {
		if strings.TrimSpace(prevOutput) == "" {
			return true, ""
		}
		return false, "saída anterior não está vazia"
	}
	if condition == "not_empty" {
		if strings.TrimSpace(prevOutput) != "" {
			return true, ""
		}
		return false, "saída anterior está vazia"
	}
	return true, ""
}

// runPipeline executa os steps em sequência, encadeando output via {{PREV_OUTPUT}}
func runPipeline(p Pipeline) PipelineRunResult {
	start := time.Now()
	result := PipelineRunResult{Pipeline: p.Name, Success: true}

	skills, err := listSkills()
	if err != nil {
		result.Success = false
		result.Steps = append(result.Steps, PipelineStepResult{
			Output: "erro ao carregar skills: " + err.Error(),
		})
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}
	skillMap := make(map[string]Skill, len(skills))
	for _, s := range skills {
		skillMap[s.Name] = s
	}

	prevOutput := ""
	for _, step := range p.Steps {
		sk, ok := skillMap[step.Skill]
		if !ok {
			result.Steps = append(result.Steps, PipelineStepResult{
				Skill: step.Skill, Output: fmt.Sprintf("skill '%s' não encontrada", step.Skill),
			})
			if step.OnError != "continue" {
				result.Success = false
				break
			}
			continue
		}

		condOK, reason := evaluateCondition(step.Condition, prevOutput)
		if !condOK {
			result.Steps = append(result.Steps, PipelineStepResult{
				Skill: step.Skill, Skipped: true, SkipReason: reason, Success: true,
			})
			continue
		}

		cmd := extractBashFromContent(sk.Content)
		if cmd == "" {
			result.Steps = append(result.Steps, PipelineStepResult{
				Skill: step.Skill, Output: "skill sem bloco bash executável",
			})
			if step.OnError != "continue" {
				result.Success = false
				break
			}
			continue
		}

		cmd = strings.ReplaceAll(cmd, "{{PREV_OUTPUT}}", prevOutput)
		output := executeCommand(cmd)
		success := !strings.Contains(strings.ToLower(output), "error") && !strings.Contains(output, "Traceback")

		result.Steps = append(result.Steps, PipelineStepResult{
			Skill: step.Skill, Output: output, Success: success,
		})
		prevOutput = output

		if !success && step.OnError != "continue" {
			result.Success = false
			break
		}
	}

	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

// ── HTTP Handlers ──────────────────────────────────────────────────────────

func handlePipelines(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(200)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	switch r.Method {
	case "GET":
		pipelines, err := listPipelines()
		if err != nil {
			respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		respondJSON(w, map[string]interface{}{"status": "ok", "pipelines": pipelines})
	case "POST":
		var p Pipeline
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			respondJSON(w, map[string]string{"status": "error", "message": "invalid JSON"})
			return
		}
		if p.Name == "" {
			respondJSON(w, map[string]string{"status": "error", "message": "name obrigatório"})
			return
		}
		if err := savePipeline(p); err != nil {
			respondJSON(w, map[string]string{"status": "error", "message": err.Error()})
			return
		}
		respondJSON(w, map[string]string{"status": "ok", "message": "pipeline salvo: " + p.Name})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func handlePipelineRun(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(200)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Name  string         `json:"name"`
		Steps []PipelineStep `json:"steps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "invalid JSON"})
		return
	}

	var p Pipeline
	if body.Name != "" && len(body.Steps) == 0 {
		saved, err := getPipeline(body.Name)
		if err != nil {
			respondJSON(w, map[string]string{"status": "error", "message": "pipeline não encontrada: " + body.Name})
			return
		}
		p = *saved
	} else {
		p = Pipeline{Name: body.Name, Steps: body.Steps}
		if p.Name == "" {
			p.Name = "adhoc"
		}
	}

	result := runPipeline(p)
	sqliteExec(fmt.Sprintf(
		"INSERT INTO logs (event, level, source) VALUES ('pipeline_run:%s success=%v', 'INFO', 'pipeline');",
		p.Name, result.Success,
	))
	respondJSON(w, result)
}
