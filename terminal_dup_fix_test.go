package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// FIX 22/08 (bug duplicação no terminal web): o replay do scrollback no
// reattach entregava blocos repetidos — (a) race attach×broadcast no backend
// (viewer registrado antes do Snapshot: chunk ia ao vivo E no replay) e
// (b) frontend reaplicando o scrollback autoritativo sem limpar o xterm.
//
// Estes testes exercitam o backend com PTY/bash REAL e WebSocket real,
// reproduzindo o cenário do vídeo: 1 comando de status digitado 1x, queda de
// conexão e reattach DURANTE stream concorrente. Cada marcador deve aparecer
// exatamente 1x no output executado (+1x no eco do input, quando aplicável):
// nunca 3x como no vídeo, e nunca 0x (perda na janela do attach).

const (
	dupUser      = "dup-fix-test-user"
	dupUA        = "HOKUNIT_A_7KQ"
	dupUBB       = "HOKUNIT_BB_3ZP"
	dupUC        = "HOKUNIT_C_5XR"
	dupMarker    = "HOKSMOKE_STATUS_9XQ"
	dupEchoCmd   = "echo " + dupMarker
	dupRaceN     = 60
	dupRacePref  = "RACE_"
	dupReadWait  = 8 * time.Second
	dupIdleDrain = 700 * time.Millisecond
)

// dupServer sobe um WS server mínimo equivalente ao handleTerminalWS
// (upgrade + getOrCreate + attach + bombeamento de input).
func dupServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := terminalWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		created := false
		s := terminalSessions.getOrCreate(terminalUserKey(dupUser), r.URL.Query().Get("session_id"), &created, false)
		if s == nil {
			return
		}
		viewer := s.attach(conn, created)
		defer s.detach(viewer)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg struct {
				Type string `json:"type"`
				Data string `json:"data"`
			}
			if json.Unmarshal(data, &msg) == nil && msg.Type == "input" {
				s.writeInput(msg.Data)
			}
		}
	}))
	cleanup := func() {
		ts.Close()
		if s := findActiveSession(terminalUserKey(dupUser)); s != nil {
			s.close("fim do teste")
		}
	}
	return ts, cleanup
}

// dupClient é um viewer WS de teste: separa controle (JSON text) de stream
// (binário do pty) e acumula tudo para as asserções de contagem.
type dupClient struct {
	conn   *websocket.Conn
	mu     sync.Mutex
	stream strings.Builder // frames binários (stream ao vivo + fila pend)
	sb     string          // scrollback decodificado do replay
	nSb    int             // nº de mensagens de scrollback recebidas (anti-race)
	sid    string          // session_id anunciado no ctrl "session"
	ready  chan struct{}
	once   sync.Once
}

func dupDial(t *testing.T, url, sid string) *dupClient {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(url+"?session_id="+sid, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	cl := &dupClient{conn: c, ready: make(chan struct{})}
	go cl.readLoop()
	return cl
}

func (c *dupClient) readLoop() {
	for {
		c.conn.SetReadDeadline(time.Now().Add(dupReadWait))
		mt, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		c.mu.Lock()
		if mt == websocket.TextMessage {
			var ctrl map[string]interface{}
			if json.Unmarshal(data, &ctrl) == nil {
				switch ctrl["type"] {
				case "session":
					if s, ok := ctrl["session_id"].(string); ok {
						c.sid = s
					}
				case "scrollback":
					c.nSb++
					if d, ok := ctrl["data"].(string); ok {
						if raw, err := base64.StdEncoding.DecodeString(d); err == nil {
							c.sb += string(raw)
						}
					}
				case "ready":
					c.once.Do(func() { close(c.ready) })
				}
			}
		} else {
			c.stream.Write(data)
		}
		c.mu.Unlock()
	}
}

func (c *dupClient) waitReady(t *testing.T) {
	t.Helper()
	select {
	case <-c.ready:
	case <-time.After(dupReadWait):
		t.Fatal("timeout aguardando ready")
	}
}

func (c *dupClient) total() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sb + c.stream.String()
}

func (c *dupClient) close() { c.conn.Close() }

// waitOutput espera até accumulated conter n ocorrências de want.
func waitOutput(t *testing.T, get func() string, want string, n int) {
	t.Helper()
	deadline := time.Now().Add(dupReadWait)
	for time.Now().Before(deadline) {
		if strings.Count(get(), want) >= n {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timeout: %q atingiu %d ocorrências, queria >= %d", want, strings.Count(get(), want), n)
}

// Cenário do vídeo: 1 comando de status digitado 1x → reattach (queda de WS)
// durante stream concorrente → o bloco NÃO pode aparecer duplicado/triplicado
// no replay, e nada do stream concorrente pode ser perdido na janela do attach.
func TestTerminalReplayReattachSemDuplicacao(t *testing.T) {
	ts, cleanup := dupServer(t)
	defer cleanup()
	url := "ws" + strings.TrimPrefix(ts.URL, "http")

	// 1) Primeiro viewer: sessão criada. Espera o readline ficar pronto
	//    (bracketed paste ON) para não confundir o redraw do readline com
	//    duplicação; então digita o comando de status UMA única vez —
	//    exatamente o cenário do vídeo.
	cA := dupDial(t, url, "")
	cA.waitReady(t)
	s := findActiveSession(terminalUserKey(dupUser))
	if s == nil {
		t.Fatal("sessão não encontrada")
	}
	waitOutput(t, cA.total, "\x1b[?2004h", 1)
	s.writeInput(dupEchoCmd + "\n")
	waitOutput(t, cA.total, dupMarker, 2) // eco do input + resultado (aparece 1x na tela)
	cA.close()

	// 2) FASE A — capture-pane com tela ESTÁTICA: reattach após a saída do
	//    bloco. O replay deve ser a TELA ATUAL renderizada pelo tmux (texto
	//    limpo, zero ANSI do ring cru) CONTENDO o bloco (ainda visível).
	time.Sleep(600 * time.Millisecond) // tela assenta
	cB := dupDial(t, url, s.ID)
	cB.waitReady(t)
	time.Sleep(700 * time.Millisecond) // drena capture+pend

	if cB.nSb != 1 {
		t.Fatalf("esperado exatamente 1 mensagem de scrollback no reattach; chegou %d", cB.nSb)
	}
	if strings.Contains(cB.sb, "\x1b[") {
		t.Fatal("scrollback contém sequências ANSI — capture-pane não aplicado")
	}
	if !strings.Contains(cB.sb, dupMarker) {
		t.Fatal("capture-pane não contém o bloco de status visível (estado atual ausente)")
	}
	cB.close()

	// 3) FASE B — bomba concorrente com viewer anexado DURANTE o stream:
	//    valida race attach×broadcast e zero perda de linhas.
	cC := dupDial(t, url, s.ID)
	cC.waitReady(t)
	s.writeInput("for i in $(seq 1 " + itoa(dupRaceN) + "); do echo " + dupRacePref + "$i; sleep 0.008; done\n")
	waitOutput(t, cC.total, dupRacePref+itoa(dupRaceN), 1)
	time.Sleep(dupIdleDrain)
	total := cC.total()

	if cC.nSb != 1 {
		t.Fatalf("fase B: esperado exatamente 1 scrollback; chegou %d", cC.nSb)
	}
	if strings.Contains(cC.sb, "\x1b[") {
		t.Fatal("fase B: scrollback contém ANSI — capture-pane não aplicado")
	}
	for i := 1; i <= dupRaceN; i++ {
		want := dupRacePref + itoa(i)
		if !strings.Contains(total, want) {
			t.Fatalf("fase B: %q ausente do stream (perda na janela do attach)", want)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Teste determinístico da mecânica anti-race (a janela attach×broadcast é de
// microssegundos — o teste de integração acima raramente a atinge). Aqui a
// invariante é exercitada DIRETAMENTE: chunk lido durante o replay NÃO vai ao
// vivo, NÃO se perde, e é entregue EXATAMENTE 1× em ordem na liberação.
func TestBroadcastBacklogFilaOrdemUnica(t *testing.T) {
	// Conexão WS real para o viewer sintético: finishBacklog escreve a fila
	// nela — permite provar tanto o NÃO-envio ao vivo quanto a entrega única.
	srvConnCh := make(chan *websocket.Conn, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := terminalWSUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		srvConnCh <- c
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer ts.Close()
	cli, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http")+"/", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer cli.Close()
	var srv *websocket.Conn
	select {
	case srv = <-srvConnCh:
	case <-time.After(5 * time.Second):
		t.Fatal("upgrade do servidor não completou")
	}

	created := false
	s := terminalSessions.getOrCreate(terminalUserKey("dup-unit-user"), "", &created, false)
	if s == nil {
		t.Fatal("sessão não criada")
	}
	defer s.close("fim do teste")

	// Coletor contínuo em goroutine: gorilla/websocket PANICA em
	// "repeated read on failed websocket connection" após qualquer erro de
	// leitura (inclusive deadline expirada) — então NUNCA setamos deadline
	// de leitura aqui; a goroutine lê sem deadline e o teste consulta o
	// acumulado sob mutex.
	var colMu sync.Mutex
	var col strings.Builder
	go func() {
		for {
			mt, data, err := cli.ReadMessage()
			if err != nil {
				return // conn fechada no fim do teste
			}
			if mt == websocket.BinaryMessage {
				colMu.Lock()
				col.Write(data)
				colMu.Unlock()
			}
		}
	}()
	readAll := func(wait time.Duration) string {
		if wait > 0 {
			time.Sleep(wait)
		}
		colMu.Lock()
		defer colMu.Unlock()
		return col.String()
	}

	// Prova de vida do wire srv→cli antes da mecânica sob teste.
	if err := srv.WriteMessage(websocket.BinaryMessage, []byte("HELLO_WIRE")); err != nil {
		t.Fatalf("wire srv->cli indisponível: %v", err)
	}
	if got := readAll(2 * time.Second); !strings.Contains(got, "HELLO_WIRE") {
		t.Fatalf("wire básico falhou: %q", got)
	}

	// Viewer sintético em replay (backlog=true).
	v := &terminalViewer{conn: srv, backlog: true}
	s.mu.Lock()
	s.viewers[v] = struct{}{}
	s.mu.Unlock()

	// 1) Chunks durante o replay: NADA vai ao vivo (o HELLO_WIRE do setup já
	//    estava no acumulado; nenhum marcador novo pode aparecer)...
	s.broadcast([]byte(dupUA))
	s.broadcast([]byte(dupUBB))
	if got := readAll(300 * time.Millisecond); strings.Contains(got, dupUA) || strings.Contains(got, dupUBB) {
		t.Fatalf("chunk do período de backlog foi ao vivo (%q) — duplicaria com o replay", got)
	}
	// ...e a fila preserva ordem/conteúdo (sem perda nem duplicação). O bash
	// pode intercalar o próprio prompt no stream do pty: por isso marcadores
	// únicos com verificação de ordem, não igualdade exata.
	s.mu.Lock()
	fila := string(bytes.Join(v.pend, nil))
	s.mu.Unlock()
	iA, iBB := strings.Index(fila, dupUA), strings.Index(fila, dupUBB)
	if iA < 0 || iBB < 0 || iA > iBB {
		t.Fatalf("fila pend = %q; esperado conter %q antes de %q, em ordem, 1× cada",
			fila, dupUA, dupUBB)
	}
	if strings.Count(fila, dupUA) != 1 || strings.Count(fila, dupUBB) != 1 {
		t.Fatalf("fila pend duplicou marcador: %q", fila)
	}

	// 2) Liberação: entrega a fila EM ORDEM, exatamente 1×.
	s.finishBacklog(v)
	var got string
	deadline := time.Now().Add(3 * time.Second)
	for {
		got = readAll(50 * time.Millisecond)
		if strings.Contains(got, dupUA) && strings.Contains(got, dupUBB) {
			break
		}
		if time.Now().After(deadline) {
			break
		}
	}
	if strings.Count(got, dupUA) != 1 || strings.Count(got, dupUBB) != 1 ||
		strings.Index(got, dupUA) > strings.Index(got, dupUBB) {
		t.Fatalf("entrega pós-replay = %q; esperado %q e %q em ordem, 1× cada", got, dupUA, dupUBB)
	}
	if v.backlog {
		t.Fatal("finishBacklog não desarmou o backlog")
	}

	// 3) Dupla liberação é idempotente: NADA novo é entregue (o acumulado é
	//    append-only; mesmo comprimento antes/depois ⇒ zero frames novos).
	beforeLen := len(readAll(0))
	s.finishBacklog(v)
	if afterLen := len(readAll(500 * time.Millisecond)); afterLen != beforeLen {
		t.Fatalf("dupla liberação entregou bytes novos (%d → %d)", beforeLen, afterLen)
	}

	// 4) Chunk pós-replay NÃO é enfileirado: sai ao vivo direto.
	s.mu.Lock()
	delete(s.viewers, v)
	s.mu.Unlock()
	s.broadcast([]byte(dupUC))
	s.mu.Lock()
	n := len(v.pend)
	s.mu.Unlock()
	if n != 0 {
		t.Fatalf("chunk pós-replay foi enfileirado indevidamente (%d itens)", n)
	}
}
