package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ── GET /repos?kind=backend|frontend ──────────────────────────
func handleGetRepos(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	q := `SELECT id, kind, name, remote_url, branch, language, local_path, stars, created_at, updated_at FROM repositories`
	if kind != "" {
		q += fmt.Sprintf(` WHERE kind='%s'`, strings.ReplaceAll(kind, "'", "''"))
	}
	q += ` ORDER BY updated_at DESC;`
	out := sqliteExec(q)
	var items []map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 10)
		if len(parts) == 10 {
			items = append(items, map[string]interface{}{
				"id":         parts[0],
				"kind":       parts[1],
				"name":       parts[2],
				"remote_url": parts[3],
				"branch":     parts[4],
				"language":   parts[5],
				"local_path": parts[6],
				"stars":      parts[7],
				"created_at": parts[8],
				"updated_at": parts[9],
			})
		}
	}
	if items == nil {
		items = []map[string]interface{}{}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "repositories": items})
}

// ── POST /repos ────────────────────────────────────────────────
func handleCreateRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		RemoteURL string `json:"remote_url"`
		Branch    string `json:"branch"`
		Language  string `json:"language"`
		LocalPath string `json:"local_path"`
		Stars     int    `json:"stars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "bad request"})
		return
	}
	if body.Name == "" || (body.Kind != "backend" && body.Kind != "frontend") {
		respondJSON(w, map[string]string{"status": "error", "message": "name e kind (backend|frontend) obrigatórios"})
		return
	}
	if body.Branch == "" {
		body.Branch = "main"
	}
	now := time.Now().Unix()
	id := fmt.Sprintf("repo_%d", time.Now().UnixNano())

	esc := func(s string) string { return strings.ReplaceAll(s, "'", "''") }

	sqliteExec(fmt.Sprintf(
		`INSERT INTO repositories (id, kind, name, remote_url, branch, language, local_path, stars, created_at, updated_at)
		 VALUES ('%s', '%s', '%s', '%s', '%s', '%s', '%s', %d, %d, %d);`,
		id, esc(body.Kind), esc(body.Name), esc(body.RemoteURL), esc(body.Branch),
		esc(body.Language), esc(body.LocalPath), body.Stars, now, now))

	respondJSON(w, map[string]string{"status": "ok", "id": id})
}

// ── DELETE /repos/{id} ───────────────────────────────────────────
func handleDeleteRepo(w http.ResponseWriter, r *http.Request, id string) {
	safeID := strings.ReplaceAll(id, "'", "''")
	sqliteExec(fmt.Sprintf(`DELETE FROM repositories WHERE id='%s';`, safeID))
	respondJSON(w, map[string]string{"status": "ok"})
}

// ── POST /repos/{id}/git ── executa git status/pull/push no repo ─
func handleRepoGitAction(w http.ResponseWriter, r *http.Request, repoID string) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "bad request"})
		return
	}
	allowed := map[string]bool{"status": true, "pull": true, "push": true}
	if !allowed[body.Action] {
		respondJSON(w, map[string]string{"status": "error", "message": "action inválida (use status, pull ou push)"})
		return
	}

	safeID := strings.ReplaceAll(repoID, "'", "''")
	out := sqliteExec(fmt.Sprintf(`SELECT local_path, branch FROM repositories WHERE id='%s';`, safeID))
	fields := strings.SplitN(strings.TrimSpace(strings.Split(out, "\n")[0]), "|", 2)
	localPath := ""
	branch := "main"
	if len(fields) >= 1 {
		localPath = strings.TrimSpace(fields[0])
	}
	if len(fields) >= 2 && strings.TrimSpace(fields[1]) != "" {
		branch = strings.TrimSpace(fields[1])
	}
	if localPath == "" {
		respondJSON(w, map[string]string{"status": "error", "message": "repositório não encontrado ou sem local_path definido"})
		return
	}
	if info, err := os.Stat(localPath); err != nil || !info.IsDir() {
		respondJSON(w, map[string]string{"status": "error", "message": "local_path inválido ou inexistente no servidor: " + localPath})
		return
	}
	if _, err := os.Stat(localPath + "/.git"); err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "local_path não é um repositório git (.git não encontrado)"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	switch body.Action {
	case "status":
		cmd = exec.CommandContext(ctx, "git", "-C", localPath, "status", "--short", "--branch")
	case "pull":
		cmd = exec.CommandContext(ctx, "git", "-C", localPath, "pull", "origin", branch)
	case "push":
		cmd = exec.CommandContext(ctx, "git", "-C", localPath, "push")
	}

	out2, err := cmd.CombinedOutput()
	if err != nil {
		respondJSON(w, map[string]interface{}{
			"status": "error",
			"action": body.Action,
			"output": string(out2),
			"error":  err.Error(),
		})
		return
	}

	now := time.Now().Unix()
	sqliteExec(fmt.Sprintf(`UPDATE repositories SET updated_at=%d WHERE id='%s';`, now, safeID))

	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"action": body.Action,
		"output": string(out2),
	})
}

// ── Router /repos ──────────────────────────────────────────────
func handleRepos(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/repos")
	path = strings.TrimPrefix(path, "/")

	switch r.Method {
	case "GET":
		handleGetRepos(w, r)
	case "POST":
		if !requireHokAuth(w, r) {
			return
		}
		if path == "" {
			handleCreateRepo(w, r)
		} else {
			parts := strings.SplitN(path, "/", 2)
			if len(parts) == 2 && parts[1] == "git" {
				handleRepoGitAction(w, r, parts[0])
			} else {
				http.Error(w, "not found", 404)
			}
		}
	case "DELETE":
		if !requireHokAuth(w, r) {
			return
		}
		if path == "" {
			http.Error(w, "id obrigatório", 400)
			return
		}
		handleDeleteRepo(w, r, path)
	default:
		http.Error(w, "method not allowed", 405)
	}
}
