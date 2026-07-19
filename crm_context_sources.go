package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
)

type putContextSourceRequest struct {
	Conteudo string `json:"conteudo"`
}

func putContextSourceHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		fonte := r.PathValue("fonte")
		if fonte == "" {
			writeErr(w, http.StatusBadRequest, "fonte obrigatoria na URL")
			return
		}
		var req putContextSourceRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		_, err := db.Exec(`
			INSERT INTO crm_context_sources (fonte, conteudo, atualizado_em)
			VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
			ON CONFLICT(fonte) DO UPDATE SET
				conteudo = excluded.conteudo,
				atualizado_em = excluded.atualizado_em
		`, fonte, req.Conteudo)
		if err != nil {
			log.Printf("crm-context-sources: erro ao salvar fonte %s: %v", fonte, err)
			writeErr(w, http.StatusInternalServerError, "erro ao salvar contexto")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "fonte": fonte})
	}
}
