package main

// env_tools.go
//
// Ferramenta de diagnostico do arquivo .env do backend. Detecta os padroes
// de bug reais encontrados na migracao Hetzner -> Hostinger:
//   - chaves duplicadas (causou autenticacao inconsistente com N8N_API_KEY)
//   - URLs apontando para localhost/127.0.0.1 (causou N8N_BASE_URL orfa)
//   - valores vazios
//
// SEGURANCA: esta tool NUNCA devolve o valor real de nenhuma variavel para
// o modelo. Todo valor e mascarado antes de sair da funcao.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const envDefaultPath = "/root/hokma/backend/.env"

// envMaskValue mostra so os primeiros 3 caracteres do valor, o resto vira "***".
// Nunca expõe o valor real da credencial para o modelo.
func envMaskValue(v string) string {
	if len(v) == 0 {
		return "(vazio)"
	}
	if len(v) <= 3 {
		return "***"
	}
	return v[:3] + "***(" + fmt.Sprintf("%d chars", len(v)) + ")"
}

// n8nDiagnoseEnv analisa o arquivo .env do backend em busca de problemas
// estruturais conhecidos, sem nunca expor valores reais de credenciais.
// args (JSON): { "path": "..." } (opcional, default /root/hokma/backend/.env)
func envDiagnoseConfig(args string) string {
	var raw map[string]string
	_ = json.Unmarshal([]byte(args), &raw) // args pode vir vazio, tudo bem

	path := envDefaultPath
	if p, ok := raw["path"]; ok && p != "" {
		path = p
	}

	f, err := os.Open(path)
	if err != nil {
		return errJSON("nao foi possivel abrir " + path + ": " + err.Error())
	}
	defer f.Close()

	type occurrence struct {
		Line   int    `json:"line"`
		Masked string `json:"masked_value"`
	}

	keyOccurrences := map[string][]occurrence{}
	lineNum := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, "=")
		if idx == -1 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		keyOccurrences[key] = append(keyOccurrences[key], occurrence{
			Line:   lineNum,
			Masked: envMaskValue(value),
		})
	}

	type finding struct {
		Key    string `json:"key"`
		Issue  string `json:"issue"`
		Detail string `json:"detail"`
	}
	var findings []finding

	for key, occs := range keyOccurrences {
		// checagem 1: chave duplicada
		if len(occs) > 1 {
			lines := []string{}
			for _, o := range occs {
				lines = append(lines, fmt.Sprintf("linha %d (%s)", o.Line, o.Masked))
			}
			findings = append(findings, finding{
				Key:    key,
				Issue:  "chave_duplicada",
				Detail: fmt.Sprintf("Aparece %d vezes: %s. O comportamento real depende de qual linha o systemd carrega por ultimo - risco de inconsistencia.", len(occs), strings.Join(lines, "; ")),
			})
		}
		// checagem 2: valor vazio
		for _, o := range occs {
			if o.Masked == "(vazio)" {
				findings = append(findings, finding{
					Key:    key,
					Issue:  "valor_vazio",
					Detail: fmt.Sprintf("Linha %d sem valor definido.", o.Line),
				})
			}
		}
		// checagem 3: URL apontando para localhost/127.0.0.1 em variavel *_URL
		if strings.Contains(strings.ToUpper(key), "URL") {
			// precisa reabrir a linha original pra checar o valor sem mascarar
			// (checagem de padrao, nao expoe o valor - so confirma presenca do padrao)
			f2, err := os.Open(path)
			if err == nil {
				sc2 := bufio.NewScanner(f2)
				ln := 0
				for sc2.Scan() {
					ln++
					l := strings.TrimSpace(sc2.Text())
					if strings.HasPrefix(l, key+"=") {
						val := strings.TrimSpace(strings.TrimPrefix(l, key+"="))
						if strings.Contains(val, "127.0.0.1") || strings.Contains(val, "localhost") {
							findings = append(findings, finding{
								Key:    key,
								Issue:  "url_localhost_suspeita",
								Detail: fmt.Sprintf("Linha %d aponta para localhost/127.0.0.1. Se essa variavel deveria apontar para um servico externo, isso pode sobrescrever silenciosamente o endpoint correto (mesmo padrao do bug N8N_BASE_URL orfa).", ln),
							})
						}
					}
				}
				f2.Close()
			}
		}
	}

	status := "saudavel"
	if len(findings) > 0 {
		status = "problemas_encontrados"
	}

	out, err := json.Marshal(map[string]any{
		"status":       "ok",
		"arquivo":      path,
		"total_chaves": len(keyOccurrences),
		"diagnostico":  status,
		"total_issues": len(findings),
		"findings":     findings,
	})
	if err != nil || len(out) == 0 {
		return `{"status":"error","error":"falha ao montar diagnostico do .env"}`
	}
	return string(out)
}
