#!/bin/bash
set -e
cd ~/hokma/backend

echo "📦 Fazendo backup dos 3 arquivos..."
cp chat_agent_routes.go chat_agent_routes.go.bak_$(date +%Y%m%d_%H%M%S)
cp agent_loop_groq.go agent_loop_groq.go.bak_$(date +%Y%m%d_%H%M%S)
cp routes.go routes.go.bak_$(date +%Y%m%d_%H%M%S)

echo "🔧 Aplicando patches..."

python3 << 'PYEOF'
import re

# ─────────────────────────────────────────────
# 1. chat_agent_routes.go
# ─────────────────────────────────────────────
path = "chat_agent_routes.go"
code = open(path).read()

anchor = "\t// Chama /agent-loop internamente"
idx = code.find(anchor)
if idx == -1:
    print("⚠️  ANCHOR NÃO ENCONTRADO em chat_agent_routes.go — abortando esse arquivo")
else:
    new_tail = '''\t// Monta payload e registra ação pendente (gate de confirmação)
\targsPayload := map[string]interface{}{
\t\t"task":     cmd.Task,
\t\t"files":    cmd.Files,
\t\t"max_iter": 3,
\t}
\targsBytes, _ := json.Marshal(argsPayload)
\tdesc := fmt.Sprintf(
\t\t"Vou editar %s via agent-loop (tarefa: %s). Isso recompila e REINICIA o backend HOK automaticamente se o build passar.",
\t\tfilesStr, cmd.Task,
\t)
\tsetPendingAction("agent_loop_edit", string(argsBytes), desc)
\tsendJSON(map[string]interface{}{
\t\t"type":    "message",
\t\t"content": fmt.Sprintf("⚠️ **Confirmação necessária**\\n\\n%s\\n\\nResponda **sim** para confirmar ou **não** para cancelar.", desc),
\t})
}

// execAgentLoopEdit executa a chamada real ao agent-loop (:8082).
// Só é chamada depois que o usuário aprova via resolvePendingAction.
func execAgentLoopEdit(argsJSON string) string {
\tclient := &http.Client{Timeout: 90 * time.Second}
\treq, err := http.NewRequest("POST", "http://127.0.0.1:8082/agent-loop", strings.NewReader(argsJSON))
\tif err != nil {
\t\treturn fmt.Sprintf("❌ Erro ao criar request: %v", err)
\t}
\treq.Header.Set("Content-Type", "application/json")
\treq.Header.Set("X-Hok-Token", "hok-api-2026")
\treq.Header.Set("X-Internal-Call", "1")
\tresp, err := client.Do(req)
\tif err != nil {
\t\treturn fmt.Sprintf("❌ Falha ao chamar agent-loop: %v\\n\\nVerifique se o backend está rodando.", err)
\t}
\tdefer resp.Body.Close()
\tbody, err := io.ReadAll(resp.Body)
\tif err != nil {
\t\treturn fmt.Sprintf("❌ Erro ao ler resposta: %v", err)
\t}
\tvar agentResp map[string]interface{}
\tif err := json.Unmarshal(body, &agentResp); err != nil {
\t\treturn fmt.Sprintf("📄 Resposta do agent:\\n\\n%s", string(body))
\t}
\tvar sb strings.Builder
\tif success, ok := agentResp["success"].(bool); ok {
\t\tif success {
\t\t\tsb.WriteString("✅ Edição concluída com sucesso!\\n\\n")
\t\t} else {
\t\t\tsb.WriteString("❌ Edição falhou\\n\\n")
\t\t}
\t}
\tif msg, ok := agentResp["message"].(string); ok && msg != "" {
\t\tsb.WriteString(fmt.Sprintf("💬 %s\\n\\n", msg))
\t}
\tif iters, ok := agentResp["iterations"]; ok {
\t\tsb.WriteString(fmt.Sprintf("🔄 Iterações: %v\\n", iters))
\t}
\tif rebuilt, ok := agentResp["rebuilt"].(bool); ok && rebuilt {
\t\tsb.WriteString("🔨 Backend recompilado e reiniciado\\n")
\t}
\tif rolledBack, ok := agentResp["rolled_back"].(bool); ok && rolledBack {
\t\tsb.WriteString("⏪ Rollback executado (build falhou)\\n")
\t}
\tout := sb.String()
\tif out == "" {
\t\tout = fmt.Sprintf("📄 Resultado:\\n\\n%s", string(body))
\t}
\treturn out
}
'''
    code = code[:idx] + new_tail
    open(path, "w").write(code)
    print("✅ chat_agent_routes.go patched")

# ─────────────────────────────────────────────
# 2. agent_loop_groq.go — novo case em executeTool
# ─────────────────────────────────────────────
path = "agent_loop_groq.go"
code = open(path).read()

anchor = '\tdefault:\n\t\treturn fmt.Sprintf("erro: ferramenta desconhecida: %s", name)'
if anchor not in code:
    print("⚠️  ANCHOR NÃO ENCONTRADO em agent_loop_groq.go — abortando esse arquivo")
else:
    replacement = '\tcase "agent_loop_edit":\n\t\treturn execAgentLoopEdit(argsJSON)\n' + anchor
    code = code.replace(anchor, replacement, 1)
    open(path, "w").write(code)
    print("✅ agent_loop_groq.go patched")

# ─────────────────────────────────────────────
# 3. routes.go — gate de aprovação em handleRoot
# ─────────────────────────────────────────────
path = "routes.go"
code = open(path).read()

anchor = "\t// ── /edit command interceptor"
if anchor not in code:
    print("⚠️  ANCHOR NÃO ENCONTRADO em routes.go — abortando esse arquivo")
else:
    gate = '''\t// ── gate de confirmação de ações pendentes ─────────────
\tif pa := getPendingAction(); pa != nil {
\t\tif isApprovalText(userMsg) {
\t\t\trespondJSON(w, map[string]string{"status": "ok", "reply": resolvePendingAction(true)})
\t\t\treturn
\t\t}
\t\tif isRejectionText(userMsg) {
\t\t\trespondJSON(w, map[string]string{"status": "ok", "reply": resolvePendingAction(false)})
\t\t\treturn
\t\t}
\t}
\t// ── fim do gate ─────────────────────────────────────────
'''
    code = code.replace(anchor, gate + anchor, 1)
    open(path, "w").write(code)
    print("✅ routes.go patched")
PYEOF

echo ""
echo "🔨 Rebuild..."
cd ~/hokma
go build -o hok ./backend
echo "✅ Build OK"

echo ""
echo "🔄 Reiniciando backend..."
if [ -f ~/hokma/hokma.sh ]; then
  ~/hokma/hokma.sh restart
else
  pkill -f "hokma/hok" 2>/dev/null || true
  sleep 1
  nohup ./hok > ~/hokma/hok.log 2>&1 &
  echo "PID: $!"
fi
sleep 2

echo ""
echo "🧪 Testando: /edit deve pedir confirmação, não executar direto"
curl -s -X POST http://127.0.0.1:8081/ -H 'Content-Type: application/json' \
  -d '{"message":"/edit teste --file backend/utils.go"}'
echo ""
echo ""
echo "✅ Se apareceu 'Confirmação necessária' acima, o patch funcionou."
echo "⚠️  Se apareceu 'Agent pensando e editando', algo não aplicou — revise os backups .bak_*"
