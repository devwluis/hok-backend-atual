package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

type ImovelData struct {
	Nome         string `json:"nome"`
	Bairro       string `json:"bairro"`
	Dormitorios  string `json:"dormitorios"`
	Metragem     string `json:"metragem"`
	PrecoAPartir string `json:"preco_a_partir"`
	Entrega      string `json:"entrega"`
	Diferenciais string `json:"diferenciais"`
}

func addImovelToSheet(data ImovelData) (string, error) {
	webhookURL := os.Getenv("N8N_ADD_IMOVEL_WEBHOOK")
	if webhookURL == "" {
		return "", fmt.Errorf("N8N_ADD_IMOVEL_WEBHOOK não configurado no .env")
	}

	body, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("erro ao chamar n8n: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("n8n retornou status %d", resp.StatusCode)
	}

	return fmt.Sprintf("Imóvel '%s' adicionado à planilha com sucesso.", data.Nome), nil
}
