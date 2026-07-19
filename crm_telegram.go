package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
)

func notifyNewLeadTelegram(nome, telefone, origem, campanha string) {
	token := os.Getenv("TELEGRAM_TOKEN")
	chatID := os.Getenv("TELEGRAM_CHAT_ID")
	if token == "" || chatID == "" {
		log.Printf("crm-telegram: TELEGRAM_TOKEN ou TELEGRAM_CHAT_ID nao configurados, pulando notificacao")
		return
	}

	if nome == "" {
		nome = "(sem nome)"
	}
	if origem == "" {
		origem = "nao informada"
	}
	if campanha == "" {
		campanha = "nao informada"
	}

	text := fmt.Sprintf(
		"🏠 Novo lead no CRM!\n\n👤 Nome: %s\n📱 Telefone: %s\n📍 Origem: %s\n📢 Campanha: %s",
		nome, telefone, origem, campanha,
	)

	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	resp, err := http.PostForm(apiURL, url.Values{
		"chat_id": {chatID},
		"text":    {text},
	})
	if err != nil {
		log.Printf("crm-telegram: erro ao enviar notificacao: %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("crm-telegram: telegram retornou status %d", resp.StatusCode)
	}
}
