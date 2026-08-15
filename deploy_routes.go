package main

import (
	"net/http"
	"os/exec"
	"strings"
)

// ── GET /deploy/status ──────────────────────────────────────────
// Status real do ambiente de produção: branch e commit do repo
// backend (somente leitura do git) + estado dos serviços systemd.
// Histórico de deploys não é registrado hoje — a resposta traz a
// lista vazia e uma nota honesta. Sem gatilho de ação.
func handleDeployStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", ROOT_PATH}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	svc := func(name string) string {
		out, err := exec.Command("systemctl", "is-active", name).Output()
		if err != nil {
			return "unknown"
		}
		return strings.TrimSpace(string(out))
	}

	commit := git("rev-parse", "HEAD")
	commitShort := ""
	if len(commit) >= 7 {
		commitShort = commit[:7]
	}
	logLine := git("log", "-1", "--format=%ci|%s")
	commitTime := ""
	commitMsg := ""
	if i := strings.Index(logLine, "|"); i >= 0 {
		commitTime = logLine[:i]
		commitMsg = logLine[i+1:]
	}

	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"env": map[string]interface{}{
			"name":         "Production",
			"branch":       git("branch", "--show-current"),
			"commit":       commit,
			"commit_short": commitShort,
			"commit_time":  commitTime,
			"commit_msg":   commitMsg,
			"services": map[string]string{
				"hokma": svc("hokma"),
				"nginx": svc("nginx"),
			},
		},
		"deploys":      []interface{}{},
		"deploys_note": "Histórico de deploys não é registrado pelo backend ainda — disponível apenas o estado atual.",
	})
}
