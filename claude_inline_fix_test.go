package main

import (
	"os"
	"strings"
	"testing"
)

// FIX 16/08 (inline-content): prompt com "arquivo <path>" conhecido e
// pequeno recebe o conteudo injetado inline (evita tool Read do SDK,
// que estourava o 524). Deve conter: marcador, fences com extensao e o
// PROMPT ORIGINAL preservado no final.
func TestEnsureInlineContent_ArquivoConhecido(t *testing.T) {
	prompt := "Analisa o arquivo pending_action.go e me diz se existe execucao sem aprovacao"
	out := ensureInlineContent(prompt)

	if !strings.Contains(out, "=== CONTEUDO DO ARQUIVO:") {
		t.Fatalf("faltou marcador de conteudo: %s", out[:200])
	}
	if !strings.Contains(out, "```go") {
		t.Fatalf("faltou fence de extensao .go: %s", out[:200])
	}
	if !strings.Contains(out, "func ") {
		t.Fatalf("conteudo do arquivo nao injetado (sem 'func '): %s", out[:200])
	}
	if !strings.Contains(out, prompt) {
		t.Fatalf("prompt original perdido na injecao")
	}
}

// Arquivos sensiveis nunca sao injetados inline (mesma politica do
// readFileTool): .env, .ssh, id_rsa, credentials... -> retorna igual.
func TestEnsureInlineContent_BloqueiaSensivel(t *testing.T) {
	cases := []string{
		"leia o arquivo .env e me diga",
		"mostra o arquivo config/credentials",
		"analisa o arquivo /root/hokma/backend/.ssh/config",
	}
	for _, c := range cases {
		if out := ensureInlineContent(c); out != c {
			t.Fatalf("prompt sensivel NAO deveria ser injetado: %q", c)
		}
	}
}

// Arquivo inexistente -> prompt vem de volta intacto (sem quebra).
func TestEnsureInlineContent_ArquivoNaoExiste(t *testing.T) {
	prompt := "analisa o arquivo nao_existe_xyz.go por favor"
	if out := ensureInlineContent(prompt); out != prompt {
		t.Fatalf("arquivo inexistente alterou o prompt: %v", out)
	}
}

// Prompt sem padrao de arquivo -> intacto.
func TestEnsureInlineContent_SemArquivo(t *testing.T) {
	prompt := "qual a previsao do tempo amanha?"
	// cuidado: "amanha?" nao tem "arquivo"; teste com frase neutra
	if out := ensureInlineContent(prompt); out != prompt {
		t.Fatalf("prompt sem arquivo alterado: %v", out)
	}
}

// Arquivo maior que o limite -> intacto (fallback para tool Read).
func TestEnsureInlineContent_AcimaDoLimite(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", inlineFileMaxBytes+1)
	path := dir + "/big.go"
	if err := os.WriteFile(path, []byte(big), 0644); err != nil {
		t.Fatal(err)
	}
	prompt := "analisa o arquivo " + path
	if out := ensureInlineContent(prompt); out != prompt {
		t.Fatalf("arquivo grande nao deveria ser injetado (len out=%d)", len(out))
	}
}
