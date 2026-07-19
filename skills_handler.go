package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func skillsDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe) + "/skills"
}

func skillsListHandler(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(skillsDir())
	if err != nil {
		jsonError(w, "skills dir error: "+err.Error(), 500)
		return
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			names = append(names, strings.TrimSuffix(e.Name(), ".md"))
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"skills": names, "count": len(names),
	})
}

func skillsReadHandler(w http.ResponseWriter, r *http.Request) {
	name := filepath.Base(r.URL.Query().Get("name"))
	if name == "" || name == "." {
		jsonError(w, "name obrigatorio", 400)
		return
	}
	data, err := os.ReadFile(skillsDir() + "/" + name + ".md")
	if err != nil {
		jsonError(w, "skill nao encontrada: "+name, 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"name": name, "content": string(data),
	})
}
