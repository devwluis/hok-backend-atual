package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

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

// FIX 20/08 (quedas de conexão do terminal): heartbeat. O servidor envia
// PING a cada terminalPingInterval; o browser responde PONG sozinho. Sem
// pong em terminalPongWait, a conexão é considerada morta e encerrada.
const (
	terminalPingInterval = 25 * time.Second
	terminalPongWait     = 90 * time.Second
	terminalWriteWait    = 10 * time.Second
	// Janela para detectar o exit de uma TUI após receber resposta de
	// terminal: respostas puras são seguradas por este tempo e descartadas
	// se o foreground voltar a ser o bash (evita órfão pós-TUI).
	terminalResponseHold = 100 * time.Millisecond
)

// FIX 20/08 (lixo no prompt após encerrar TUI filho, ex: opencode):
// TUIs enviam ESC[6n (Cursor Position Report) ao renderizar. O xterm.js
// responde automaticamente com ESC[<linha>;<col>R (e similares: DSR/DA),
// e essa resposta chega ao servidor como INPUT. Se o processo que fez a
// consulta (opencode) encerrou antes de consumir a resposta, os bytes
// ficam órfãos no buffer de input do pty e o bash os lê como se fossem
// digitação do usuário ("35: command not found", etc.).
//
// Correção: quando o bash é o processo em FOREGROUND do pty (tcgetpgrp —
// ou seja, NENHUMA TUI está ativa para consumir respostas), qualquer
// resposta de terminal emulador que chegue como input é órfã por definição
// e é descartada antes de ser escrita no pty. Com uma TUI em foreground as
// respostas passam normalmente (a TUI as consome). Isso equivale a
// "descartar bytes de controle residuais que não formam um comando válido
// antes do próximo prompt" — cobre CPR, DSR e DA.
var terminalResponseRe = regexp.MustCompile(
	`\x1b\[[0-9;?]*R` + // CPR: ESC[<r>;<c>R (e privada ?)
		`|\x1b\[[0-9;?]*n` + // DSR: ESC[<n>n / ESC[?<n>n
		`|\x1b\[[<>=?]*[0-9;]*c` + // DA: ESC[?<...>c / ESC[><...>c / ESC[<...>c
		`|\x1b\[[?0-9;]*\$y` + // DECRQM: ESC[?<n>;<v>$y
		`|\x1b\[Z`, // DECID (obsoleto)
)

// foregroundPgrp devolve o grupo de processos em foreground do pty.
func foregroundPgrp(fd uintptr) (int, error) {
	var pgrp int
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, syscall.TIOCGPGRP, uintptr(unsafe.Pointer(&pgrp)))
	if errno != 0 {
		return 0, errno
	}
	return pgrp, nil
}

// stripTerminalResponses remove respostas de terminal emulador (CPR/DSR/DA)
// de um bloco de input. Chamada apenas quando o bash é o foreground do pty.
func stripTerminalResponses(input string) string {
	return terminalResponseRe.ReplaceAllString(input, "")
}

// writeTerminalInput escreve input no pty. Previne que respostas de terminal
// emulador (CPR/DSR/DA) órfãs sejam lidas pelo bash como comandos:
//
//   - A) bash em foreground: resposta de terminal é órfã por definição
//     (nenhuma TUI viva para consumi-la) → descartada antes de escrever.
//   - B) TUI em foreground: resposta é segurada por terminalResponseHold e o
//     foreground é re-checado. Se a TUI sair no intervalo (janela TOCTOU do
//     exit), a resposta é descartada sem nunca ser escrita — evita o órfão
//     que o readline do bash consumiria. Se a TUI continua viva, escreve
//     normalmente (a TUI lê).
func writeTerminalInput(ptmx *os.File, ptyMu *sync.Mutex, bashPgrp int, data string) {
	if bashPgrp > 0 {
		if pgrp, err := foregroundPgrp(ptmx.Fd()); err == nil {
			if pgrp == bashPgrp {
				cleaned := stripTerminalResponses(data)
				if cleaned == "" {
					return
				}
				ptyMu.Lock()
				ptmx.Write([]byte(cleaned))
				ptyMu.Unlock()
				return
			}
			// TUI em foreground: segura respostas puras brevemente e re-checa.
			if isPureTerminalResponse(data) {
				go func(d string) {
					time.Sleep(terminalResponseHold)
					if pgrp2, err := foregroundPgrp(ptmx.Fd()); err == nil && pgrp2 != bashPgrp {
						ptyMu.Lock()
						ptmx.Write([]byte(d))
						ptyMu.Unlock()
					}
				}(data)
				return
			}
		}
	}
	ptyMu.Lock()
	ptmx.Write([]byte(data))
	ptyMu.Unlock()
}

// isPureTerminalResponse: o chunk é inteiramente uma resposta de terminal
// emulador (CPR/DSR/DA/etc.) — candidata a ficar órfã no exit da TUI.
func isPureTerminalResponse(data string) bool {
	return data != "" && stripTerminalResponses(data) == ""
}

// handleTerminalWS — terminal interativo real via PTY persistente + WebSocket.
// Requer autenticação: /terminal/ws?token=<HOK_TOKEN> (igual ao X-Hok-Token).
//
// FIX 20/08 (terminal persistente): a conexão WS é apenas um VIEWER da sessão
// pty (ver terminal_session.go). O pty/bash vive no processo do backend e
// sobrevive à queda do WS (fechar navegador, refresh, suspensão do Android,
// rede). O parâmetro opcional session_id faz reattach na MESMA sessão; se a
// sessão não existir (expirada/morta), uma nova é criada. O frontend recebe
// o session_id e o scrollback via mensagens de controle no próprio WS.
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

	userKey := terminalUserKey(token)
	sessionID := r.URL.Query().Get("session_id")

	// Busca ou cria a sessão persistente (NÃO é destruída quando o WS cai).
	created := false
	s := terminalSessions.getOrCreate(userKey, sessionID, &created)
	if s == nil {
		log.Printf("[term-ws] falha ao criar sessão pty (user=%s)", userKey)
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"session_error","error":"pty_start"}`))
		return
	}

	viewer := s.attach(conn, created)
	// Diagnóstico de queda (aberto): registra cada viewer/reattach pra
	// distinguir reconexão normal (created=false → reattach na MESMA sessão)
	// de sessão nova (created=true → perda de estado) no log de queda.
	procAlive := s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil
	log.Printf("[term-ws] viewer conectado user=%s session=%s created=%v bashPgrp=%d procAlive=%v",
		userKey, s.ID, created, s.bashPgrp, procAlive)

	// Heartbeat do viewer (FIX 20/08): o browser responde PING com PONG
	// automaticamente; pong renova o read deadline, e viewers mortos são
	// encerrados em ~2 ciclos (a SESSÃO, porém, permanece viva).
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(terminalPongWait))
	})
	connClosed := make(chan struct{})
	defer close(connClosed)
	go func() {
		ticker := time.NewTicker(terminalPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				viewer.wsMu.Lock()
				err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(terminalWriteWait))
				viewer.wsMu.Unlock()
				if err != nil {
					return
				}
			case <-connClosed:
				return
			}
		}
	}()

	// Processa mensagens do viewer (input/resize vão para a sessão).
	conn.SetReadLimit(64 * 1024)
	var closeCode int
	var closeReason string
	for {
		conn.SetReadDeadline(time.Now().Add(terminalPongWait))
		_, data, err := conn.ReadMessage()
		if err != nil {
			if ce, ok := err.(*websocket.CloseError); ok {
				closeCode = ce.Code
				closeReason = ce.Text
			} else {
				closeCode = -1
				closeReason = err.Error()
			}
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
				if jsonUnmarshalSafe(raw, &msg) {
					s.resize(msg.Cols, msg.Rows)
				}
				continue
			}
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if jsonUnmarshalSafe(raw, &msg) && msg.Type == "input" {
				s.writeInput(msg.Data)
				continue
			}
		}
		s.writeInput(raw)
	}

	// WS caiu: apenas desanexa o viewer — o processo pty da sessão PERMANECE
	// vivo (comandos em andamento, diretório e scrollback preservados).
	s.detach(viewer)
	procAlive = s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil
	log.Printf("[term-ws] desconexao user=%s session=%s reattach=%v closeCode=%d reason=%q bashPgrp=%d procAlive=%v",
		userKey, s.ID, !created, closeCode, closeReason, s.bashPgrp, procAlive)
}

// tokenMatches compara o token fornecido com o HOK_TOKEN do ambiente.
func tokenMatches(given string) bool {
	expected := os.Getenv("HOK_TOKEN")
	return expected != "" && given == expected
}

func jsonUnmarshalSafe(raw string, dst interface{}) bool {
	return json.Unmarshal([]byte(raw), dst) == nil
}