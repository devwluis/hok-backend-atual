package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
)

type crmContext struct {
	Conteudo     string `json:"conteudo"`
	AtualizadoEm string `json:"atualizado_em"`
}

func getCRMContext(db *sql.DB) (string, error) {
	var conteudo string
	err := db.QueryRow(`SELECT conteudo FROM crm_context WHERE id = 1`).Scan(&conteudo)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	rows, err := db.Query(`SELECT fonte, conteudo FROM crm_context_sources ORDER BY fonte`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var fonte, texto string
			if scanErr := rows.Scan(&fonte, &texto); scanErr == nil && texto != "" {
				conteudo += "\n\n--- " + fonte + " ---\n" + texto
			}
		}
	}

	return conteudo, nil
}

func getContextHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		var c crmContext
		err := db.QueryRow(`SELECT conteudo, atualizado_em FROM crm_context WHERE id = 1`).Scan(&c.Conteudo, &c.AtualizadoEm)
		if err != nil {
			log.Printf("crm-context: erro ao ler contexto: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao ler contexto")
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

type putContextRequest struct {
	Conteudo string `json:"conteudo"`
}

func putContextHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		var req putContextRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		_, err := db.Exec(`
			UPDATE crm_context
			SET conteudo = ?, atualizado_em = strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE id = 1
		`, req.Conteudo)
		if err != nil {
			log.Printf("crm-context: erro ao salvar contexto: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao salvar contexto")
			return
		}
		var c crmContext
		err = db.QueryRow(`SELECT conteudo, atualizado_em FROM crm_context WHERE id = 1`).Scan(&c.Conteudo, &c.AtualizadoEm)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "erro ao ler contexto salvo")
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}
