package main

import (
	"testing"
	"time"
)

// TestDebounceEntryIdentity garante que um callback de timer antigo (que
// disparou mesmo após Stop()) não processa quando já existe um entry novo
// no map — senão a resposta da IA chega fora da janela de debounce e o
// entry novo é apagado por engano.
func TestDebounceEntryIdentity(t *testing.T) {
	entry := &debounceEntry{telefone: "+5511999999999"}
	entry.timer = time.AfterFunc(5*time.Second, func() {})

	old := debounceTimers
	debounceTimers = map[string]*debounceEntry{}
	defer func() { debounceTimers = old }()

	novo := &debounceEntry{telefone: "+5511888888888"}
	debounceTimers["lead-1"] = novo

	// callback do entry antigo roda: retorna imediatamente (sem db, sem
	// geracao de resposta) porque não é mais o dono do entry no map
	processDebouncedReply(nil, "lead-1", entry)

	if cur, ok := debounceTimers["lead-1"]; !ok || cur != novo {
		t.Fatalf("callback antigo removeu/substituiu o entry novo: ok=%v cur=%p novo=%p", ok, cur, novo)
	}
}

// TestDebounceReschedule verifica que scheduleAIReply reinicia a janela:
// duas chamadas rápidas deixam só um entry, pertencente ao timer mais recente.
func TestDebounceReschedule(t *testing.T) {
	old := debounceTimers
	debounceTimers = map[string]*debounceEntry{}
	defer func() { debounceTimers = old }()

	scheduleAIReply(nil, "lead-x", "tel-1")
	e1 := debounceTimers["lead-x"]
	scheduleAIReply(nil, "lead-x", "tel-2")
	e2 := debounceTimers["lead-x"]

	if len(debounceTimers) != 1 {
		t.Fatalf("esperado 1 entry no map, got %d", len(debounceTimers))
	}
	if e1 == e2 {
		t.Fatalf("esperado entry novo na segunda chamada (timer reiniciado)")
	}
}