package main

import "testing"

// TestChatNaoInterpretaMarkdownComoComando — regressão do bug 20/08:
// texto longo em markdown colado no Chat deve ser tratado como instrução em
// linguagem natural (ir para o modelo), NUNCA como comando bash/read_file
// automático.
func TestChatNaoInterpretaMarkdownComoComando(t *testing.T) {
	markdownPaste := `# Teste (ajustado) — Validar modelo unificado nos 4 motores

Contexto: Projeto Hok OS, frontend em produção /var/www/hok-os (repo GitHub
devwluis/hok-frontend-atual ou devwluis/Hok_atual2, confirmar qual está ativo
antes de editar). Componente: TerminalScreen.tsx (shell interativo PTY real,
já com persistência de sessão/histórico implementada).

## Problema
No mobile, ao tocar no campo de input do terminal, abre o teclado padrão do
Android (letras/números), sem nenhuma tecla especial de terminal. Faltam:
Ctrl, Alt, Esc, Tab, e setas (↑ ↓ ← →) — essenciais para comandos como
Ctrl+C, Ctrl+D, Ctrl+Alt+G (mencionado no próprio hint do terminal).

## Tarefa
1. Localizar o componente TerminalScreen.tsx.
2. Adicionar uma barra fixa de teclas especiais...
[bloco de codigo]
- lista item
- outro item`

	// 1) texto markdown colado NUNCA é comando de automodificação
	if isSelfModCommand(markdownPaste) {
		t.Errorf("markdown longo colado não deve ser tratado como comando bash (isSelfModCommand=true)")
	}

	// 2) texto markdown colado NUNCA força read_file
	if shouldForceReadFile(markdownPaste) {
		t.Errorf("markdown longo colado não deve forçar a tool read_file")
	}

	// 3) single-line com prosa também não
	proseSingleLine := "como compilar o backend?"
	if isSelfModCommand(proseSingleLine) {
		t.Errorf("prosa de uma linha não deve ser comando bash")
	}
	proseRode := "rode ls no backend por favor"
	if isSelfModCommand(proseRode) {
		t.Errorf("frase com 'rode' não deve ser comando bash")
	}
}

func TestSelfModCommandLiteral(t *testing.T) {
	// comandos shell LITERAIS de uma linha continuam funcionando
	// (exigem contexto de projeto: /root/hokma | backend | frontend)
	ok := []string{
		"ls -la /root/hokma/backend",
		"$ ls /root/hokma/backend",
		"cat /root/hokma/backend/main.go",
		"go build ./... no backend",
		"df -h /root/hokma",
	}
	for _, c := range ok {
		if !isSelfModCommand(c) {
			t.Errorf("isSelfModCommand(%q) = false, esperado true (comando literal)", c)
		}
	}
	gate := []string{
		"", "ls", // sem argumento/path
		"como listar arquivos no backend", // prosa
		"git status por favor",
	}
	for _, c := range gate {
		if isSelfModCommand(c) {
			t.Errorf("isSelfModCommand(%q) = true, esperado false", c)
		}
	}
}

func TestShouldForceReadFile(t *testing.T) {
	ok := []string{
		"leia o arquivo /root/hokma/backend/main.go",
		"mostre o conteudo de config.json",
	}
	for _, c := range ok {
		if !shouldForceReadFile(c) {
			t.Errorf("shouldForceReadFile(%q) = false, esperado true", c)
		}
	}
	no := []string{
		"",
		"leia o arquivo main.go", // curto e válido (força) — manter
	}
	_ = no
	// texto longo / markdown / multi-linha nunca força
	longMarkdown := "Contexto: Projeto Hok OS. ## Tarefa 1. leia o arquivo principal\nbloco com varias linhas e markdown"
	if shouldForceReadFile(longMarkdown) {
		t.Errorf("texto longo markdown não deve forçar read_file")
	}
	multiLine := "leia o arquivo x\nsegunda linha"
	if shouldForceReadFile(multiLine) {
		t.Errorf("multi-linha não deve forçar read_file")
	}
}
