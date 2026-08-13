package main

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
		if _, ok := nodeMap["position"]; !ok {
			// espaca nodes horizontalmente para nao empilhar tudo em (0,0)
			nodeMap["position"] = []any{float64(i * 250), float64(300)}
		}
		if _, ok := nodeMap["parameters"]; !ok {
			nodeMap["parameters"] = map[string]any{}
		}
		nodesRaw[i] = nodeMap
	}
	payload["nodes"] = nodesRaw
	return payload
}
