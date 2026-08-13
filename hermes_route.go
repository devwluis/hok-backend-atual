// hermes_route.go
// Bridge HTTP entre o HokMa backend e o Hermes sub-agente.
// NAO modifica o main.go - este arquivo usa init() que o Go executa
// automaticamente quando compila o package.
//
// Rotas adicionadas:
//   GET  /v1/hermes/health - health check do bridge
//   POST /v1/hermes/chat   - chat que delega pro hermes-gateway
//
// Idempotente: pode ser compilado varias vezes sem efeito colateral.

package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	chat "hokma_backend/internal/chat"
)

var hermesClient *chat.HermesClient

func init() {
	// Inicializa o client Hermes quando o package main carrega
	var err error
	hermesClient, err = chat.NewHermesClient(chat.DefaultHermesConfig(), log.Default())
	if err != nil {
		log.Printf("WARN: nao foi possivel inicializar hermes_client: %v", err)
		log.Printf("WARN: rotas /v1/hermes/* nao serao habilitadas")
		return
	}

	http.HandleFunc("/v1/hermes/health", handleHermesHealth)
	http.HandleFunc("/v1/hermes/chat", handleHermesChat)
	log.Println("HokMa->Hermes bridge: rotas /v1/hermes/{chat,health} habilitadas")
}

func handleHermesHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	status := map[string]any{
		"status":  "ok",
		"backend": "hermes_chat",
	}
	if hermesClient == nil {
		status["status"] = "degraded"
		status["error"] = "hermes_client not initialized"
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(status)
}

func handleHermesChat(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (use POST)", http.StatusMethodNotAllowed)
		return
	}
	if hermesClient == nil {
		http.Error(w, "hermes bridge not initialized", http.StatusServiceUnavailable)
		return
	}

	var req chat.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Stream {
		handleHermesStream(w, r, req)
		return
	}
	resp, err := hermesClient.Chat(r.Context(), req)
	if err != nil {
		log.Printf("ERRO hermes chat: %v", err)
		http.Error(w, "hermes error: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
func handleHermesStream(w http.ResponseWriter, r *http.Request, req chat.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	chunks, errs := hermesClient.StreamChat(r.Context(), req)
	for {
		select {
		case <-r.Context().Done():
			return
		case err := <-errs:
			if err != nil {
				payload, _ := json.Marshal(map[string]string{"error": err.Error()})
				fmt.Fprintf(w, "data: %s\n\n", payload)
				flusher.Flush()
			}
			return
		case chunk, ok := <-chunks:
			if !ok {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			payload, _ := json.Marshal(map[string]any{"content": chunk.Content, "done": chunk.Done})
			fmt.Fprintf(w, "data: %s\n\n", payload)
			flusher.Flush()
			if chunk.Done {
				return
			}
		}
	}
}
