package main

import (
	"encoding/json"
	"net/http"
)

func handleDebugTools(w http.ResponseWriter, r *http.Request) {
	if !requireHokAuth(w, r) {
		return
	}
	tools := agentTools()
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tools); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
