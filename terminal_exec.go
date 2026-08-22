package main

import (
	"time"
)

// ExecResult resultado da execucao de um comando no terminal PTY.
type ExecResult struct {
	Reply   string
	Partial bool
}

// executeTerminalCommand escreve o comando no PTY ativo com marcador de fim
// e retorna true se a operacao foi iniciada. O output e tratado pelo chamador.
func executeTerminalCommand(msg string, userKey string) (*ExecResult, bool) {
	// Buscar sessao PTY ativa do usuario
	s := findActiveSession(userKey)
	if s == nil {
		return &ExecResult{Reply: "Nenhuma sessao PTY ativa."}, true
	}

	// Escrever comando no PTY com marcador de conclusao
	delimiter := "\n___HOK_CMD_DONE___\n"
	s.writeInput(msg + delimiter)

	// Pequena pausa para o bash iniciar
	time.Sleep(200 * time.Millisecond)

	// Capturar output ate o marcador (versao simplificada):
	// como nao temos acesso facil ao buffer interno neste contexto,
	// apenas sinalizamos sucesso e deixo o output ser capturado
	// pelo mecanismo do broadcast do PTY (j ja existe em readerLoop).
	// O chamador pode ler do output do terminal web.

	return &ExecResult{Reply: "Comando enviado ao terminal: " + msg, Partial: false}, true
}

// findActiveSession movida para terminal_session.go (implementação canônica).