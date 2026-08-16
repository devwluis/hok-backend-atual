package main

import (
	"strings"
	"testing"
)

// Caso do incidente: a mensagem do usuario nao dispara o filtro
func TestThresholdCamada2_EchoSimplesNaoBloqueia(t *testing.T) {
	if detectSystemPromptLeak("Rode: echo teste-hok-123") {
		t.Fatal("echo simples NAO deveria ser bloqueado")
	}
}

// 2 sinais DISTINTOS (1 fraco exclusivo + 1 narrativa exclusiva, sem overlap
// com a Camada 1): com limiar 3 nao deve bloquear (antes bloqueava com 2)
func TestThresholdCamada2_DoisSinaisDistintosPassa(t *testing.T) {
	// "in this mode by default" = fraco; "this is a simple" = narrativa;
	// nenhum dos dois esta em systemPromptLeakStrong (camada 1)
	s := "in this mode by default this is a simple request"
	if detectSystemPromptLeak(s) {
		t.Fatalf("2 sinais distintos NAO deveriam bloquear com limiar 3: %q", s)
	}
}

// 3+ sinais reais de vazamento: ainda deve bloquear
func TestThresholdCamada2_TresSinaisBloqueia(t *testing.T) {
	s := "in this mode by default this is a simple request and the user wants me to proceed"
	if !detectSystemPromptLeak(s) {
		t.Fatalf("3+ sinais deveriam bloquear, texto: %s", s)
	}
}

// Camadas 1/3/4 nao foram afetadas
func TestThresholdCamadasOutrasIntactas(t *testing.T) {
	if !detectSystemPromptLeak("i should respond in portuguese per the project instructions") {
		t.Fatal("camada 1 (sinal forte) deveria bloquear")
	}
	if !detectSystemPromptLeak("- /agents: descricao de skill") {
		t.Fatal("camada 3 (slash) deveria bloquear")
	}
	dense := strings.Join([]string{
		"against and available based brief channel checklist comparing control",
		"debug default demands described diff directly discussions exit",
		"explicit explore feedback file harness help include instructions",
	}, " ")
	if !detectSystemPromptLeak(dense) {
		t.Fatal("camada 4 (densidade) deveria bloquear")
	}
}
