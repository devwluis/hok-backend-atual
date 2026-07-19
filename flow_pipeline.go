package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type FlowStep struct {
	Type    string `json:"type"`
	Skill   string `json:"skill,omitempty"`
	Cond    string `json:"cond,omitempty"`
	Then    string `json:"then,omitempty"`
	Else    string `json:"else,omitempty"`
	Do      string `json:"do,omitempty"`
	MaxIter int    `json:"max_iter,omitempty"`
}

type FlowRequest struct {
	Task  string     `json:"task"`
	Steps []FlowStep `json:"steps"`
}

type FlowStepResult struct {
	Type    string `json:"type"`
	Skill   string `json:"skill"`
	Output  string `json:"output"`
	Success bool   `json:"success"`
	Note    string `json:"note,omitempty"`
}

type FlowResponse struct {
	Task    string           `json:"task"`
	Steps   []FlowStepResult `json:"steps"`
	Success bool             `json:"success"`
	Message string           `json:"message"`
}

func evalCond(cond string, output string, success bool) bool {
	switch {
	case cond == "success":
		return success
	case cond == "fail":
		return !success
	case cond == "empty":
		return strings.TrimSpace(output) == ""
	case strings.HasPrefix(cond, "contains:"):
		return strings.Contains(output, strings.TrimPrefix(cond, "contains:"))
	case strings.HasPrefix(cond, "not_contains:"):
		return !strings.Contains(output, strings.TrimPrefix(cond, "not_contains:"))
	default:
		return false
	}
}

func handleFlowPipeline(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var req FlowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Steps) == 0 {
		http.Error(w, `{"error":"steps obrigatorios"}`, 400)
		return
	}

	results := []FlowStepResult{}
	currentInput := req.Task
	lastOutput := ""
	lastSuccess := true

	for _, step := range req.Steps {
		switch step.Type {

		case "run":
			out, ok, _, _ := runSkillWithRetry(step.Skill, currentInput)
			results = append(results, FlowStepResult{
				Type: "run", Skill: step.Skill,
				Output: out, Success: ok,
			})
			lastOutput = out
			lastSuccess = ok
			currentInput = out

		case "if":
			branch := step.Else
			note := "else"
			if evalCond(step.Cond, lastOutput, lastSuccess) {
				branch = step.Then
				note = "then"
			}
			if branch != "" {
				out, ok, _, _ := runSkillWithRetry(branch, currentInput)
				results = append(results, FlowStepResult{
					Type: "if", Skill: branch,
					Output: out, Success: ok, Note: note,
				})
				lastOutput = out
				lastSuccess = ok
				currentInput = out
			}

		case "while":
			maxIter := step.MaxIter
			if maxIter <= 0 {
				maxIter = 5
			}
			for i := 0; i < maxIter; i++ {
				if !evalCond(step.Cond, lastOutput, lastSuccess) {
					break
				}
				out, ok, _, _ := runSkillWithRetry(step.Do, currentInput)
				results = append(results, FlowStepResult{
					Type: "while", Skill: step.Do,
					Output: out, Success: ok,
					Note: fmt.Sprintf("iter %d/%d", i+1, maxIter),
				})
				lastOutput = out
				lastSuccess = ok
				currentInput = out
			}

		default:
			results = append(results, FlowStepResult{
				Type: step.Type, Note: "tipo desconhecido",
			})
		}
	}

	respondJSON(w, FlowResponse{
		Task:    req.Task,
		Steps:   results,
		Success: lastSuccess,
		Message: fmt.Sprintf("%d steps executados", len(results)),
	})
}
