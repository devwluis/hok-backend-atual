package main

import (
	"strconv"
	"strings"
)

// n8nRepairNodeDefaults preenche campos default ausentes em cada node de um
// payload de workflow (position, parameters) antes de enviar ao n8n. Segue o
// mesmo estilo defensivo de n8nRepairConnections: nunca falha, só completa o
// que falta; se o formato de "nodes" for inesperado, devolve o payload sem
// alteracao.
//
// NOTA IMPORTANTE: NAO seta typeVersion cegamente. Testado contra a API real
// (n8n 2026): node sem typeVersion é aceito e resolvido pelo proprio servidor
// para a versão default; forçar typeVersion=1 pode apontar para uma versão
// antiga de node inexistente na instancia (ex: nodes com minVersion>1),
// quebrando o create/update. Se o chamador enviou typeVersion, ele é mantido.
func n8nRepairNodeDefaults(payload map[string]any) map[string]any {
	nodesRaw, ok := payload["nodes"].([]any)
	if !ok {
		return payload
	}
	for i, n := range nodesRaw {
		nodeMap, ok := n.(map[string]any)
		if !ok {
			continue
		}
		// position corrompido (ex: objeto {"item": ["0","0"]} gerado pelo
		// minimax-m3) e normalizado para [x, y] numerico em vez de quebrar
		// a criacao no n8n; se ausente, espaca horizontalmente.
		positionOK := false
		if pos, exists := nodeMap["position"]; exists && pos != nil {
			if posArr, isArr := pos.([]any); isArr && len(posArr) == 2 {
				valid := true
				for _, c := range posArr {
					if _, isNum := c.(float64); !isNum {
						valid = false
						break
					}
				}
				positionOK = valid
			}
		}
		if !positionOK {
			nodeMap["position"] = []any{float64(i * 250), float64(300)}
		}
		// typeVersion como string (ex: "1.1") normalizado para numero; se o
		// valor nao converter, remove (o n8n resolve para a versao default).
		if tv, exists := nodeMap["typeVersion"]; exists && tv != nil {
			if f, isStr := tv.(string); isStr {
				if v, err := strconv.ParseFloat(strings.TrimSpace(f), 64); err == nil {
					nodeMap["typeVersion"] = v
				} else {
					delete(nodeMap, "typeVersion")
				}
			}
		}
		if _, ok := nodeMap["parameters"]; !ok {
			nodeMap["parameters"] = map[string]any{}
		}
		nodesRaw[i] = nodeMap
	}
	payload["nodes"] = nodesRaw
	return payload
}
