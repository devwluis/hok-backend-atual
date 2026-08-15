package main

import (
	"crypto/subtle"
	"encoding/json"
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
			sqliteExecParams(
				`INSERT OR REPLACE INTO app_settings (key, value, updated_at) VALUES (?, ?, ?);`,
				k, v, now)
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

	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Hok-Token")), []byte(os.Getenv("HOK_TOKEN"))) != 1 {
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
