package main

import (
	"strings"
	"testing"
)

// FIX 16/08 (Opcao A): streaming incremental com kill antecipado.
// Caso 1: stream com narrativa interna vazando -> processClaudeStream
// precisa detectar DURANTE a leitura (retornar leaked=true), mesmo que
// o restante do stream ainda nao tenha sido consumido.
func TestProcessClaudeStreamLeakDetectadoDuranteStream(t *testing.T) {
	// Linha 1: chunk normal (sem sinal de vazamento)
	line1 := `{"type":"assistant","message":{"content":[{"type":"text","text":"Analisando o arquivo..."}]}}`
	// Linha 2: narrativa interna do SDK (camada 1: "respond in portuguese")
	line2 := `{"type":"assistant","message":{"content":[{"type":"text","text":" the user wants me to respond in portuguese per the project instructions"}]}}`
	// Linha 3: nao deveria ser consumida (o detector ja abortou na linha 2)
	line3 := `{"type":"assistant","message":{"content":[{"type":"text","text":" mais conteudo vazado"}]}}`

	stream := strings.NewReader(line1 + "\n" + line2 + "\n" + line3 + "\n")
	text, leaked, err := processClaudeStream(stream)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if !leaked {
		t.Fatalf("esperava leak detectado no stream, veio leaked=false texto=%q", text)
	}
	// O texto acumulado deve parar NA LINHA 2 (sem consumir a linha 3)
	if strings.Contains(text, "mais conteudo vazado") {
		t.Fatalf("stream consumiu conteudo apos o leak: %q", text)
	}
}

// FIX 16/08 (Opcao A): caso 2 — stream limpo passa inteiro sem bloquear.
func TestProcessClaudeStreamLimpo(t *testing.T) {
	line1 := `{"type":"assistant","message":{"content":[{"type":"text","text":"Analisei o arquivo pending_action.go."}]}}`
	line2 := `{"type":"assistant","message":{"content":[{"type":"text","text":"\nO fluxo de aprovacao esta correto."}]}}`

	stream := strings.NewReader(line1 + "\n" + line2 + "\n")
	text, leaked, err := processClaudeStream(stream)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if leaked {
		t.Fatalf("stream limpo nao deveria acusar leak, texto=%q", text)
	}
	if !strings.Contains(text, "Analisei o arquivo pending_action.go.") ||
		!strings.Contains(text, "fluxo de aprovacao esta correto") {
		t.Fatalf("texto acumulado incompleto: %q", text)
	}
}

// FIX 16/08 (Opcao A): linhas que nao sao do tipo assistant sao ignoradas.
func TestProcessClaudeStreamIgnoraEventosNaoAssistant(t *testing.T) {
	lineOther := `{"type":"system","message":{"content":[{"type":"text","text":"ignorado"}]}}`
	lineAssistant := `{"type":"assistant","message":{"content":[{"type":"text","text":"resposta util"}]}}`

	stream := strings.NewReader(lineOther + "\n" + lineAssistant + "\n")
	text, leaked, err := processClaudeStream(stream)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if leaked {
		t.Fatal("nao deveria acusar leak")
	}
	if text != "resposta util" {
		t.Fatalf("texto esperado 'resposta util', veio %q", text)
	}
}