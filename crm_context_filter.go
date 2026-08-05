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
			if len(w) < 3 || crmStopwords[w] || seen[w] {
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
const maxTotalFichasChars = 60000

func getCRMContextForLead(db *sql.DB, lead Lead, ultimaMensagem string) (string, error) {
	var conteudo string

	keywords := extractCRMKeywords(lead.Campanha, lead.Origem, lead.Nome, ultimaMensagem)
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

func getCrawlerContextForLead(db *sql.DB, keywords []string) (string, error) {
	rows, err := db.Query(`SELECT context_key, context_value FROM crm_context WHERE context_type = 'empreendimentos_texto'`)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var resultado string
	total := 0
	for rows.Next() {
		var key, texto string
		if err := rows.Scan(&key, &texto); err != nil {
			continue
		}
		textoLower := strings.ToLower(texto)
		bateu := len(keywords) == 0
		for _, kw := range keywords {
			matched, _ := regexp.MatchString(`\b`+regexp.QuoteMeta(kw)+`\b`, textoLower)
			if matched {
				bateu = true
				break
			}
		}
		if !bateu {
			continue
		}
		trecho := texto
		if len(trecho) > maxSourceChars {
			trecho = trecho[:maxSourceChars] + "\n[...conteudo truncado...]"
		}
		if total+len(trecho) > maxTotalSourceChars {
			break
		}
		resultado += "\n\n" + trecho
		total += len(trecho)
	}

	// fallback: ficha nao bateu — carrega todas com R$
	if total == 0 {
		log.Printf("crm-context: fallback R$ (%d kw)", len(keywords))
		fbRows, err2 := db.Query(`SELECT context_value FROM crm_context WHERE context_type = 'ficha_empreendimento_texto' AND context_value LIKE '%R$%'`)
		if err2 == nil {
			defer fbRows.Close()
			for fbRows.Next() {
				var v2 string
				if fbRows.Scan(&v2) != nil || v2 == "" {
					continue
				}
				if len(v2) > maxSourceChars {
					v2 = v2[:maxSourceChars]
				}
				if total+len(v2) > maxTotalSourceChars {
					break
				}
				resultado += "\n\n" + v2
				total += len(v2)
			}
		}
	}
	return resultado, nil
}

// getFichasEmpreendimentosContext retorna fichas completas gravadas pelo extrator
// (context_type='ficha_empreendimento_texto'), filtradas por keywords.
func getFichasEmpreendimentosContext(db *sql.DB, keywords []string) (string, error) {
	rows, err := db.Query(`SELECT context_key, context_value FROM crm_context WHERE context_type = 'ficha_empreendimento_texto'`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var resultado string
	total := 0
	for rows.Next() {
		var key, texto string
		if scanErr := rows.Scan(&key, &texto); scanErr != nil || texto == "" {
			continue
		}
		if len(keywords) > 0 {
			tl := strings.ToLower(texto)
			bateu := false
			for _, kw := range keywords {
				if strings.Contains(tl, kw) {
					bateu = true
					break
				}
			}
			if !bateu {
				continue
			}
		}
		trecho := texto
		if len(trecho) > maxSourceChars {
			trecho = trecho[:maxSourceChars]
		}
		if total+len(trecho) > maxTotalFichasChars {
			break
		}
		resultado += "\n\n" + trecho
		total += len(trecho)
		_ = key
	}
	if total == 0 {
		fbRows, fbErr := db.Query(`SELECT context_value FROM crm_context WHERE context_type = 'ficha_empreendimento_texto' AND context_value LIKE '%R$%'`)
		if fbErr == nil {
			defer fbRows.Close()
			for fbRows.Next() {
				var v string
				if scanErr := fbRows.Scan(&v); scanErr != nil || v == "" {
					continue
				}
				trecho := v
				if len(trecho) > maxSourceChars {
					trecho = trecho[:maxSourceChars]
				}
				if total+len(trecho) > maxTotalFichasChars {
					break
				}
				resultado += "\n\n" + trecho
				total += len(trecho)
			}
		}
	}
	return resultado, nil
}
