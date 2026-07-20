package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"sort"
	"strings"
)

type EndpointInfo struct {
	Path string `json:"path"`
}

func handleIntrospect(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	mainPath := os.Getenv("HOME") + "/ecossistema/backend/main.go"
	f, err := os.Open(mainPath)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": err.Error()})
		return
	}
	defer f.Close()

	var endpoints []EndpointInfo
	seen := map[string]bool{}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "http.HandleFunc(") {
			start := strings.Index(line, `"`)
			end := strings.Index(line[start+1:], `"`)
			if start >= 0 && end >= 0 {
				path := line[start+1 : start+1+end]
				if !seen[path] {
					seen[path] = true
					endpoints = append(endpoints, EndpointInfo{Path: path})
				}
			}
		}
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].Path < endpoints[j].Path
	})

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"endpoints": endpoints,
		"total":     len(endpoints),
		"version":   "v25",
	})
}
