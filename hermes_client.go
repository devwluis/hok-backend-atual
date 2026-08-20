package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

func isComplexTask(msg string) bool {
	keywords := []string{
		"crie uma skill", "criar skill", "nova skill",
		"crie um script", "cria um script", "faca um script",
		"pesquise na web", "busque na internet",
		"procure na web", "ultimas noticias",
		"automatize", "automatizar", "cron", "agende",
		"monitore", "monitorar", "instale", "deploy",
		"rebuild", "redeploy", "analise o log",
		"diagnostique", "por que esta falhando",
	}
	lower := strings.ToLower(msg)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// HermesModelA/B vindos de env (default: ModelA/DeepSeek, ModelB/Gemini)
var (
	hermesModelA = os.Getenv("HERMES_MODEL_A")
	hermesModelB = os.Getenv("HERMES_MODEL_B")
)

func init() {
	if hermesModelA == "" {
		hermesModelA = ModelA
	}
	if hermesModelB == "" {
		hermesModelB = ModelB
	}
}

func callHermes(prompt string) (string, error) {
	// modelo ativo global (selecionado via /models/select no frontend);
	// fallback automatico para hermesModelB quando o ativo falha.
	model := getActiveModel()
	out, err := callHermesWith(model, prompt)
	if err == nil {
		return out, nil
	}
	logModelIncompatibility("hermes", model, err)
	log.Printf("⚠️ Hermes modelA falhou (%v) — reexecutando com modelB", err)
	// fallback automatico para modelB
	return callHermesWith(hermesModelB, prompt)
}

// callHermesWith roda a imagem do hermes-gateway dockerizada com o modelo dado.
func callHermesWith(model string, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/docker", "exec", "hermes-gateway",
		"hermes", "-z", prompt, "-m", model, "--provider", "openrouter", "--yolo")
	out, err := cmd.Output()
	if err != nil {
		sqliteExec("INSERT INTO logs (event, level, source) VALUES ('hermes_invoke:minimax-m3 fail', 'WARN', 'hermes_client');")
		return "", err
	}
	sqliteExec("INSERT INTO logs (event, level, source) VALUES ('hermes_invoke:minimax-m3 ok', 'INFO', 'hermes_client');")
	return strings.TrimSpace(string(out)), nil
}

func buildHermesPrompt(msg string, req ClientRequest) string {
	var sb strings.Builder
	sb.WriteString("Você é Hokmá, assistente pessoal de desenvolvimento e DevOps.\n")
	sb.WriteString("Responda sempre em português brasileiro.\n\n")
	if len(req.History) > 0 {
		sb.WriteString("Histórico recente:\n")
		limit := len(req.History)
		if limit > 6 {
			limit = 6
		}
		for _, h := range req.History[len(req.History)-limit:] {
			sb.WriteString(h.Role + ": " + h.Content + "\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Usuário: " + msg)
	return sb.String()
}
