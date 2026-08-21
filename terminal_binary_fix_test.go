package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Verificação temporária do FIX 21/08 (close 1002): o stream do PTY deve
// trafegar como frame BINÁRIO (opcode 0x2), mesmo com bytes não-UTF-8.
func TestWSFix_BroadcastBinaryComBytesInvalidos(t *testing.T) {
	sessCh := make(chan *TerminalSession, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := terminalWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close()
		created := false
		s := terminalSessions.getOrCreate("wsfix:"+terminalUserKey("wsfix-token"), "", &created, false)
		if s == nil {
			return
		}
		v := s.attach(c, created)
		defer s.detach(v)
		sessCh <- s
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[4:] + "/"
	clientWS, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer clientWS.Close()

	var s *TerminalSession
	select {
	case s = <-sessCh:
	case <-time.After(3 * time.Second):
		t.Fatal("sessão não anexada a tempo")
	}
	defer s.close("fim do teste")

	// bytes inválidos de UTF-8 (0xFF, 0xFE) + escape OSC, como uma TUI real
	chunk := []byte("\x1b]11;rgb:6e6e/e7e7/b7b7\x07olá \xff\xfe world\n")
	s.broadcast(chunk)

	clientWS.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		mt, msg, err := clientWS.ReadMessage()
		if err != nil {
			t.Fatalf("leitura do ws: %v (erro 1002 = fix falhou)", err)
		}
		if mt == websocket.TextMessage {
			// frames de controle (session/ready) — só o stream importa
			continue
		}
		if mt == websocket.BinaryMessage && string(msg) == string(chunk) {
			t.Logf("stream do pty chegou como frame BINÁRIO íntegro (%d bytes)", len(msg))
			return
		}
	}
}
