package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ────────────────────────────────────────────────────────────
// /edit command — permite ao usuário pedir edições de código
// diretamente pelo chat do app, sem abrir o Termux.
//
// Sintaxe suportada:
//   /edit <tarefa> --file <arquivo>
//   /edit <tarefa> --files <arq1,arq2>
//   /code <tarefa> --file <arquivo>      (alias)
//   /patch <tarefa> --file <arquivo>     (alias)
//
// Exemplos:
//   /edit adiciona log de erro em utils.go --file backend/utils.go
//   /edit refatora o handler de /health --files backend/routes.go,backend/types.go
// ────────────────────────────────────────────────────────────

type EditCommand struct {
	Task  string
	Files []string
	Valid bool
	Error string
}

// parseEditCommand extrai tarefa e arquivos da mensagem do usuário
func parseEditCommand(msg string) EditCommand {
	msg = strings.TrimSpace(msg)

	// Remove prefixo do comando
	prefixes := []string{"/edit ", "/code ", "/patch "}
	rawBody := ""
	for _, p := range prefixes {
		if strings.HasPrefix(strings.ToLower(msg), p) {
			rawBody = msg[len(p):]
			break
		}
	}

	if rawBody == "" {
		return EditCommand{Valid: false, Error: "comando não reconhecido"}
	}

	// Separa tarefa dos flags
	task := rawBody
	var files []string

	// Suporte a --files arq1,arq2
	if idx := strings.Index(strings.ToLower(rawBody), " --files "); idx != -1 {
		task = strings.TrimSpace(rawBody[:idx])
		filesPart := strings.TrimSpace(rawBody[idx+9:])
		for _, f := range strings.Split(filesPart, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				files = append(files, f)
			}
		}
	} else if idx := strings.Index(strings.ToLower(rawBody), " --file "); idx != -1 {
		// Suporte a --file arquivo
		task = strings.TrimSpace(rawBody[:idx])
		filePart := strings.TrimSpace(rawBody[idx+8:])
		if filePart != "" {
			files = append(files, filePart)
		}
	}

	if task == "" {
		return EditCommand{Valid: false, Error: "tarefa não especificada"}
	}
	if len(files) == 0 {
		return EditCommand{Valid: false, Error: "especifique o arquivo com --file <arquivo> ou --files <arq1,arq2>"}
	}

	return EditCommand{Task: task, Files: files, Valid: true}
}

// isEditCommand verifica se a mensagem é um /edit command
func isEditCommand(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	return strings.HasPrefix(lower, "/edit ") ||
		strings.HasPrefix(lower, "/code ") ||
		strings.HasPrefix(lower, "/patch ") ||
		lower == "/edit" || lower == "/code" || lower == "/patch"
}

// editCommandHelp retorna mensagem de ajuda do comando
func editCommandHelp() string {
	return `🛠️ **Comando /edit**

Permite editar o código do HOK OS diretamente pelo chat.

**Sintaxe:**
` + "`" + `/edit <tarefa> --file <arquivo>` + "`" + `
` + "`" + `/edit <tarefa> --files <arq1,arq2>` + "`" + `

**Exemplos:**
` + "`" + `/edit adiciona tratamento de erro em utils.go --file backend/utils.go` + "`" + `
` + "`" + `/edit melhora o handler de /health e adiciona uptime --file backend/routes.go` + "`" + `
` + "`" + `/edit refatora logs --files backend/utils.go,backend/routes.go` + "`" + `

**Aliases:** /code, /patch

**Notas:**
• O path do arquivo é relativo a ~/ecossistema/
• O agent-loop faz backup automático antes de editar
• Se o build falhar, rollback automático é executado
• Progresso em tempo real no chat`
}

// handleEditCommand processa o /edit command via SSE (streaming)
func handleEditCommand(w http.ResponseWriter, r *http.Request, userMsg string) {
	// Configura SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, canFlush := w.(http.Flusher)

	sendSSE := func(data string) {
		fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}
	}

	sendJSON := func(v interface{}) {
		b, _ := json.Marshal(v)
		sendSSE(string(b))
	}

	// Se só digitou /edit sem args, mostra ajuda
	lower := strings.ToLower(strings.TrimSpace(userMsg))
	if lower == "/edit" || lower == "/code" || lower == "/patch" {
		sendJSON(map[string]interface{}{
			"type":    "message",
			"content": editCommandHelp(),
		})
		return
	}

	// Parse do comando
	cmd := parseEditCommand(userMsg)
	if !cmd.Valid {
		sendJSON(map[string]interface{}{
			"type":    "error",
			"content": fmt.Sprintf("❌ Erro no comando: %s\n\nDigite /edit para ver a sintaxe.", cmd.Error),
		})
		return
	}

	// Mostra início
	filesStr := strings.Join(cmd.Files, ", ")
	sendJSON(map[string]interface{}{
		"type":    "progress",
		"content": fmt.Sprintf("🚀 Iniciando agent-loop...\n\n📋 **Tarefa:** %s\n📁 **Arquivo(s):** %s\n\n⏳ Aguarde...", cmd.Task, filesStr),
	})

	// Monta payload e registra ação pendente (gate de confirmação)
	argsPayload := map[string]interface{}{
		"task":     cmd.Task,
		"files":    cmd.Files,
		"max_iter": 3,
	}
	argsBytes, _ := json.Marshal(argsPayload)
	desc := fmt.Sprintf(
		"Vou editar %s via agent-loop (tarefa: %s). Isso recompila e REINICIA o backend HOK automaticamente se o build passar.",
		filesStr, cmd.Task,
	)
	setPendingAction("agent_loop_edit", string(argsBytes), desc)
	sendJSON(map[string]interface{}{
		"type":    "message",
		"content": fmt.Sprintf("⚠️ **Confirmação necessária**\n\n%s\n\nResponda **sim** para confirmar ou **não** para cancelar.", desc),
	})
}

// execAgentLoopEdit executa a chamada real ao agent-loop (:8082).
// Só é chamada depois que o usuário aprova via resolvePendingAction.
func execAgentLoopEdit(argsJSON string) string {
	client := &http.Client{Timeout: 90 * time.Second}
	req, err := http.NewRequest("POST", "http://127.0.0.1:8082/agent-loop", strings.NewReader(argsJSON))
	if err != nil {
		return fmt.Sprintf("❌ Erro ao criar request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hok-Token", "hok-api-2026")
	req.Header.Set("X-Internal-Call", "1")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Sprintf("❌ Falha ao chamar agent-loop: %v\n\nVerifique se o backend está rodando.", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Sprintf("❌ Erro ao ler resposta: %v", err)
	}
	var agentResp map[string]interface{}
	if err := json.Unmarshal(body, &agentResp); err != nil {
		return fmt.Sprintf("📄 Resposta do agent:\n\n%s", string(body))
	}
	var sb strings.Builder
	if success, ok := agentResp["success"].(bool); ok {
		if success {
			sb.WriteString("✅ Edição concluída com sucesso!\n\n")
		} else {
			sb.WriteString("❌ Edição falhou\n\n")
		}
	}
	if msg, ok := agentResp["message"].(string); ok && msg != "" {
		sb.WriteString(fmt.Sprintf("💬 %s\n\n", msg))
	}
	if iters, ok := agentResp["iterations"]; ok {
		sb.WriteString(fmt.Sprintf("🔄 Iterações: %v\n", iters))
	}
	if rebuilt, ok := agentResp["rebuilt"].(bool); ok && rebuilt {
		sb.WriteString("🔨 Backend recompilado e reiniciado\n")
	}
	if rolledBack, ok := agentResp["rolled_back"].(bool); ok && rolledBack {
		sb.WriteString("⏪ Rollback executado (build falhou)\n")
	}
	out := sb.String()
	if out == "" {
		out = fmt.Sprintf("📄 Resultado:\n\n%s", string(body))
	}
	return out
}
