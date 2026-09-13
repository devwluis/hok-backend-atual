package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// handleTerminalSSE — terminal via SSE (Server-Sent Events).
// Fallback para redes móveis que bloqueiam WebSocket upgrade.
// Mesma autenticação e lógica de sessão que handleTerminalWS.
func handleTerminalSSE(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if token == "" || !tokenMatches(token) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"unauthorized"}`))
		return
	}

	userKey := terminalUserKey(token)
	sessionID := r.URL.Query().Get("session_id")
	forceNew := r.URL.Query().Get("new") == "1"

	created := false
	s := terminalSessions.getOrCreate(userKey, sessionID, &created, forceNew)
	if s == nil {
		log.Printf("[term-sse] falha ao criar sessão user=%s", userKey)
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"pty_start"}`))
		return
	}

	// Setup SSE response (must be before any writes).
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher := w.(http.Flusher)

	// Register SSE client BEFORE sending any data (avoids race with
	// readerLoop broadcast: output generated after register but before
	// scrollback snapshot arrives via channel, no duplication).
	sseCh, stop := s.registerSSE()
	defer s.unregisterSSE(sseCh)
	defer close(stop)

	// Send session info.
	sendSSEvent(w, "session", map[string]interface{}{
		"type":       "session",
		"session_id": s.ID,
		"created":    created,
	}, flusher)

	// Send scrollback snapshot.
	sb := s.buf.Snapshot()
	if len(sb) > 0 {
		sendSSEvent(w, "scrollback", map[string]string{
			"type": "scrollback",
			"data": base64.StdEncoding.EncodeToString(sb),
		}, flusher)
	}

	// Send ready.
	sendSSEvent(w, "ready", nil, flusher)

	// Stream output.
	for {
		select {
		case chunk := <-sseCh:
			if chunk == nil {
				return
			}
			data := base64.StdEncoding.EncodeToString(chunk)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-stop:
			return
		case <-time.After(30 * time.Second):
			fmt.Fprint(w, ":heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func sendSSEvent(w http.ResponseWriter, event string, data interface{}, flusher http.Flusher) {
	if data == nil {
		fmt.Fprintf(w, "event: %s\n\n", event)
	} else {
		jsonData, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(jsonData))
	}
	if flusher != nil {
		flusher.Flush()
	}
}

// handleTerminalInput — POST /terminal/input: envia input para sessão via HTTP.
// Fallback para redes móveis que bloqueiam WebSocket.
func handleTerminalInput(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if token == "" || !tokenMatches(token) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"unauthorized"}`))
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		SessionID string `json:"session_id"`
		Data      string `json:"data"`
		Cols      uint16 `json:"cols,omitempty"`
		Rows      uint16 `json:"rows,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid body"}`))
		return
	}

	userKey := terminalUserKey(token)
	s := terminalSessions.get(userKey, req.SessionID)
	if s == nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"session not found"}`))
		return
	}
	if req.Cols > 0 && req.Rows > 0 {
		s.resize(req.Cols, req.Rows)
	}
	s.writeInput(req.Data)
	w.Write([]byte(`{"status":"ok"}`))
}
