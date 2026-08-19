package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// terminalWSUpgrader — conexões WebSocket para o terminal interativo.
var terminalWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	// Origem verificada pela autenticação por token (abaixo); sem isso,
	// browsers externos não conseguiriam abrir o terminal.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleTerminalWS — terminal interativo real via PTY + WebSocket.
// Requer autenticação: /terminal/ws?token=<HOK_TOKEN> (igual ao X-Hok-Token).
// Cada conexão spawna um shell bash real num PTY; teclas digitadas no
// frontend vão para o processo e a saída volta pela conexão.
func handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		token = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	if token == "" || !tokenMatches(token) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"unauthorized"}`))
		return
	}

	conn, err := terminalWSUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[term-ws] upgrade falhou: %v", err)
		return
	}
	defer conn.Close()

	// Shell real (bash) dentro de um PTY
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"HOK_TERM=1",
	)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Printf("[term-ws] pty.Start: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n$ ERRO ao iniciar shell: "+err.Error()+"\r\n"))
		return
	}
	defer ptmx.Close()

	go func() {
		defer conn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.TextMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// deadline de escrita generoso; processa mensagens do cliente
	conn.SetReadLimit(64 * 1024)
	for {
		conn.SetReadDeadline(time.Now().Add(24 * time.Hour))
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		// Protocolo: JSON {"type":"input","data":"..."} ou {"type":"resize","cols":N,"rows":N}
		// (compatibilidade: texto cru é tratado como input)
		raw := string(data)
		if strings.HasPrefix(strings.TrimSpace(raw), `{"type"`) {
			if strings.Contains(raw, `"resize"`) {
				var msg struct {
					Type string `json:"type"`
					Cols uint16 `json:"cols"`
					Rows uint16 `json:"rows"`
				}
				if jsonUnmarshalSafe(raw, &msg) && msg.Cols > 0 && msg.Rows > 0 {
					pty.Setsize(ptmx, &pty.Winsize{Cols: msg.Cols, Rows: msg.Rows})
				}
				continue
			}
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if jsonUnmarshalSafe(raw, &msg) && msg.Type == "input" {
				ptmx.Write([]byte(msg.Data))
				continue
			}
		}
		ptmx.Write(data)
	}

	// Termina o shell quando a conexão fecha
	if cmd.Process != nil {
		cmd.Process.Kill()
	}
}

// tokenMatches compara o token fornecido com o HOK_TOKEN do ambiente.
func tokenMatches(given string) bool {
	expected := os.Getenv("HOK_TOKEN")
	return expected != "" && given == expected
}

func jsonUnmarshalSafe(raw string, dst interface{}) bool {
	return json.Unmarshal([]byte(raw), dst) == nil
}