package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

func handleGetSettings(w http.ResponseWriter, r *http.Request) {
	out := sqliteExec(`SELECT key, value FROM app_settings;`)
	result := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "settings": result})
}

func handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "bad request"})
		return
	}
	now := time.Now().Unix()
	allowed := []string{"baseUrl", "tunnelUrl",
		"token", "deepseekKey", "openrouterKey", "geminiKey", "openaiKey", "groqKey", "anthropicKey", "n8nApiKey"}
	saved := []string{}
	for _, k := range allowed {
		if v, ok := body[k]; ok {
			key := strings.ReplaceAll(k, "'", "''")
			val := strings.ReplaceAll(v, "'", "''")
			sqliteExec(fmt.Sprintf(
				`INSERT OR REPLACE INTO app_settings (key, value, updated_at) VALUES ('%s', '%s', %d);`,
				key, val, now))
			saved = append(saved, k)
		}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "saved": saved})
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	if r.Header.Get("X-Hok-Token") != os.Getenv("HOK_TOKEN") {
		http.Error(w, "unauthorized", 401)
		return
	}

	switch r.Method {
	case "GET":
		handleGetSettings(w, r)
	case "POST":
		handleSaveSettings(w, r)
	default:
		http.Error(w, "method not allowed", 405)
	}
}
