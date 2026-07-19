package main

// whatsapp_routes.go
//
// Endpoint direto com a Meta Cloud API (sem BSP), preparado pra
// Coexistence no número virgem da Adriana.
//
// Fluxo:
//   GET  /whatsapp/webhook  -> verificação inicial da Meta (hub.challenge)
//   POST /whatsapp/webhook  -> recebe mensagens, echoes do app (Coexistence)
//                              e eventos de status; faz upsert do lead e
//                              grava interação, reaproveitando o mesmo
//                              padrão de crm_routes.go
//
// TODOs marcados no código:
//   1. Preencher WHATSAPP_VERIFY_TOKEN, WHATSAPP_ACCESS_TOKEN,
//      WHATSAPP_PHONE_NUMBER_ID, META_APP_SECRET no .env quando a conta
//      Meta Business Manager estiver verificada e o número conectado.
//   2. Implementar o envio de resposta (sendWhatsAppMessage) quando
//      formos ligar isso ao ai-reply / resposta manual do CRM.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// ---------------------------------------------------------------------
// Registro de rotas
// ---------------------------------------------------------------------

func RegisterWhatsAppRoutes(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("GET /whatsapp/webhook", whatsappVerifyHandler)
	mux.HandleFunc("POST /whatsapp/webhook", whatsappReceiveHandler(db))
}

// ---------------------------------------------------------------------
// GET /whatsapp/webhook — verificação inicial (hub.challenge)
// ---------------------------------------------------------------------

func whatsappVerifyHandler(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	expectedToken := os.Getenv("WHATSAPP_VERIFY_TOKEN")

	if mode == "subscribe" && expectedToken != "" && token == expectedToken {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge)) // IMPORTANTE: texto puro, nao JSON
		return
	}

	log.Printf("whatsapp: falha na verificacao do webhook (mode=%s, token_bate=%v)", mode, token == expectedToken)
	w.WriteHeader(http.StatusForbidden)
}

// ---------------------------------------------------------------------
// Tipos do payload da Meta (estrutura aninhada oficial)
// ---------------------------------------------------------------------

type waWebhookPayload struct {
	Entry []waEntry `json:"entry"`
}

type waEntry struct {
	ID      string     `json:"id"`
	Changes []waChange `json:"changes"`
}

type waChange struct {
	Field string  `json:"field"` // "messages", "smb_message_echoes", "smb_app_state_sync"
	Value waValue `json:"value"`
}

type waValue struct {
	MessagingProduct string          `json:"messaging_product"`
	Metadata         waMetadata      `json:"metadata"`
	Contacts         []waContact     `json:"contacts"`
	Messages         []waMessage     `json:"messages"`
	MessageEchoes    []waMessage     `json:"message_echoes"`
	Statuses         []waStatus      `json:"statuses"`
}

type waMetadata struct {
	DisplayPhoneNumber string `json:"display_phone_number"`
	PhoneNumberID      string `json:"phone_number_id"`
}

type waContact struct {
	Profile struct {
		Name string `json:"name"`
	} `json:"profile"`
	WaID string `json:"wa_id"`
}

type waMessage struct {
	From string `json:"from"`
	ID   string `json:"id"`
	Type string `json:"type"`
	Text struct {
		Body string `json:"body"`
	} `json:"text"`
	Timestamp string `json:"timestamp"`
}

type waStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"` // sent, delivered, read, failed
}

// ---------------------------------------------------------------------
// POST /whatsapp/webhook — recebimento
// ---------------------------------------------------------------------

func whatsappReceiveHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		// Sempre responder 200 rapido - Meta reenvia ate 3x se demorar
		// mais de alguns segundos ou se receber erro.
		defer func() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("EVENT_RECEIVED"))
		}()

		if secret := os.Getenv("META_APP_SECRET"); secret != "" {
			if !verifyMetaSignature(r, body, secret) {
				log.Printf("whatsapp: assinatura invalida - possivel requisicao falsa, ignorando")
				return
			}
		}

		var payload waWebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("whatsapp: erro ao decodificar payload: %v | body: %s", err, truncate(string(body), 500))
			return
		}

		for _, entry := range payload.Entry {
			for _, change := range entry.Changes {
				switch change.Field {
				case "messages":
					handleInboundMessages(db, change.Value)
				case "smb_message_echoes":
					handleEchoMessages(db, change.Value)
				case "smb_app_state_sync":
					log.Printf("whatsapp: evento de sync de contatos do app recebido (nao processado ainda)")
				default:
					log.Printf("whatsapp: campo de webhook nao tratado: %s", change.Field)
				}
			}
		}
	}
}

// handleInboundMessages processa mensagens que o CLIENTE mandou (entrada).
func handleInboundMessages(db *sql.DB, v waValue) {
	nomePorTelefone := map[string]string{}
	for _, c := range v.Contacts {
		nomePorTelefone[c.WaID] = c.Profile.Name
	}

	for _, msg := range v.Messages {
		if msg.Type != "text" {
			log.Printf("whatsapp: mensagem tipo '%s' recebida de %s - ainda nao suportado (so texto por enquanto)", msg.Type, msg.From)
			continue
		}

		telefone := msg.From
		nome := nomePorTelefone[telefone]

		leadID, iaAtiva, err := upsertLeadFromWhatsApp(db, nome, telefone)
		if err != nil {
			log.Printf("whatsapp: erro ao criar/atualizar lead %s: %v", telefone, err)
			continue
		}

		if err := insertInteraction(db, leadID, "whatsapp", "entrada", msg.Text.Body, nil); err != nil {
			log.Printf("whatsapp: erro ao salvar interacao de entrada do lead %s: %v", leadID, err)
			continue
		}

		log.Printf("whatsapp: mensagem de entrada salva - lead %s (%s): %s", leadID, telefone, truncate(msg.Text.Body, 80))

		if !iaAtiva {
			log.Printf("whatsapp: ia desativada para o lead %s - aguardando atendimento humano", leadID)
			continue
		}

		it, err := generateAndSaveAIReply(db, leadID, "whatsapp")
		if err != nil {
			log.Printf("whatsapp: erro ao gerar resposta da ia para o lead %s: %v", leadID, err)
			continue
		}

		if err := sendWhatsAppMessage(telefone, it.Mensagem); err != nil {
			log.Printf("whatsapp: erro ao enviar resposta da ia para %s: %v", telefone, err)
			continue
		}

		log.Printf("whatsapp: resposta da ia enviada - lead %s (%s): %s", leadID, telefone, truncate(it.Mensagem, 80))
	}
}

// handleEchoMessages processa mensagens que a ADRIANA mandou pelo APP
// (Coexistence espelha essas mensagens pro nosso webhook).
func handleEchoMessages(db *sql.DB, v waValue) {
	for _, msg := range v.MessageEchoes {
		if msg.Type != "text" {
			continue
		}

		// Em echo, "msg.From" e o proprio numero do negocio, nao do cliente.
		// O destinatario real precisa ser resolvido pelo contexto da
		// conversa - a Meta nao manda o campo "to" de forma direta em
		// todo evento de echo. TODO: confirmar campo exato na primeira
		// mensagem real recebida (o formato pode variar) e ajustar aqui.
		log.Printf("whatsapp: echo de mensagem enviada pelo app recebido: %s", truncate(msg.Text.Body, 80))

		// TODO: uma vez identificado o telefone de destino, chamar:
		//   insertInteraction(db, leadID, "whatsapp", "saida", msg.Text.Body, ptr("humano"))
		// e desligar ia_ativa do lead, igual ja fazemos em createInteractionHandler.
	}
}

// ---------------------------------------------------------------------
// Helpers compartilhados
// ---------------------------------------------------------------------

// upsertLeadFromWhatsApp reaproveita o mesmo padrao de
// createOrUpdateLeadHandler (INSERT ... ON CONFLICT por telefone),
// mas chamado internamente pelo webhook, sem passar por HTTP.
func upsertLeadFromWhatsApp(db *sql.DB, nome, telefone string) (leadID string, iaAtiva bool, err error) {
	telefone = strings.TrimSpace(telefone)

	var existingID string
	errCheck := db.QueryRow(`SELECT id FROM crm_leads WHERE telefone = ?`, telefone).Scan(&existingID)
	isNewLead := errCheck == sql.ErrNoRows

	_, err = db.Exec(`
		INSERT INTO crm_leads (nome, telefone, origem, campanha)
		VALUES (?, ?, 'whatsapp', 'organico')
		ON CONFLICT(telefone) DO UPDATE SET
			nome = COALESCE(NULLIF(excluded.nome, ''), crm_leads.nome)
	`, nome, telefone)
	if err != nil {
		return "", false, err
	}

	if isNewLead {
		go notifyNewLeadTelegram(nome, telefone, "whatsapp", "organico")
	}

	err = db.QueryRow(`SELECT id, ia_ativa FROM crm_leads WHERE telefone = ?`, telefone).Scan(&leadID, &iaAtiva)
	return leadID, iaAtiva, err
}

// insertInteraction grava uma interacao. origemResposta pode ser nil
// (mensagem de entrada nao tem origem de resposta).
func insertInteraction(db *sql.DB, leadID, canal, direcao, mensagem string, origemResposta *string) error {
	_, err := db.Exec(`
		INSERT INTO crm_interactions (lead_id, canal, direcao, mensagem, origem_resposta)
		VALUES (?, ?, ?, ?, ?)
	`, leadID, canal, direcao, mensagem, origemResposta)
	return err
}

// verifyMetaSignature confere o header X-Hub-Signature-256 contra o
// corpo bruto da requisicao, usando META_APP_SECRET (HMAC-SHA256).
func verifyMetaSignature(r *http.Request, body []byte, secret string) bool {
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if sigHeader == "" || !strings.HasPrefix(sigHeader, "sha256=") {
		return false
	}
	expectedSig := strings.TrimPrefix(sigHeader, "sha256=")

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	computedSig := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expectedSig), []byte(computedSig))
}


// ---------------------------------------------------------------------
// Envio de mensagem via Meta Cloud API
// ---------------------------------------------------------------------

type waSendRequest struct {
	MessagingProduct string `json:"messaging_product"`
	To               string `json:"to"`
	Type             string `json:"type"`
	Text             struct {
		Body string `json:"body"`
	} `json:"text"`
}

// sendWhatsAppMessage envia uma mensagem de texto simples via Graph API.
// Requer WHATSAPP_ACCESS_TOKEN e WHATSAPP_PHONE_NUMBER_ID no .env -
// so preenchidos quando a conta Meta estiver verificada e o numero
// conectado (ver CONTEXTO_WHATSAPP_API.txt, secao 5).
func sendWhatsAppMessage(to, body string) error {
	accessToken := os.Getenv("WHATSAPP_ACCESS_TOKEN")
	phoneNumberID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")

	if accessToken == "" || phoneNumberID == "" {
		return fmt.Errorf("WHATSAPP_ACCESS_TOKEN ou WHATSAPP_PHONE_NUMBER_ID nao configurados - envio pulado (esperado ate a conta Meta ser verificada)")
	}

	reqBody := waSendRequest{
		MessagingProduct: "whatsapp",
		To:               to,
		Type:             "text",
	}
	reqBody.Text.Body = body

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://graph.facebook.com/v20.0/%s/messages", phoneNumberID)
	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+accessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("meta retornou %d: %s", resp.StatusCode, truncate(string(respBytes), 300))
	}

	return nil
}