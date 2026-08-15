package main

// HokMá modo debug.n8n — versão limpa v3
// Analisa workflows n8n via DeepHat/Qwen e sugere fixes
// Suporta JSON nested ("fix": {...}) e flat ("fix.description": "...")
// v3: parser robusto (aspas curly, trailing junk em numeros, code fences multiplos)
//
// ⚠️ ILHA DE DIAGNÓSTICO — NÃO CLASSIFICAR COMO CÓDIGO MORTO:
// Este arquivo é intencionalmente autocontido: a rota /n8n/debug é registrada
// via init() (linha 482) e NENHUM símbolo dele é chamado por outro arquivo.
// É uma ferramenta de diagnóstico legítima (suporte manual ao n8n), mantida
// como ilha de propósito. A análise de "símbolos sem uso externo" vai marcá-lo
// como morto — ignorar. Se um dia for removida, remover inteira (rota + init).

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// === Estruturas ===

type DebugN8nRequest struct {
	Message  string `json:"message"`
	UserID   string `json:"user_id,omitempty"`
	ApplyFix bool   `json:"apply_fix,omitempty"`
}

type DebugN8nResponse struct {
	Reply      string       `json:"reply"`
	Mode       string       `json:"mode"`
	SkillUsed  string       `json:"skill_used"`
	LatencyMs  int64        `json:"latency_ms"`
	Analysis   *N8nAnalysis `json:"analysis,omitempty"`
	Applied    bool         `json:"applied,omitempty"`
	ApplyError string       `json:"apply_error,omitempty"`
	RawAI      string       `json:"raw_ai,omitempty"`
}

type N8nAnalysis struct {
	BrokenNode string  `json:"broken_node"`
	RootCause  string  `json:"root_cause"`
	Fix        N8nFix  `json:"fix"`
	Confidence float64 `json:"confidence"`
}

type N8nFix struct {
	Description string                 `json:"description"`
	Diff        map[string]interface{} `json:"diff"`
}

type n8nWorkflow struct {
	ID    string                   `json:"id,omitempty"`
	Name  string                   `json:"name"`
	Nodes []map[string]interface{} `json:"nodes"`
}

// === Sanitizadores (novos em v3) ===

// stripCodeFences remove markdown code fences (```json, ```JSON, ```)
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

// normalizeQuotes converte aspas tipograficas em retas
func normalizeQuotes(s string) string {
	repl := map[string]string{
		"\u201c": "\"", // left double quote
		"\u201d": "\"", // right double quote
		"\u2018": "'",  // left single quote
		"\u2019": "'",  // right single quote
		"\u00b4": "'",  // acute accent
	}
	for old, neu := range repl {
		s = strings.ReplaceAll(s, old, neu)
	}
	return s
}

// sanitizeJSON limpa lixo comum: numeros com ponto/coma final e trailing commas.
// (RE2 nao suporta lookahead, entao capturamos a "suja" e o delimitador juntos.)
func sanitizeJSON(s string) string {
	// Numero com "." ou "," sobrando antes de } , ] ou quebra de linha.
	// Ex: "timeout": 10000. -> "timeout": 10000
	numJunk := regexp.MustCompile(`(\d+(?:\.\d+)?)[.,]+(\s*[,\]\}])`)
	s = numJunk.ReplaceAllString(s, "${1}${2}")
	// Trailing comma antes de } ou ]
	trailComma := regexp.MustCompile(`,(\s*[}\]])`)
	s = trailComma.ReplaceAllString(s, "${1}")
	return s
}

// firstJSONBlock retorna [inicio, fim] do primeiro bloco {...} balanceado de s.
// Retorna -1, -1 se nao encontrar.
func firstJSONBlock(s string) (int, int) {
	start := strings.Index(s, "{")
	if start == -1 {
		return -1, -1
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case !inStr:
			if c == '{' {
				depth++
			} else if c == '}' {
				depth--
				if depth == 0 {
					return start, i
				}
			}
		}
	}
	return start, -1
}

// numToFloat converte json.Number (ou outros) em float64
func numToFloat(v interface{}) float64 {
	switch x := v.(type) {
	case json.Number:
		f, _ := x.Float64()
		return f
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

// strOf extrai string de uma interface generica
func strOf(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// === Parser principal (v3 robusto) ===

func extractAnalysis(raw string) (*N8nAnalysis, error) {
	cleaned := strings.TrimSpace(raw)

	// Pipeline de limpeza
	cleaned = stripCodeFences(cleaned)
	cleaned = normalizeQuotes(cleaned)
	cleaned = sanitizeJSON(cleaned)

	// Extrai o primeiro {...} balanceado
	if i, j := firstJSONBlock(cleaned); i >= 0 && j > i {
		cleaned = cleaned[i : j+1]
	} else {
		return nil, fmt.Errorf("nenhum bloco JSON encontrado em: %.200s", cleaned)
	}

	// Decodifica usando json.Number pra ter precisao em ints
	m := map[string]interface{}{}
	dec := json.NewDecoder(strings.NewReader(cleaned))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("nao consegui parsear JSON apos sanitizacao: %w | input: %.200s", err, cleaned)
	}

	a := &N8nAnalysis{
		BrokenNode: strOf(m["broken_node"]),
		RootCause:  strOf(m["root_cause"]),
		Confidence: numToFloat(m["confidence"]),
	}

	// fix pode vir nested ou flat
	if fixObj, ok := m["fix"].(map[string]interface{}); ok {
		a.Fix.Description = strOf(fixObj["description"])
		if d, ok := fixObj["diff"].(map[string]interface{}); ok {
			a.Fix.Diff = d
		}
	} else {
		a.Fix.Description = strOf(m["fix.description"])
		if d, ok := m["fix.diff"].(map[string]interface{}); ok {
			a.Fix.Diff = d
		}
	}
	if a.Fix.Diff == nil {
		a.Fix.Diff = map[string]interface{}{}
	}
	return a, nil
}

// === Extrai workflow JSON da mensagem do usuario ===

func extractWorkflowFromMessage(msg string) (*n8nWorkflow, string, bool) {
	jsonStart, jsonEnd := -1, -1
	depth, inString, escape := 0, false, false

	for i, c := range msg {
		switch {
		case escape:
			escape = false
		case c == '\\' && inString:
			escape = true
		case c == '"':
			inString = !inString
		case !inString && c == '{':
			if depth == 0 {
				jsonStart = i
			}
			depth++
		case !inString && c == '}':
			depth--
			if depth == 0 && jsonStart != -1 {
				jsonEnd = i + 1
			}
		}
	}

	if jsonStart == -1 || jsonEnd == -1 {
		return nil, "", false
	}

	jsonStr := msg[jsonStart:jsonEnd]
	var wf n8nWorkflow
	if err := json.Unmarshal([]byte(jsonStr), &wf); err != nil || len(wf.Nodes) == 0 {
		return nil, "", false
	}

	errorMsg := strings.TrimSpace(msg[:jsonStart] + msg[jsonEnd:])
	if errorMsg == "" {
		errorMsg = "(sem erro explicito - analise preventiva do workflow)"
	}
	return &wf, errorMsg, true
}

// === Analisa com DeepHat/Qwen ===

func callDebugN8nAnalysis(workflow *n8nWorkflow, errorMsg string) (*N8nAnalysis, string, error) {
	wfJSON, _ := json.MarshalIndent(workflow, "", "  ")

	const systemPrompt = `Voce e HokMa debug.n8n, especialista em workflows n8n.

TAREFA: dado um workflow JSON (formato n8n) e um erro, retorne:

1. broken_node: nome/ID do no com problema
2. root_cause: causa raiz em portugues
3. fix.description: o que fazer
4. fix.diff: objeto campo->valor (suporta chaves aninhadas com ponto, ex "options.timeout")
5. confidence: 0.0 a 1.0

FORMATO OBRIGATORIO - JSON com chave "fix" sendo OBJETO aninhado:

{"broken_node":"...","root_cause":"...","fix":{"description":"...","diff":{"campo":"valor"}},"confidence":0.85}

REGRAS: responda SOMENTE JSON valido (sem markdown, sem code fences). Portugues brasileiro.`

	userPrompt := fmt.Sprintf("WORKFLOW:\n%s\n\nERRO:\n%s\n\nJSON:", string(wfJSON), errorMsg)

	msgs := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}

	raw, err := callDeepHat("", msgs)
	if err != nil {
		return nil, raw, err
	}

	a, parseErr := extractAnalysis(raw)
	if parseErr != nil {
		log.Printf("parser falhou: %v | raw[0:300]: %.300s", parseErr, raw)
		return &N8nAnalysis{
			BrokenNode: "(resposta nao estruturada)",
			RootCause:  raw,
			Fix: N8nFix{
				Description: "DeepHat nao retornou JSON parseavel",
				Diff:        map[string]interface{}{},
			},
			Confidence: 0.3,
		}, raw, nil
	}
	return a, raw, nil
}

// === Aplica fix via API n8n ===

// applyN8nFix aplica o diff do analista NO WORKFLOW REAL DO SERVIDOR:
// busca o estado atual via GET, localiza o node alvo, aplica só os campos
// do diff e faz PUT. Nunca sobrescreve o workflow com o JSON da mensagem do
// usuário (que pode estar parcial/corrompido), evitando perder nodes,
// connections, settings e credentials que não foram citados na análise.
func applyN8nFix(workflow *n8nWorkflow, analysis *N8nAnalysis) error {
	apiKey := os.Getenv("N8N_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("N8N_API_KEY nao configurada")
	}
	if workflow.ID == "" {
		return fmt.Errorf("workflow precisa ter campo 'id' para atualizar via API")
	}

	currentBody, err := n8nCall("GET", "/workflows/"+workflow.ID, nil)
	if err != nil {
		return fmt.Errorf("falha ao buscar workflow atual via API: %w", err)
	}
	var current map[string]interface{}
	if err := json.Unmarshal(currentBody, &current); err != nil {
		return fmt.Errorf("falha ao parsear workflow atual: %w", err)
	}

	nodesRaw, ok := current["nodes"].([]interface{})
	if !ok {
		return fmt.Errorf("workflow atual sem campo 'nodes' usavel")
	}

	targetIdx := -1
	for i, raw := range nodesRaw {
		node, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := node["name"].(string)
		id, _ := node["id"].(string)
		if name == analysis.BrokenNode || id == analysis.BrokenNode {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		// fallback: busca por substring no nome (mesma tolerancia da v1)
		for i, raw := range nodesRaw {
			node, ok := raw.(map[string]interface{})
			if !ok {
				continue
			}
			if name, _ := node["name"].(string); strings.Contains(strings.ToLower(name), strings.ToLower(analysis.BrokenNode)) {
				targetIdx = i
				break
			}
		}
	}
	if targetIdx == -1 {
		return fmt.Errorf("no '%s' nao encontrado no workflow atual", analysis.BrokenNode)
	}

	node := nodesRaw[targetIdx].(map[string]interface{})
	params, _ := node["parameters"].(map[string]interface{})
	if params == nil {
		params = make(map[string]interface{})
	}
	for k, v := range analysis.Fix.Diff {
		if strings.Contains(k, ".") {
			parts := strings.SplitN(k, ".", 2)
			sub, _ := params[parts[0]].(map[string]interface{})
			if sub == nil {
				sub = make(map[string]interface{})
			}
			sub[parts[1]] = v
			params[parts[0]] = sub
		} else {
			params[k] = v
		}
	}
	node["parameters"] = params
	nodesRaw[targetIdx] = node
	current["nodes"] = nodesRaw

	// gate de seguranca: valida o workflow modificado antes de qualquer PUT
	if !validateWorkflowJSON(current).Valid {
		return fmt.Errorf("workflow modificado nao passa na validacao estatica; fix NAO aplicado")
	}

	payload := n8nCleanPayload(current)
	body, _ := json.Marshal(payload)
	url := n8nBaseURL() + "/workflows/" + workflow.ID

	req, _ := http.NewRequest("PUT", url, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-N8N-API-KEY", apiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("erro n8n: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("n8n HTTP %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// === Formata resposta ===

func formatDebugN8nReply(a *N8nAnalysis) string {
	diffStr := "(vazio)"
	if len(a.Fix.Diff) > 0 {
		b, _ := json.MarshalIndent(a.Fix.Diff, "", "  ")
		diffStr = string(b)
	}
	return fmt.Sprintf("🎯 No com problema: %s\n\n🐛 Causa raiz: %s\n\n💡 Fix: %s\n\n📋 Diff:\n%s\n\n🎲 Confianca: %.0f%%",
		a.BrokenNode, a.RootCause, a.Fix.Description, diffStr, a.Confidence*100)
}

// === Handler HTTP + auto-registro ===

func handleDebugN8n(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	start := time.Now()

	var req DebugN8nRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "JSON invalido", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "campo 'message' obrigatorio", http.StatusBadRequest)
		return
	}

	wf, errorMsg, ok := extractWorkflowFromMessage(req.Message)
	if !ok {
		http.Error(w, "cole um JSON de workflow n8n valido na mensagem", http.StatusBadRequest)
		return
	}

	analysis, rawAI, err := callDebugN8nAnalysis(wf, errorMsg)
	if err != nil {
		http.Error(w, fmt.Sprintf("DeepHat falhou: %v", err), http.StatusBadGateway)
		return
	}

	resp := DebugN8nResponse{
		Reply:     formatDebugN8nReply(analysis),
		Mode:      "debug_n8n",
		SkillUsed: "DeepHat-V1-7B",
		LatencyMs: time.Since(start).Milliseconds(),
		Analysis:  analysis,
		RawAI:     rawAI,
	}

	if req.ApplyFix {
		if err := applyN8nFix(wf, analysis); err != nil {
			resp.Applied = false
			resp.ApplyError = err.Error()
			resp.Reply += fmt.Sprintf("\n\n⚠️ Nao foi possivel aplicar: %s", err.Error())
		} else {
			resp.Applied = true
			resp.Reply += "\n\n✅ Fix aplicado no n8n com sucesso!"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func init() {
	http.HandleFunc("/n8n/debug", handleDebugN8n)
	log.Println("✅ rota /n8n/debug registrada via init()")
}
