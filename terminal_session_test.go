package main

import (
	"bytes"
	"strings"
	"testing"
)

// Testes do terminal persistente (terminal_session.go): ring buffer de
// scrollback e registro de sessões.

func TestTerminalRingBuffer_OrdemEManutenção(t *testing.T) {
	b := &terminalRingBuffer{}
	for i := 0; i < 10; i++ {
		b.Append([]byte(strings.Repeat("a", 100)))
	}
	snap := b.Snapshot()
	if len(snap) != 1000 {
		t.Fatalf("esperado 1000 bytes, tem %d", len(snap))
	}
	if !bytes.Equal(snap[:100], bytes.Repeat([]byte("a"), 100)) {
		t.Fatal("ordem do scrollback errada (mais antigo primeiro)")
	}
}

func TestTerminalRingBuffer_CapMaxBytes(t *testing.T) {
	b := &terminalRingBuffer{}
	for i := 0; i < 20000; i++ {
		b.Append(bytes.Repeat([]byte("x"), 100))
	}
	snap := b.Snapshot()
	if len(snap) > terminalRingMaxBytes {
		t.Fatalf("scrollback excedeu limite: %d > %d", len(snap), terminalRingMaxBytes)
	}
	if len(snap) == 0 {
		t.Fatal("scrollback não deveria ficar vazio")
	}
}

func TestTerminalRingBuffer_CapMaxChunks(t *testing.T) {
	b := &terminalRingBuffer{}
	for i := 0; i < terminalRingMaxChunks+500; i++ {
		b.Append([]byte{byte(i)})
	}
	snap := b.Snapshot()
	if len(snap) > terminalRingMaxChunks {
		t.Fatalf("chunks excederam limite: %d", len(snap))
	}
}

func TestTerminalSessionRegistry_ReattachMesmaSessao(t *testing.T) {
	user := "user:" + terminalUserKey("abc")
	created := false
	s1 := terminalSessions.getOrCreate(user, "", &created, false)
	if s1 == nil {
		t.Fatal("sessão não criada")
	}
	if !created {
		t.Fatal("esperava created=true na primeira")
	}

	// reattach com o session_id conhecido → MESMA sessão
	created2 := true
	s2 := terminalSessions.getOrCreate(user, s1.ID, &created2, false)
	if s2 != s1 {
		t.Fatal("reattach deveria retornar a MESMA sessão")
	}
	if created2 {
		t.Fatal("reattach não deveria criar sessão nova")
	}

	// sessão fechada (exit) → próxima conexão cria sessão nova
	s1.close("teste")
	created3 := false
	s3 := terminalSessions.getOrCreate(user, s1.ID, &created3, false)
	if s3 == nil || s3 == s1 {
		t.Fatal("esperava sessão nova após fechar a antiga")
	}
	if !created3 {
		t.Fatal("esperava created=true após sessão anterior fechada")
	}
	s3.close("teste")
}
