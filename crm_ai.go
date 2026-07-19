package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const crmSystemPromptTemplate = `Voce e Adriana Chaves, corretora de imoveis, atendendo pelo WhatsApp da imobiliaria (imoveischaves.com).
Fale sempre como Adriana, em primeira pessoa, nunca mencione que e uma inteligencia artificial, assistente virtual ou robo.
Tom: humano, caloroso, direto, profissional. Frases curtas, como quem digita rapido no celular. Nada de linguagem robotica ou formal demais.
Objetivo principal: entender o que o lead procura (tipo de imovel, regiao, faixa de preco, prazo) e conduzir para o agendamento de uma visita ou ligacao.
Dados do lead nesta conversa:
- Nome: %s
- Origem: %s
- Campanha: %s
Regras:
- Nunca invente disponibilidade, preco ou endereco especifico de imovel que voce nao tem certeza.
- Se o lead pedir algo que voce nao sabe responder com certeza (preco exato, disponibilidade real), diga que vai confirmar e ja retorna.
- Se o lead demonstrar urgencia ou interesse forte, sugira agendar visita ou ligacao.
- Respostas curtas (2-4 frases), como mensagem real de WhatsApp, nao como email.`

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
}

type openRouterResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type aiReplyRequest struct {
	Canal string `json:"canal"`
}

type aiReplyResponse struct {
	Reply       string      `json:"reply"`
	Interaction Interaction `json:"interaction"`
}

// Erros sentinela - permitem que quem chamar (handler HTTP ou webhook)
// decida como reagir sem duplicar a logica de checagem.
var (
	ErrLeadNaoEncontrado = errors.New("lead nao encontrado")
	ErrIADesativada      = errors.New("ia esta desativada para este lead (atendimento humano em andamento)")
)

// ---------------------------------------------------------------------
// Nucleo reaproveitavel: gera o texto da resposta da IA para um lead.
// Usado tanto pelo handler HTTP /ai-reply quanto pelo webhook do WhatsApp.
// ---------------------------------------------------------------------

func generateAIReplyText(db *sql.DB, leadID string) (string, error) {
	var l Lead
	err := db.QueryRow(`
		SELECT id, nome, telefone, origem, campanha, status, ia_ativa, criado_em, atualizado_em
		FROM crm_leads WHERE id = ?
	`, leadID).Scan(&l.ID, &l.Nome, &l.Telefone, &l.Origem, &l.Campanha, &l.Status, &l.IaAtiva, &l.CriadoEm, &l.AtualizadoEm)
	if err != nil {
		return "", ErrLeadNaoEncontrado
	}
	if !l.IaAtiva {
		return "", ErrIADesativada
	}

	rows, err := db.Query(`
		SELECT direcao, mensagem
		FROM crm_interactions
		WHERE lead_id = ?
		ORDER BY criado_em ASC
		LIMIT 30
	`, leadID)
	if err != nil {
		log.Printf("crm-ai: erro ao buscar historico do lead %s: %v", leadID, err)
		return "", fmt.Errorf("erro ao buscar historico: %w", err)
	}
	defer rows.Close()

	nome := l.Nome
	if nome == "" {
		nome = "nao informado"
	}
	origem := l.Origem
	if origem == "" {
		origem = "nao informada"
	}
	campanha := l.Campanha
	if campanha == "" {
		campanha = "nao informada"
	}

	knowledgeBase, err := getCRMContextForLead(db, l)
	if err != nil {
		log.Printf("crm-ai: erro ao buscar contexto/base de conhecimento: %v", err)
		knowledgeBase = ""
	}

	systemPrompt := fmt.Sprintf(crmSystemPromptTemplate, nome, origem, campanha)
	if knowledgeBase != "" {
		systemPrompt += "\n\nBASE DE CONHECIMENTO (empreendimentos e condicoes disponiveis - use APENAS essas informacoes, nunca invente preco, endereco ou condicao que nao esteja aqui):\n" + knowledgeBase
	}

	messages := []openRouterMessage{
		{Role: "system", Content: systemPrompt},
	}
	for rows.Next() {
		var direcao, mensagem string
		if err := rows.Scan(&direcao, &mensagem); err != nil {
			continue
		}
		role := "user"
		if direcao == "saida" {
			role = "assistant"
		}
		messages = append(messages, openRouterMessage{Role: role, Content: mensagem})
	}

	orKey := os.Getenv("OPENROUTER_API_KEY")
	if orKey == "" {
		return "", errors.New("OPENROUTER_API_KEY nao configurado")
	}

	reqBody := openRouterRequest{
		Model:    "minimax/minimax-m2.5",
		Messages: messages,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	httpReq, err := http.NewRequest("POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("erro ao montar requisicao: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+orKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://app.imoveischaves.com")
	httpReq.Header.Set("X-Title", "HOK CRM")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("crm-ai: erro ao chamar OpenRouter: %v", err)
		return "", fmt.Errorf("erro ao chamar provedor de IA: %w", err)
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(resp.Body)
	var orResp openRouterResponse
	if err := json.Unmarshal(respBytes, &orResp); err != nil {
		log.Printf("crm-ai: erro ao decodificar resposta OpenRouter: %v | body: %s", err, string(respBytes))
		return "", fmt.Errorf("resposta invalida do provedor de IA: %w", err)
	}
	if orResp.Error != nil {
		log.Printf("crm-ai: OpenRouter retornou erro: %s", orResp.Error.Message)
		return "", fmt.Errorf("erro do provedor de IA: %s", orResp.Error.Message)
	}
	if len(orResp.Choices) == 0 {
		log.Printf("crm-ai: OpenRouter sem choices | body: %s", string(respBytes))
		return "", errors.New("provedor de IA nao retornou resposta")
	}

	return orResp.Choices[0].Message.Content, nil
}

// saveAIReply grava a resposta da IA como interacao de saida e atualiza
// o lead. Reaproveitado pelo handler HTTP e pelo webhook do WhatsApp.
func saveAIReply(db *sql.DB, leadID, canal, replyText string) (Interaction, error) {
	tx, err := db.Begin()
	if err != nil {
		return Interaction{}, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`
		INSERT INTO crm_interactions (lead_id, canal, direcao, mensagem, origem_resposta)
		VALUES (?, ?, 'saida', ?, 'ia')
	`, leadID, canal, replyText)
	if err != nil {
		log.Printf("crm-ai: erro ao salvar interacao de resposta do lead %s: %v", leadID, err)
		return Interaction{}, err
	}

	var it Interaction
	err = tx.QueryRow(`
		SELECT id, lead_id, canal, direcao, mensagem, origem_resposta, criado_em
		FROM crm_interactions WHERE lead_id = ? ORDER BY criado_em DESC, id DESC LIMIT 1
	`, leadID).Scan(&it.ID, &it.LeadID, &it.Canal, &it.Direcao, &it.Mensagem, &it.OrigemResposta, &it.CriadoEm)
	if err != nil {
		return Interaction{}, err
	}

	if _, err := tx.Exec(`
		UPDATE crm_leads SET atualizado_em = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ?
	`, leadID); err != nil {
		return Interaction{}, err
	}

	if err := tx.Commit(); err != nil {
		return Interaction{}, err
	}

	return it, nil
}

// generateAndSaveAIReply combina os dois passos acima. E o ponto de
// entrada usado pelo webhook do WhatsApp (chamada interna, sem HTTP).
func generateAndSaveAIReply(db *sql.DB, leadID, canal string) (Interaction, error) {
	text, err := generateAIReplyText(db, leadID)
	if err != nil {
		return Interaction{}, err
	}
	return saveAIReply(db, leadID, canal, text)
}

// ---------------------------------------------------------------------
// Handler HTTP - agora um wrapper fino em cima do nucleo acima.
// ---------------------------------------------------------------------

func aiReplyHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		leadID := r.PathValue("id")
		if leadID == "" {
			writeErr(w, http.StatusBadRequest, "id do lead e obrigatorio")
			return
		}
		var req aiReplyRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Canal == "" {
			req.Canal = "whatsapp"
		}

		it, err := generateAndSaveAIReply(db, leadID, req.Canal)
		if err != nil {
			switch {
			case errors.Is(err, ErrLeadNaoEncontrado):
				writeErr(w, http.StatusNotFound, err.Error())
			case errors.Is(err, ErrIADesativada):
				writeErr(w, http.StatusConflict, err.Error())
			default:
				log.Printf("crm-ai: erro ao gerar resposta para lead %s: %v", leadID, err)
				writeErr(w, http.StatusBadGateway, "erro ao gerar resposta da IA")
			}
			return
		}

		writeJSON(w, http.StatusOK, aiReplyResponse{Reply: it.Mensagem, Interaction: it})
	}
}
