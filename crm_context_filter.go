package main

import (
	"database/sql"
	"log"
	"regexp"
	"strings"
)

var crmStopwords = map[string]bool{
	"organico": true, "manual": true, "teste": true, "verificacao": true,
	"final": true, "meta": true, "ads": true, "lead": true, "leads": true,
	"lancamento": true, "campanha": true, "geral": true, "novo": true,
	"nao": true, "informado": true, "informada": true, "drive": true,
	"whatsapp": true,
}

var crmWordSplitRe = regexp.MustCompile(`[^a-z0-9]+`)

func extractCRMKeywords(texts ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range texts {
		t = strings.ToLower(t)
		for _, w := range crmWordSplitRe.Split(t, -1) {
			if len(w) < 4 || crmStopwords[w] || seen[w] {
				continue
			}
			seen[w] = true
			out = append(out, w)
		}
	}
	return out
}

const maxSourceChars = 4000
const maxTotalSourceChars = 12000

func getCRMContextForLead(db *sql.DB, lead Lead) (string, error) {
	var conteudo string
	err := db.QueryRow(`SELECT conteudo FROM crm_context WHERE id = 1`).Scan(&conteudo)
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}

	keywords := extractCRMKeywords(lead.Campanha, lead.Origem, lead.Nome)
	if len(keywords) == 0 {
		return conteudo, nil
	}

	rows, err := db.Query(`SELECT fonte, conteudo FROM crm_context_sources ORDER BY fonte`)
	if err != nil {
		return conteudo, nil
	}
	defer rows.Close()

	totalAdded := 0
	for rows.Next() {
		var fonte, texto string
		if scanErr := rows.Scan(&fonte, &texto); scanErr != nil || texto == "" {
			continue
		}
		fonteLower := strings.ToLower(fonte)
		textoLower := strings.ToLower(texto)
		matched := false
		for _, kw := range keywords {
			if strings.Contains(fonteLower, kw) || strings.Contains(textoLower, kw) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		trecho := texto
		if len(trecho) > maxSourceChars {
			trecho = trecho[:maxSourceChars] + "\n[...conteudo truncado...]"
		}
		if totalAdded+len(trecho) > maxTotalSourceChars {
			log.Printf("crm-context: limite de contexto atingido, ignorando fonte %s", fonte)
			continue
		}
		conteudo += "\n\n--- " + fonte + " ---\n" + trecho
		totalAdded += len(trecho)
	}

	return conteudo, nil
}
