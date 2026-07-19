package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"
)

// SelfHealEvent — payload enviado ao N8N
type SelfHealEvent struct {
	EventType  string `json:"event_type"`   // "build_fail" | "loop_abort" | "recovery_ok"
	Model      string `json:"model"`
	File       string `json:"file"`
	Task       string `json:"task"`
	Error      string `json:"error"`
	Iterations int    `json:"iterations"`
	TunnelURL  string `json:"tunnel_url"`   // URL atual do tunnel para N8N chamar de volta
	Timestamp  string `json:"timestamp"`
}

// getTunnelURL retorna a URL do tunnel Cloudflare via env TUNNEL_URL
func getTunnelURL() string {
	url := os.Getenv("TUNNEL_URL")
	if url == "" {
		return "http://localhost:8082" // fallback local
	}
	return url
}

// notifyN8N envia evento ao webhook do N8N de forma assíncrona (não bloqueia o loop)
func notifyN8N(event SelfHealEvent) {
	webhookURL := os.Getenv("N8N_WEBHOOK_URL")
	if webhookURL == "" {
		webhookURL = "http://127.0.0.1:5678/webhook/hokos-self-heal"
	}

	event.TunnelURL = getTunnelURL()
	event.Timestamp = time.Now().Format(time.RFC3339)

	go func() {
		body, err := json.Marshal(event)
		if err != nil {
			log.Printf("[self-heal] erro ao serializar evento: %v", err)
			return
		}

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(body))
		if err != nil {
			log.Printf("[self-heal] N8N inalcançável: %v", err)
			return
		}
		defer resp.Body.Close()
		log.Printf("[self-heal] evento '%s' enviado ao N8N — status %d", event.EventType, resp.StatusCode)
	}()
}

// selfHealHandler — POST /self-heal
// Recebido pelo N8N para acionar recovery task
func selfHealHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method == "OPTIONS" {
		w.WriteHeader(200)
		return
	}

	if !requireHokAuth(w, r) {
		return
	}
	var req AgentLoopReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid_body"}`, 400)
		return
	}

	// Defaults para recovery
	if req.Task == "" {
		req.Task = "Analise o arquivo, identifique erros de compilação ou lógica e corrija-os."
	}
	if req.MaxIter == 0 {
		req.MaxIter = 3
	}
	if req.Model == "" {
		req.Model = getTopModel()
	}
	if req.DsKey == "" {
		req.DsKey = os.Getenv("DS_KEY")
	}
	if req.OrKey == "" {
		req.OrKey = os.Getenv("OR_KEY")
	}

	log.Printf("[self-heal] recovery iniciado — arquivo: %s, modelo: %s", req.File, req.Model)

	// Executar agent-loop internamente via goroutine
	// Retornar imediatamente com ACK para o N8N não dar timeout
	go func() {
		// Simular chamada ao agentLoopHandler via HTTP interno
		body, _ := json.Marshal(req)
		client := &http.Client{Timeout: 5 * time.Minute}
		resp, err := client.Post(
			fmt.Sprintf("http://localhost:%s/agent/task", getPort()),
			"application/json",
			bytes.NewBuffer(body),
		)
		if err != nil {
			log.Printf("[self-heal] erro ao chamar agent-loop: %v", err)
			notifyN8N(SelfHealEvent{
				EventType: "recovery_fail",
				File:      req.File,
				Error:     err.Error(),
			})
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			log.Printf("[self-heal] recovery OK para %s", req.File)
			notifyN8N(SelfHealEvent{
				EventType: "recovery_ok",
				File:      req.File,
				Model:     req.Model,
			})
		}
	}()

	json.NewEncoder(w).Encode(map[string]string{
		"status":  "recovery_initiated",
		"file":    req.File,
		"model":   req.Model,
	})
}

// getPort retorna a porta do backend
func getPort() string {
	p := os.Getenv("PORT")
	if p == "" {
		return "8082"
	}
	return p
}
