package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// ─────────────────────────────────────────────────────────────────────────
// Terminal persistente (FIX 20/08): sessão pty DESACOPLADA do WebSocket.
//
// Cada sessão roda um bash num PTY do processo do backend, independente de
// qualquer conexão WS. O WebSocket é apenas um "viewer" da sessão: pode cair
// (fechar navegador, refresh, suspensão do Android, rede) que o processo pty
// CONTINUA vivo — comando, diretório e processos filhos (opencode, claude,
// tail -f, vim) sobrevivem. A sessão só é destruída por:
//   - exit/Ctrl+D do usuário (bash encerra → EOF no pty);
//   - timeout de inatividade longo (terminalSessionTTL, configurável);
//   - restart do serviço hokma (sessões vivem em memória).
//
// Múltiplos viewers (abas/dispositivos com o mesmo token) espelham a MESMA
// sessão (estilo tmux attach). O session_id é a chave de reattach; o
// frontend salva em localStorage e envia no reconnect.
// ─────────────────────────────────────────────────────────────────────────

const (
	// TTL de inatividade (sem NENHUM viewer conectado) antes de destruir a sessão.
	terminalSessionTTL = 24 * time.Hour
	// Intervalo do sweeper que destrói sessões expiradas.
	terminalSessionSweep = 30 * time.Minute
	// Limites do ring buffer de scrollback (por sessão).
	terminalRingMaxChunks = 400
	terminalRingMaxBytes  = 512 * 1024 // 512KB
)

// terminalUserKey deriva o dono da sessão a partir do token — tokens
// diferentes → sessões independentes (pronto para multi-tenant).
func terminalUserKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:8])
}

// newTerminalSessionID gera um id aleatório de sessão (chave de reattach).
func newTerminalSessionID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ── Ring buffer de scrollback ────────────────────────────────────────────

// terminalRingBuffer guarda os últimos ~512KB do output do pty, para replay
// ao reconectar um viewer. Ring simples: append + evicta do início.
type terminalRingBuffer struct {
	mu     sync.Mutex
	chunks [][]byte
	total  int
}

func (b *terminalRingBuffer) Append(p []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.chunks = append(b.chunks, append([]byte(nil), p...))
	b.total += len(p)
	for b.total > terminalRingMaxBytes && len(b.chunks) > 1 {
		b.total -= len(b.chunks[0])
		b.chunks = b.chunks[1:]
	}
	for len(b.chunks) > terminalRingMaxChunks {
		b.total -= len(b.chunks[0])
		b.chunks = b.chunks[1:]
	}
}

// Snapshot devolve o scrollback completo (mais antigo → mais novo).
func (b *terminalRingBuffer) Snapshot() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, 0, b.total)
	for _, c := range b.chunks {
		out = append(out, c...)
	}
	return out
}

// ── Sessão ───────────────────────────────────────────────────────────────

// terminalViewer é uma conexão WS conectada a uma sessão (viewer espelhado).
type terminalViewer struct {
	conn *websocket.Conn
	wsMu sync.Mutex // gorilla/websocket exige um único writer por conexão
}

// TerminalSession é o processo pty persistente + seus viewers + scrollback.
type TerminalSession struct {
	ID       string
	UserKey  string
	ptmx     *os.File
	cmd      *exec.Cmd
	bashPgrp int
	ptyMu    sync.Mutex // serializa escritas no pty (input + respostas seguradas)
	buf      *terminalRingBuffer
	mu       sync.Mutex
	viewers  map[*terminalViewer]struct{}
	lastUsed time.Time
	closed   bool
}

func newTerminalSession(userKey string) *TerminalSession {
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
		log.Printf("[term-session] pty.Start: %v", err)
		return nil
	}
	s := &TerminalSession{
		ID:       newTerminalSessionID(),
		UserKey:  userKey,
		ptmx:     ptmx,
		cmd:      cmd,
		buf:      &terminalRingBuffer{},
		viewers:  map[*terminalViewer]struct{}{},
		lastUsed: time.Now(),
	}
	if pgrp, perr := foregroundPgrp(ptmx.Fd()); perr == nil {
		s.bashPgrp = pgrp
	} else if cmd.Process != nil {
		s.bashPgrp = cmd.Process.Pid
	}
	go s.readerLoop()
	// Vigia o PROCESSO bash: se ele sair (exit/Ctrl+D), a sessão fecha mesmo
	// que processos em background ainda segurem o slave fd (senão o EOF do
	// pty não dispara e a sessão ficaria "viva" sem shell).
	go func() {
		if s.cmd != nil && s.cmd.Process != nil {
			s.cmd.Wait()
		}
		s.close("bash encerrou (processo)")
	}()
	log.Printf("[term-session] criada %s user=%s", s.ID, userKey)
	return s
}

// readerLoop lê o output do pty (bash e filhos), acumula no scrollback e
// transmite aos viewers. EOF = bash encerrou → fecha a sessão.
func (s *TerminalSession) readerLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.buf.Append(chunk)
			s.broadcast(chunk)
		}
		if err != nil {
			s.close("bash encerrou")
			return
		}
	}
}

func (s *TerminalSession) broadcast(chunk []byte) {
	s.mu.Lock()
	viewers := make([]*terminalViewer, 0, len(s.viewers))
	for v := range s.viewers {
		viewers = append(viewers, v)
	}
	s.mu.Unlock()
	for _, v := range viewers {
		v.wsMu.Lock()
		// FIX 21/08 (closeCode=1002 em loop): output do PTY é BINÁRIO — pode
		// conter escape sequences ANSI/OSC, cores em bytes crus ou bytes que
		// não formam UTF-8 válido (TUI do opencode, cat de arquivo binário).
		// Frames TEXT exigem UTF-8 puro; se violar, o browser fecha com 1002.
		// Usar BinaryMessage (opcode 0x2) permite qualquer byte; o frontend
		// decodifica com TextDecoder (bytes inválidos viram U+FFFD).
		v.conn.WriteMessage(websocket.BinaryMessage, chunk)
		v.wsMu.Unlock()
	}
}

// attach adiciona um viewer e envia a sequência de controle: session_id,
// scrollback (base64) e "ready". A sequência é serializada (wsMu) para não
// intercalar com o stream ao vivo do reader.
func (s *TerminalSession) attach(conn *websocket.Conn, created bool) *terminalViewer {
	v := &terminalViewer{conn: conn}
	s.mu.Lock()
	s.viewers[v] = struct{}{}
	s.lastUsed = time.Now()
	s.mu.Unlock()

	v.wsMu.Lock()
	defer v.wsMu.Unlock()
	ctrl, _ := json.Marshal(map[string]interface{}{
		"type": "session", "session_id": s.ID, "created": created,
	})
	v.conn.WriteMessage(websocket.TextMessage, ctrl)
	if sb := s.buf.Snapshot(); len(sb) > 0 {
		sbMsg, _ := json.Marshal(map[string]string{
			"type": "scrollback", "data": base64.StdEncoding.EncodeToString(sb),
		})
		v.conn.WriteMessage(websocket.TextMessage, sbMsg)
	}
	v.conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready"}`))
	return v
}

func (s *TerminalSession) detach(v *terminalViewer) {
	s.mu.Lock()
	delete(s.viewers, v)
	s.lastUsed = time.Now()
	s.mu.Unlock()
}

// writeInput aplica o guard de CPR/DSR/DA (fix anterior) e escreve no pty.
func (s *TerminalSession) writeInput(data string) {
	writeTerminalInput(s.ptmx, &s.ptyMu, s.bashPgrp, data)
}

func (s *TerminalSession) resize(cols, rows uint16) {
	if cols > 0 && rows > 0 {
		pty.Setsize(s.ptmx, &pty.Winsize{Cols: cols, Rows: rows})
	}
}

func (s *TerminalSession) close(reason string) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	viewers := make([]*terminalViewer, 0, len(s.viewers))
	for v := range s.viewers {
		viewers = append(viewers, v)
	}
	s.viewers = map[*terminalViewer]struct{}{}
	s.mu.Unlock()

	terminalSessions.remove(s)

	s.ptmx.Close()
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	for _, v := range viewers {
		v.wsMu.Lock()
		v.conn.Close()
		v.wsMu.Unlock()
	}
	log.Printf("[term-session] fechada %s (%s)", s.ID, reason)
}

// ── Registro ─────────────────────────────────────────────────────────────

var terminalSessions = &terminalSessionRegistry{
	sessions: map[string]*TerminalSession{},
	byUser:   map[string]string{},
}

type terminalSessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*TerminalSession // by sessionID
	byUser   map[string]string           // userKey -> sessionID
}

// getOrCreate retorna a sessão existente (pelo session_id informado OU pela
// sessão atual do usuário) ou cria uma nova. created=true se criou.
//
// FASE 6 (múltiplas sessões simultâneas): forceNew=true ignora o session_id e
// o byUser e cria SEMPRE uma sessão nova — é o que o frontend usa ao abrir
// uma aba nova (Sessão 2, 3...). O byUser continua como fallback (quem
// conecta sem session_id reattach à sessão mais recente do usuário), e o
// registro mantém N sessões vivas por usuário (cada uma com seu pty).
func (r *terminalSessionRegistry) getOrCreate(userKey, sessionID string, created *bool, forceNew bool) *TerminalSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !forceNew {
		if sessionID != "" {
			if s, ok := r.sessions[sessionID]; ok {
				s.mu.Lock()
				s.lastUsed = time.Now()
				s.mu.Unlock()
				*created = false
				return s
			}
		}
		if id, ok := r.byUser[userKey]; ok {
			if s, ok := r.sessions[id]; ok {
				s.mu.Lock()
				s.lastUsed = time.Now()
				s.mu.Unlock()
				*created = false
				return s
			}
		}
	}
	s := newTerminalSession(userKey)
	if s == nil {
		return nil
	}
	r.sessions[s.ID] = s
	r.byUser[userKey] = s.ID
	*created = true
	return s
}

func (r *terminalSessionRegistry) remove(s *TerminalSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, s.ID)
	if r.byUser[s.UserKey] == s.ID {
		delete(r.byUser, s.UserKey)
	}
}

func (r *terminalSessionRegistry) sweepExpired() {
	cutoff := time.Now().Add(-terminalSessionTTL)
	r.mu.Lock()
	var toClose []*TerminalSession
	for _, s := range r.sessions {
		s.mu.Lock()
		idle := s.lastUsed.Before(cutoff)
		s.mu.Unlock()
		if idle {
			toClose = append(toClose, s)
		}
	}
	r.mu.Unlock()
	for _, s := range toClose {
		go s.close("timeout de inatividade")
	}
}

func runTerminalSessionSweeper() {
	for {
		time.Sleep(terminalSessionSweep)
		terminalSessions.sweepExpired()
	}
}

// findActiveSession busca uma sessão PTY ATIVA existente do usuário (bash
// vivo, sessão não fechada) para reuso — ex.: execução de comandos via chat.
// NÃO cria sessão nova; retorna nil se nenhuma estiver viva.
func findActiveSession(userKey string) *TerminalSession {
	terminalSessions.mu.Lock()
	defer terminalSessions.mu.Unlock()

	for _, s := range terminalSessions.sessions {
		if s.UserKey == userKey && !s.closed && s.bashPgrp > 0 {
			if s.cmd != nil && s.cmd.Process != nil && s.cmd.ProcessState == nil {
				return s
			}
		}
	}
	return nil
}
