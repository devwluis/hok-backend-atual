package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
)

type EmpreendimentoPlanta struct {
	ID              int64    `json:"id"`
	EmpreendimentoID int64   `json:"empreendimento_id"`
	Tipologia       string   `json:"tipologia"`
	AreaM2          *float64 `json:"area_m2,omitempty"`
	PrecoAPartir    *float64 `json:"preco_a_partir,omitempty"`
	Disponivel      bool     `json:"disponivel"`
}

type Empreendimento struct {
	ID                 int64                   `json:"id"`
	Nome               string                  `json:"nome"`
	Incorporadora      string                  `json:"incorporadora"`
	Localizacao        string                  `json:"localizacao,omitempty"`
	Diferenciais       string                  `json:"diferenciais,omitempty"`
	Lazer              string                  `json:"lazer,omitempty"`
	EntregaPrevista    string                  `json:"entrega_prevista,omitempty"`
	VideoDecoradoURL   string                  `json:"video_decorado_url,omitempty"`
	EbookURL           string                  `json:"ebook_url,omitempty"`
	FotosAlbumURL      string                  `json:"fotos_album_url,omitempty"`
	SimuladorPagamento string                  `json:"simulador_pagamento,omitempty"`
	Status             string                  `json:"status"`
	CriadoEm           string                  `json:"criado_em"`
	AtualizadoEm       string                  `json:"atualizado_em"`
	Plantas            []EmpreendimentoPlanta  `json:"plantas,omitempty"`
}

// ---------------------------------------------------------------------
// GET /crm/empreendimentos  (filtros opcionais: ?incorporadora=X&status=Y)
// ---------------------------------------------------------------------
func listEmpreendimentosHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		incorporadora := r.URL.Query().Get("incorporadora")
		status := r.URL.Query().Get("status")

		query := `SELECT id, nome, incorporadora, localizacao, diferenciais, lazer,
			entrega_prevista, video_decorado_url, ebook_url, fotos_album_url,
			simulador_pagamento, status, criado_em, atualizado_em
			FROM empreendimentos WHERE 1=1`
		var args []interface{}
		if incorporadora != "" {
			query += " AND incorporadora LIKE ?"
			args = append(args, "%"+incorporadora+"%")
		}
		if status != "" {
			query += " AND status = ?"
			args = append(args, status)
		}
		query += " ORDER BY nome"

		rows, err := db.Query(query, args...)
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao listar: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao listar empreendimentos")
			return
		}
		defer rows.Close()

		var lista []Empreendimento
		for rows.Next() {
			var e Empreendimento
			if err := rows.Scan(&e.ID, &e.Nome, &e.Incorporadora, &e.Localizacao, &e.Diferenciais,
				&e.Lazer, &e.EntregaPrevista, &e.VideoDecoradoURL, &e.EbookURL, &e.FotosAlbumURL,
				&e.SimuladorPagamento, &e.Status, &e.CriadoEm, &e.AtualizadoEm); err != nil {
				continue
			}
			lista = append(lista, e)
		}
		writeJSON(w, http.StatusOK, lista)
	}
}

// ---------------------------------------------------------------------
// GET /crm/empreendimentos/{id}  (inclui plantas)
// ---------------------------------------------------------------------
func getEmpreendimentoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "id invalido")
			return
		}
		var e Empreendimento
		err = db.QueryRow(`SELECT id, nome, incorporadora, localizacao, diferenciais, lazer,
			entrega_prevista, video_decorado_url, ebook_url, fotos_album_url,
			simulador_pagamento, status, criado_em, atualizado_em
			FROM empreendimentos WHERE id = ?`, id).Scan(
			&e.ID, &e.Nome, &e.Incorporadora, &e.Localizacao, &e.Diferenciais,
			&e.Lazer, &e.EntregaPrevista, &e.VideoDecoradoURL, &e.EbookURL, &e.FotosAlbumURL,
			&e.SimuladorPagamento, &e.Status, &e.CriadoEm, &e.AtualizadoEm)
		if err == sql.ErrNoRows {
			writeErr(w, http.StatusNotFound, "empreendimento nao encontrado")
			return
		}
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao buscar %d: %v", id, err)
			writeErr(w, http.StatusInternalServerError, "erro ao buscar empreendimento")
			return
		}

		rows, err := db.Query(`SELECT id, empreendimento_id, tipologia, area_m2, preco_a_partir, disponivel
			FROM empreendimento_plantas WHERE empreendimento_id = ?`, id)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var p EmpreendimentoPlanta
				var disp int
				if err := rows.Scan(&p.ID, &p.EmpreendimentoID, &p.Tipologia, &p.AreaM2, &p.PrecoAPartir, &disp); err == nil {
					p.Disponivel = disp != 0
					e.Plantas = append(e.Plantas, p)
				}
			}
		}
		writeJSON(w, http.StatusOK, e)
	}
}

// ---------------------------------------------------------------------
// POST /crm/empreendimentos
// ---------------------------------------------------------------------
type createEmpreendimentoRequest struct {
	Nome               string `json:"nome"`
	Incorporadora      string `json:"incorporadora"`
	Localizacao        string `json:"localizacao"`
	Diferenciais       string `json:"diferenciais"`
	Lazer              string `json:"lazer"`
	EntregaPrevista    string `json:"entrega_prevista"`
	VideoDecoradoURL   string `json:"video_decorado_url"`
	EbookURL           string `json:"ebook_url"`
	FotosAlbumURL      string `json:"fotos_album_url"`
	SimuladorPagamento string `json:"simulador_pagamento"`
	Status             string `json:"status"`
}

func createEmpreendimentoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		var req createEmpreendimentoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		if req.Nome == "" || req.Incorporadora == "" {
			writeErr(w, http.StatusBadRequest, "nome e incorporadora sao obrigatorios")
			return
		}
		if req.Status == "" {
			req.Status = "ativo"
		}
		res, err := db.Exec(`INSERT INTO empreendimentos
			(nome, incorporadora, localizacao, diferenciais, lazer, entrega_prevista,
			 video_decorado_url, ebook_url, fotos_album_url, simulador_pagamento, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			req.Nome, req.Incorporadora, req.Localizacao, req.Diferenciais, req.Lazer,
			req.EntregaPrevista, req.VideoDecoradoURL, req.EbookURL, req.FotosAlbumURL,
			req.SimuladorPagamento, req.Status)
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao criar: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao criar empreendimento")
			return
		}
		id, _ := res.LastInsertId()
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
	}
}

// ---------------------------------------------------------------------
// PATCH /crm/empreendimentos/{id}
// ---------------------------------------------------------------------
type updateEmpreendimentoRequest struct {
	Nome               *string `json:"nome"`
	Incorporadora      *string `json:"incorporadora"`
	Localizacao        *string `json:"localizacao"`
	Diferenciais       *string `json:"diferenciais"`
	Lazer              *string `json:"lazer"`
	EntregaPrevista    *string `json:"entrega_prevista"`
	VideoDecoradoURL   *string `json:"video_decorado_url"`
	EbookURL           *string `json:"ebook_url"`
	FotosAlbumURL      *string `json:"fotos_album_url"`
	SimuladorPagamento *string `json:"simulador_pagamento"`
	Status             *string `json:"status"`
}

func updateEmpreendimentoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "id invalido")
			return
		}
		var req updateEmpreendimentoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		_, err = db.Exec(`UPDATE empreendimentos SET
			nome = COALESCE(?, nome),
			incorporadora = COALESCE(?, incorporadora),
			localizacao = COALESCE(?, localizacao),
			diferenciais = COALESCE(?, diferenciais),
			lazer = COALESCE(?, lazer),
			entrega_prevista = COALESCE(?, entrega_prevista),
			video_decorado_url = COALESCE(?, video_decorado_url),
			ebook_url = COALESCE(?, ebook_url),
			fotos_album_url = COALESCE(?, fotos_album_url),
			simulador_pagamento = COALESCE(?, simulador_pagamento),
			status = COALESCE(?, status),
			atualizado_em = CURRENT_TIMESTAMP
			WHERE id = ?`,
			req.Nome, req.Incorporadora, req.Localizacao, req.Diferenciais, req.Lazer,
			req.EntregaPrevista, req.VideoDecoradoURL, req.EbookURL, req.FotosAlbumURL,
			req.SimuladorPagamento, req.Status, id)
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao atualizar %d: %v", id, err)
			writeErr(w, http.StatusInternalServerError, "erro ao atualizar empreendimento")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ---------------------------------------------------------------------
// DELETE /crm/empreendimentos/{id}  (soft delete -> status = 'inativo')
// ---------------------------------------------------------------------
func deleteEmpreendimentoHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "id invalido")
			return
		}
		_, err = db.Exec(`UPDATE empreendimentos SET status = 'inativo', atualizado_em = CURRENT_TIMESTAMP WHERE id = ?`, id)
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao desativar %d: %v", id, err)
			writeErr(w, http.StatusInternalServerError, "erro ao desativar empreendimento")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ---------------------------------------------------------------------
// POST /crm/empreendimentos/{id}/plantas
// ---------------------------------------------------------------------
type createPlantaRequest struct {
	Tipologia    string   `json:"tipologia"`
	AreaM2       *float64 `json:"area_m2"`
	PrecoAPartir *float64 `json:"preco_a_partir"`
	Disponivel   *bool    `json:"disponivel"`
}

func createPlantaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		empID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "id invalido")
			return
		}
		var req createPlantaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		disponivel := 1
		if req.Disponivel != nil && !*req.Disponivel {
			disponivel = 0
		}
		res, err := db.Exec(`INSERT INTO empreendimento_plantas
			(empreendimento_id, tipologia, area_m2, preco_a_partir, disponivel)
			VALUES (?, ?, ?, ?, ?)`, empID, req.Tipologia, req.AreaM2, req.PrecoAPartir, disponivel)
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao criar planta: %v", err)
			writeErr(w, http.StatusInternalServerError, "erro ao criar planta")
			return
		}
		id, _ := res.LastInsertId()
		writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
	}
}

// ---------------------------------------------------------------------
// PATCH /crm/empreendimentos/{id}/plantas/{planta_id}
// ---------------------------------------------------------------------
type updatePlantaRequest struct {
	Tipologia    *string  `json:"tipologia"`
	AreaM2       *float64 `json:"area_m2"`
	PrecoAPartir *float64 `json:"preco_a_partir"`
	Disponivel   *bool    `json:"disponivel"`
}

func updatePlantaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		plantaID, err := strconv.ParseInt(r.PathValue("planta_id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "planta_id invalido")
			return
		}
		var req updatePlantaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, "corpo da requisicao invalido")
			return
		}
		var dispPtr *int
		if req.Disponivel != nil {
			v := 0
			if *req.Disponivel {
				v = 1
			}
			dispPtr = &v
		}
		_, err = db.Exec(`UPDATE empreendimento_plantas SET
			tipologia = COALESCE(?, tipologia),
			area_m2 = COALESCE(?, area_m2),
			preco_a_partir = COALESCE(?, preco_a_partir),
			disponivel = COALESCE(?, disponivel)
			WHERE id = ?`, req.Tipologia, req.AreaM2, req.PrecoAPartir, dispPtr, plantaID)
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao atualizar planta %d: %v", plantaID, err)
			writeErr(w, http.StatusInternalServerError, "erro ao atualizar planta")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ---------------------------------------------------------------------
// DELETE /crm/empreendimentos/{id}/plantas/{planta_id}
// ---------------------------------------------------------------------
func deletePlantaHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireHokAuth(w, r) {
			return
		}
		plantaID, err := strconv.ParseInt(r.PathValue("planta_id"), 10, 64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "planta_id invalido")
			return
		}
		_, err = db.Exec(`DELETE FROM empreendimento_plantas WHERE id = ?`, plantaID)
		if err != nil {
			log.Printf("crm-empreendimentos: erro ao remover planta %d: %v", plantaID, err)
			writeErr(w, http.StatusInternalServerError, "erro ao remover planta")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

// ---------------------------------------------------------------------
// RegisterEmpreendimentosRoutes
// ---------------------------------------------------------------------
func RegisterEmpreendimentosRoutes(mux *http.ServeMux, db *sql.DB) {
	mux.HandleFunc("GET /crm/empreendimentos", listEmpreendimentosHandler(db))
	mux.HandleFunc("GET /crm/empreendimentos/{id}", getEmpreendimentoHandler(db))
	mux.HandleFunc("POST /crm/empreendimentos", createEmpreendimentoHandler(db))
	mux.HandleFunc("PATCH /crm/empreendimentos/{id}", updateEmpreendimentoHandler(db))
	mux.HandleFunc("DELETE /crm/empreendimentos/{id}", deleteEmpreendimentoHandler(db))
	mux.HandleFunc("POST /crm/empreendimentos/{id}/plantas", createPlantaHandler(db))
	mux.HandleFunc("PATCH /crm/empreendimentos/{id}/plantas/{planta_id}", updatePlantaHandler(db))
	mux.HandleFunc("DELETE /crm/empreendimentos/{id}/plantas/{planta_id}", deletePlantaHandler(db))
}

// ---------------------------------------------------------------------
// getEmpreendimentosContextForLead: usado pela IA (crm_ai.go) para puxar
// dados estruturados e confiaveis de empreendimentos cadastrados manualmente,
// filtrando por keyword (nome ou incorporadora).
// ---------------------------------------------------------------------
func getEmpreendimentosContextForLead(db *sql.DB, keywords []string) (string, error) {
	rows, err := db.Query(`SELECT e.nome, e.incorporadora, e.localizacao, e.diferenciais, e.lazer,
		e.entrega_prevista, e.simulador_pagamento,
		p.tipologia, p.area_m2, p.preco_a_partir, p.disponivel
		FROM empreendimentos e
		LEFT JOIN empreendimento_plantas p ON p.empreendimento_id = e.id
		WHERE e.status = 'ativo'
		ORDER BY e.nome`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	type plantaInfo struct {
		tipologia    string
		areaM2       sql.NullFloat64
		precoAPartir sql.NullFloat64
		disponivel   int
	}
	type empInfo struct {
		nome, incorporadora, localizacao, diferenciais, lazer, entrega, simulador string
		plantas                                                                  []plantaInfo
	}
	ordem := []string{}
	agrupado := map[string]*empInfo{}

	for rows.Next() {
		var nome, incorporadora, localizacao, diferenciais, lazer, entrega, simulador sql.NullString
		var tipologia sql.NullString
		var areaM2, precoAPartir sql.NullFloat64
		var disponivel sql.NullInt64
		if err := rows.Scan(&nome, &incorporadora, &localizacao, &diferenciais, &lazer,
			&entrega, &simulador, &tipologia, &areaM2, &precoAPartir, &disponivel); err != nil {
			continue
		}
		key := nome.String
		if _, ok := agrupado[key]; !ok {
			agrupado[key] = &empInfo{
				nome: nome.String, incorporadora: incorporadora.String, localizacao: localizacao.String,
				diferenciais: diferenciais.String, lazer: lazer.String, entrega: entrega.String,
				simulador: simulador.String,
			}
			ordem = append(ordem, key)
		}
		if tipologia.Valid {
			agrupado[key].plantas = append(agrupado[key].plantas, plantaInfo{
				tipologia: tipologia.String, areaM2: areaM2, precoAPartir: precoAPartir, disponivel: int(disponivel.Int64),
			})
		}
	}

	textoLower := func(s string) string {
		r := []rune(s)
		for i, c := range r {
			if c >= 'A' && c <= 'Z' {
				r[i] = c + 32
			}
		}
		return string(r)
	}

	var resultado string
	for _, key := range ordem {
		e := agrupado[key]
		blob := textoLower(e.nome + " " + e.incorporadora + " " + e.localizacao)
		bateu := len(keywords) == 0
		for _, kw := range keywords {
			if containsSubstr(blob, kw) {
				bateu = true
				break
			}
		}
		if !bateu {
			continue
		}
		txt := "\n\n### " + e.nome + " (" + e.incorporadora + ")\n"
		if e.localizacao != "" {
			txt += "Localizacao: " + e.localizacao + "\n"
		}
		if e.entrega != "" {
			txt += "Entrega: " + e.entrega + "\n"
		}
		if e.diferenciais != "" {
			txt += "Diferenciais: " + e.diferenciais + "\n"
		}
		if e.lazer != "" {
			txt += "Lazer: " + e.lazer + "\n"
		}
		if e.simulador != "" {
			txt += "Formas de pagamento: " + e.simulador + "\n"
		}
		if len(e.plantas) > 0 {
			txt += "Plantas disponiveis:\n"
			for _, p := range e.plantas {
				status := "disponivel"
				if p.disponivel == 0 {
					status = "indisponivel"
				}
				txt += "- " + p.tipologia
				if p.areaM2.Valid {
					txt += " (" + strconv.FormatFloat(p.areaM2.Float64, 'f', 0, 64) + "m2)"
				}
				if p.precoAPartir.Valid {
					txt += " a partir de R$ " + strconv.FormatFloat(p.precoAPartir.Float64, 'f', 0, 64)
				}
				txt += " [" + status + "]\n"
			}
		}
		resultado += txt
	}
	return resultado, nil
}

func containsSubstr(s, sub string) bool {
	return len(sub) > 0 && (len(s) >= len(sub)) && (indexOfSubstr(s, sub) >= 0)
}

func indexOfSubstr(s, sub string) int {
	n, m := len(s), len(sub)
	for i := 0; i+m <= n; i++ {
		if s[i:i+m] == sub {
			return i
		}
	}
	return -1
}
