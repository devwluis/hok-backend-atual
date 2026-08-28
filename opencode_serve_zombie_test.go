package main

import "testing"

// TestZombieCounter — o contador de respostas vazias por sessão (detecção da
// sessão zumbi pós-TTL): N vazias consecutivas → limiar; resposta válida zera.
func TestZombieCounter(t *testing.T) {
	sid := "ses_zumbi_test"
	openCodeServeNoteOk(sid) // garante estado limpo
	if n := openCodeServeNoteEmpty(sid); n != 1 {
		t.Fatalf("primeira vazia: esperado 1, veio %d", n)
	}
	if n := openCodeServeNoteEmpty(sid); n != 2 {
		t.Fatalf("segunda vazia: esperado 2, veio %d", n)
	}
	if n := openCodeServeNoteEmpty(sid); n != 3 {
		t.Fatalf("terceira vazia: esperado 3, veio %d", n)
	}
	openCodeServeNoteOk(sid)
	if n := openCodeServeNoteEmpty(sid); n != 1 {
		t.Fatalf("apos resposta valida: esperado reinicio em 1, veio %d", n)
	}
	openCodeServeNoteOk(sid)
}

// TestZombieIsolation — o contador é por sessão (sessões diferentes não
// interferem entre si).
func TestZombieIsolation(t *testing.T) {
	openCodeServeNoteOk("ses_a")
	openCodeServeNoteOk("ses_b")
	openCodeServeNoteEmpty("ses_a")
	openCodeServeNoteEmpty("ses_a")
	if n := openCodeServeNoteEmpty("ses_b"); n != 1 {
		t.Fatalf("ses_b deveria ter contagem independente (1), veio %d", n)
	}
	openCodeServeNoteOk("ses_a")
	openCodeServeNoteOk("ses_b")
}