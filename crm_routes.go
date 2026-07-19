package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	_ "modernc.org/sqlite"
)

func openCRMDB() (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", DB_PATH)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("crm: abrir banco: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode = WAL;`); err != nil {
		return nil, fmt.Errorf("crm: habilitar WAL: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON;`); err != nil {
		return nil, fmt.Errorf("crm: habilitar foreign_keys: %w", err)
	}
	return db, nil
}

type Lead struct {
	ID           string `json:"id"`
	Nome         string `json:"nome"`
	Telefone     string `json:"telefone"`
	Origem       string `json:"origem"`
	Campanha     string `json:"campanha"`
	Status       string `json:"status"`
	IaAtiva      bool   `json:"ia_ativa"`
	CriadoEm     string `json:"criado_em"`
	AtualizadoEm string `json:"atualizado_em"`
}

type Interaction struct {
	ID             string  `json:"id"`
	LeadID         string  `json:"lead_id"`
	Canal          string  `json:"canal"`
	Direcao        string  `json:"direcao"`
	Mensagem       string  `json:"mensagem"`
	OrigemResposta *string `json:"origem_resposta,omitempty"`
	CriadoEm       string  `json:"criado_em"`
}

func RegisterCRMRoutes(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("GET /crm/leads", listLeadsHandler(db))
	mux.HandleFunc("POST /crm/leads", createOrUpdateLeadHandler(db))
	mux.HandleFunc("PATCH /crm/leads/{id}", updateLeadHandler(db))
	mux.HandleFunc("PATCH /crm/leads/{id}/ia", toggleIaHandler(db))
	mux.HandleFunc("GET /crm/leads/{id}/interactions", listInteractionsHandler(db))
	mux.HandleFunc("POST /crm/leads/{id}/interactions", createInteractionHandler(db))
	mux.HandleFunc("POST /crm/leads/{id}/ai-reply", aiReplyHandler(db))
	mux.HandleFunc("GET /crm/context", getContextHandler(db))
	mux.HandleFunc("PUT /crm/context", putContextHandler(db))
	mux.HandleFunc("PUT /crm/context/{fonte}", putContextSourceHandler(db))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	setCORS(w)
	w.WriteHeader(status)
	if v != nil {
		if err := json.NewEncoder(w).Encode(v); err != nil {
			log.Printf("crm: erro ao codificar resposta JSON: %v", err)
		}
	}
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func listLeadsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		status := r.URL.Query().Get("status")
		query := `SELECT id, nome, telefone, origem, campanha, status, ia_ativa, criado_em, atualizado_em
          FROM crm_leads`
		args := []any{}
		if status != "" {
			query += ` WHERE status = ?`
			args = append(args, status)
		}
		query += ` ORDER BY criado_em DESC`
		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("crm: erro ao listar leads: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao listar leads")
			return
		}
		defer rows.Close()
		leads := []Lead{}
		for rows.Next() {
			var l Lead
			if err := rows.Scan(&l.ID, &l.Nome, &l.Telefone, &l.Origem, &l.Campanha, &l.Status, &l.IaAtiva, &l.CriadoEm, &l.AtualizadoEm); err != nil {
				log.Printf("crm: erro ao ler lead: %v", err)
				writeErr(w, http.StatusInternalServerError, "erro ao ler leads")
				return
			}
			leads = append(leads, l)
		}
		writeJSON(w, http.StatusOK, leads)
	}
}

type createLeadRequest struct {
	Nome     string `json:"nome"`
	Telefone string `json:"telefone"`
	Origem   string `json:"origem"`
	Campanha string `json:"campanha"`
}

func createOrUpdateLeadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		var req createLeadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		req.Telefone = strings.TrimSpace(req.Telefone)
		if req.Telefone == "" {
			writeErr(w, http.StatusBadRequest, "telefone e obrigatorio")
			return
		}
		var existingID string
		errCheck := db.QueryRow(`SELECT id FROM crm_leads WHERE telefone = ?`, req.Telefone).Scan(&existingID)
		isNewLead := errCheck == sql.ErrNoRows

		_, err := db.Exec(`
			INSERT INTO crm_leads (nome, telefone, origem, campanha)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(telefone) DO UPDATE SET
				nome     = COALESCE(NULLIF(excluded.nome, ''), crm_leads.nome),
				origem   = COALESCE(NULLIF(excluded.origem, ''), crm_leads.origem),
				campanha = COALESCE(NULLIF(excluded.campanha, ''), crm_leads.campanha)
		`, req.Nome, req.Telefone, req.Origem, req.Campanha)
		if err != nil {
			log.Printf("crm: erro ao criar/atualizar lead: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao salvar lead")
			return
		}

		if isNewLead {
			go notifyNewLeadTelegram(req.Nome, req.Telefone, req.Origem, req.Campanha)
		}
		var l Lead
		err = db.QueryRow(`
			SELECT id, nome, telefone, origem, campanha, status, ia_ativa, criado_em, atualizado_em
			FROM crm_leads WHERE telefone = ?
		`, req.Telefone).Scan(&l.ID, &l.Nome, &l.Telefone, &l.Origem, &l.Campanha, &l.Status, &l.IaAtiva, &l.CriadoEm, &l.AtualizadoEm)
		if err != nil {
			log.Printf("crm: erro ao ler lead recem-salvo: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao salvar lead")
			return
		}
		writeJSON(w, http.StatusCreated, l)
	}
}

type updateLeadRequest struct {
	Nome     *string `json:"nome"`
	Status   *string `json:"status"`
	Origem   *string `json:"origem"`
	Campanha *string `json:"campanha"`
}

func updateLeadHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeErr(w, http.StatusBadRequest, "id e obrigatorio")
			return
		}
		var req updateLeadRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		res, err := db.Exec(`
			UPDATE crm_leads
			SET nome = COALESCE(?, nome), status = COALESCE(?, status),
				origem = COALESCE(?, origem), campanha = COALESCE(?, campanha),
				atualizado_em = strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE id = ?
		`, req.Nome, req.Status, req.Origem, req.Campanha, id)
		if err != nil {
			log.Printf("crm: erro ao atualizar lead %s: %v", id, err)
			writeErr(w, http.StatusInternalServerError, "erro ao atualizar lead")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeErr(w, http.StatusNotFound, "lead nao encontrado")
			return
		}
		var l Lead
		err = db.QueryRow(`
			SELECT id, nome, telefone, origem, campanha, status, ia_ativa, criado_em, atualizado_em
			FROM crm_leads WHERE id = ?
		`, id).Scan(&l.ID, &l.Nome, &l.Telefone, &l.Origem, &l.Campanha, &l.Status, &l.IaAtiva, &l.CriadoEm, &l.AtualizadoEm)
		if err != nil {
			log.Printf("crm: erro ao ler lead atualizado %s: %v", id, err)
			writeErr(w, http.StatusInternalServerError, "erro ao atualizar lead")
			return
		}
		writeJSON(w, http.StatusOK, l)
	}
}

type toggleIaRequest struct {
	IaAtiva bool `json:"ia_ativa"`
}

func toggleIaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		id := r.PathValue("id")
		if id == "" {
			writeErr(w, http.StatusBadRequest, "id e obrigatorio")
			return
		}
		var req toggleIaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		res, err := db.Exec(`
			UPDATE crm_leads SET ia_ativa = ?, atualizado_em = strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE id = ?
		`, req.IaAtiva, id)
		if err != nil {
			log.Printf("crm: erro ao alternar ia_ativa do lead %s: %v", id, err)
			writeErr(w, http.StatusInternalServerError, "erro ao atualizar lead")
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			writeErr(w, http.StatusNotFound, "lead nao encontrado")
			return
		}
		var l Lead
		err = db.QueryRow(`
			SELECT id, nome, telefone, origem, campanha, status, ia_ativa, criado_em, atualizado_em
			FROM crm_leads WHERE id = ?
		`, id).Scan(&l.ID, &l.Nome, &l.Telefone, &l.Origem, &l.Campanha, &l.Status, &l.IaAtiva, &l.CriadoEm, &l.AtualizadoEm)
		if err != nil {
			log.Printf("crm: erro ao ler lead %s: %v", id, err)
			writeErr(w, http.StatusInternalServerError, "erro ao atualizar lead")
			return
		}
		writeJSON(w, http.StatusOK, l)
	}
}

func listInteractionsHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		leadID := r.PathValue("id")
		if leadID == "" {
			writeErr(w, http.StatusBadRequest, "id do lead e obrigatorio")
			return
		}
		rows, err := db.Query(`
			SELECT id, lead_id, canal, direcao, mensagem, origem_resposta, criado_em
			FROM crm_interactions WHERE lead_id = ? ORDER BY criado_em ASC
		`, leadID)
		if err != nil {
			log.Printf("crm: erro ao listar interacoes do lead %s: %v", leadID, err)
			writeErr(w, http.StatusInternalServerError, "erro ao listar interacoes")
			return
		}
		defer rows.Close()
		interactions := []Interaction{}
		for rows.Next() {
			var it Interaction
			if err := rows.Scan(&it.ID, &it.LeadID, &it.Canal, &it.Direcao, &it.Mensagem, &it.OrigemResposta, &it.CriadoEm); err != nil {
				log.Printf("crm: erro ao ler interacao: %v", err)
				writeErr(w, http.StatusInternalServerError, "erro ao ler interacoes")
				return
			}
			interactions = append(interactions, it)
		}
		writeJSON(w, http.StatusOK, interactions)
	}
}

type createInteractionRequest struct {
	Canal          string  `json:"canal"`
	Direcao        string  `json:"direcao"`
	Mensagem       string  `json:"mensagem"`
	OrigemResposta *string `json:"origem_resposta,omitempty"`
}

func createInteractionHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		leadID := r.PathValue("id")
		if leadID == "" {
			writeErr(w, http.StatusBadRequest, "id do lead e obrigatorio")
			return
		}
		var req createInteractionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		if req.Mensagem == "" || req.Canal == "" || req.Direcao == "" {
			writeErr(w, http.StatusBadRequest, "canal, direcao e mensagem sao obrigatorios")
			return
		}
		tx, err := db.Begin()
		if err != nil {
			log.Printf("crm: erro ao iniciar transacao: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		defer tx.Rollback()
		_, err = tx.Exec(`
			INSERT INTO crm_interactions (lead_id, canal, direcao, mensagem, origem_resposta)
			VALUES (?, ?, ?, ?, ?)
		`, leadID, req.Canal, req.Direcao, req.Mensagem, req.OrigemResposta)
		if err != nil {
			log.Printf("crm: erro ao criar interacao para lead %s: %v", leadID, err)
			writeErr(w, http.StatusInternalServerError, "erro ao salvar interacao")
			return
		}
		var it Interaction
		err = tx.QueryRow(`
			SELECT id, lead_id, canal, direcao, mensagem, origem_resposta, criado_em
			FROM crm_interactions WHERE lead_id = ? ORDER BY criado_em DESC, id DESC LIMIT 1
		`, leadID).Scan(&it.ID, &it.LeadID, &it.Canal, &it.Direcao, &it.Mensagem, &it.OrigemResposta, &it.CriadoEm)
		if err != nil {
			log.Printf("crm: erro ao ler interacao recem-criada (lead %s): %v", leadID, err)
			writeErr(w, http.StatusInternalServerError, "erro ao salvar interacao")
			return
		}
		if req.Direcao == "saida" && req.OrigemResposta != nil && *req.OrigemResposta == "humano" {
			if _, err := tx.Exec(`
				UPDATE crm_leads SET ia_ativa = 0, atualizado_em = strftime('%Y-%m-%dT%H:%M:%fZ','now')
				WHERE id = ?
			`, leadID); err != nil {
				log.Printf("crm: erro ao pausar IA do lead %s: %v", leadID, err)
				writeErr(w, http.StatusInternalServerError, "erro ao atualizar lead")
				return
			}
		} else {
			if _, err := tx.Exec(`
				UPDATE crm_leads SET atualizado_em = strftime('%Y-%m-%dT%H:%M:%fZ','now')
				WHERE id = ?
			`, leadID); err != nil {
				log.Printf("crm: erro ao atualizar timestamp do lead %s: %v", leadID, err)
				writeErr(w, http.StatusInternalServerError, "erro ao atualizar lead")
				return
			}
		}
		if err := tx.Commit(); err != nil {
			log.Printf("crm: erro ao commitar transacao: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro interno")
			return
		}
		writeJSON(w, http.StatusCreated, it)
	}
}
