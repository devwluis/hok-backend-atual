package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

func sendTelegram(message string) error {
	token := os.Getenv("TELEGRAM_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		return fmt.Errorf("TELEGRAM_TOKEN ou TELEGRAM_CHAT_ID não configurado")
	}
	body, _ := json.Marshal(map[string]string{
		"chat_id": chatID,
		"text":    message,
	})
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

func handleNotify(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var req struct {
		Message string `json:"message"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if req.Message == "" {
		req.Message = "Alerta HOK OS!"
	}
	if err := sendTelegram(req.Message); err != nil {
		respondJSON(w, map[string]string{"status": "error", "error": err.Error()})
		return
	}
	respondJSON(w, map[string]string{"status": "ok", "message": "Enviado!"})
}
