package main

// n8n_xml_guard.go
//
// Guarda de segurança contra XML malicioso em metadata de workflow n8n.
//
// O payload de workflow (nodes[*].parameters — metadata livre gerada pelo
// LLM) é encaminhado para o servidor MCP (validate_workflow) e para a API
// REST do n8n. Esses valores string podem embutir XML criado por um atacante
// (muitas vezes via prompt injection no próprio LLM). Três riscos são cobertos:
//
//  1. XXE no downstream: se o MCP server / n8n core parsear esse XML com um
//     parser vulnerável, uma declaração <!DOCTYPE> / <!ENTITY ... SYSTEM>
//     pode ler arquivos locais, fazer SSRF ou DoS ("billion laughs").
//     → Bloqueamos o payload ANTES de encaminhá-lo.
//
//  2. Reflexão para o LLM: XML malicioso em metadata que é ecoado de volta
//     no relatório de validação pode carregar instruções de prompt injection.
//     → Como bloqueamos na origem, nada desse XML chega ao relatório.
//
//  3. Nodes/params XML inseguros: um payload que declara conteúdo XML com
//     entidades externas é rejeitado pelo mesmo guarda, independentemente
//     do tipo do node.
//
// Uso (enforcer único): aplicar guardWorkflowXML em todos os pontos de
// fronteira — n8nCreateWorkflow, n8nUpdateWorkflow e n8nValidateWorkflowViaMCP.
// Se retornar findings, o payload NÃO deve prosseguir para o MCP/REST.

import (
	"fmt"
	"regexp"
	"strings"
)

// regexes de indicadores XXE. DOCTYPE é o vetor clássico de XXE; ENTITY declara
// entidades (internas = billion laughs, externas com SYSTEM/PUBLIC = leitura de
// arquivo / SSRF). XML de dados normal nunca precisa de nenhum dos dois.
var (
	reDoctype  = regexp.MustCompile(`(?i)<![\s]*DOCTYPE`)
	reEntity   = regexp.MustCompile(`(?i)<![\s]*ENTITY`)
	reExtEnt   = regexp.MustCompile(`(?i)SYSTEM[\s]+['"]`)
	rePubEnt   = regexp.MustCompile(`(?i)PUBLIC[\s]+['"]`)
	reParamEnt = regexp.MustCompile(`(?i)<![\s]*ENTITY[\s]+%`)
)

// dangerousXMLReason devolve um motivo (descrição) se a string contiver XML
// perigoso, ou "" se estiver limpa. Usada tanto para detectar quanto para
// relatar de forma legível onde o problema está.
func dangerousXMLReason(s string) string {
	if s == "" {
		return ""
	}
	lower := strings.ToLower(s)
	if reDoctype.MatchString(lower) {
		return "contém declaração <!DOCTYPE (vetor XXE)"
	}
	if reEntity.MatchString(lower) {
		switch {
		case reParamEnt.MatchString(lower):
			return "contém entity parameter (% ...) — billion laughs / XXE param"
		case reExtEnt.MatchString(lower) || rePubEnt.MatchString(lower):
			return "contém entidade externa (SYSTEM/PUBLIC) — leitura de arquivo/SSRF"
		default:
			return "contém declaração <!ENTITY (possível XXE / entity expansion)"
		}
	}
	return ""
}

// walkValue percorre recursivamente o JSON de um node e chama fn para cada
// valor string, informando o caminho (ex: "options.timeout[0].url").
func walkValue(v any, fn func(path string, s string)) {
	var rec func(prefix string, x any)
	rec = func(prefix string, x any) {
		switch t := x.(type) {
		case string:
			fn(prefix, t)
		case map[string]any:
			for k, c := range t {
				np := k
				if prefix != "" {
					np = prefix + "." + k
				}
				rec(np, c)
			}
		case []any:
			for i, c := range t {
				rec(fmt.Sprintf("%s[%d]", prefix, i), c)
			}
		}
	}
	rec("", v)
}

// guardWorkflowXML varre a metadata de todos os nodes de um payload de
// workflow (campos name/type e, principalmente, parameters aninhados) em busca
// de XML malicioso. Retorna uma lista de findings legíveis; lista vazia =
// payload limpo. É reentrante e nunca modifica o payload.
func guardWorkflowXML(payload map[string]any) []string {
	var findings []string
	nodesRaw, ok := payload["nodes"].([]any)
	if !ok {
		return findings
	}
	for i, n := range nodesRaw {
		node, ok := n.(map[string]any)
		if !ok {
			continue
		}
		label := fmt.Sprintf("node[%d]", i)
		if name, _ := node["name"].(string); name != "" {
			label = fmt.Sprintf("node %q", name)
		}
		// varre name/type (refletidos em relatórios de validação) e parameters
		for _, f := range []string{"name", "type"} {
			if v, ok := node[f].(string); ok {
				if reason := dangerousXMLReason(v); reason != "" {
					findings = append(findings, fmt.Sprintf("%s campo '%s': %s", label, f, reason))
				}
			}
		}
		if params, ok := node["parameters"].(map[string]any); ok {
			walkValue(params, func(path string, s string) {
				if reason := dangerousXMLReason(s); reason != "" {
					if path == "" {
						path = "(raiz)"
					}
					findings = append(findings, fmt.Sprintf("%s parâmetro '%s': %s", label, path, reason))
				}
			})
		}
	}
	return findings
}

// formatGuardFindings monta o texto do relatório de bloqueio.
func formatGuardFindings(findings []string) string {
	return "XML malicioso em metadata — payload NÃO encaminhado ao MCP/n8n:\n- " + strings.Join(findings, "\n- ")
}
