#!/bin/bash
set -e
cd ~/hokma/backend

echo "🔄 Restaurando arquivos duplicados a partir de backups limpos..."
cp agent_loop_groq.go.bak_1784378896 agent_loop_groq.go
cp routes.go.bak_20260718_144145 routes.go
echo "✅ Restaurados"

echo ""
echo "🔧 Reaplicando patches (idempotente — não duplica se já existir)..."

python3 << 'PYEOF'
# ─────────────────────────────────────────────
# 1. agent_loop_groq.go — novo case em executeTool
# ─────────────────────────────────────────────
path = "agent_loop_groq.go"
code = open(path).read()

if 'case "agent_loop_edit":' in code:
    print("ℹ️  agent_loop_groq.go já tem o patch — pulando")
else:
    anchor = '\tdefault:\n\t\treturn fmt.Sprintf("erro: ferramenta desconhecida: %s", name)'
    if anchor not in code:
        print("⚠️  ANCHOR NÃO ENCONTRADO em agent_loop_groq.go — abortando esse arquivo")
    else:
        replacement = '\tcase "agent_loop_edit":\n\t\treturn execAgentLoopEdit(argsJSON)\n' + anchor
        code = code.replace(anchor, replacement, 1)
        open(path, "w").write(code)
        print("✅ agent_loop_groq.go patched")

# ─────────────────────────────────────────────
# 2. routes.go — gate de aprovação em handleRoot
# ─────────────────────────────────────────────
path = "routes.go"
code = open(path).read()

if "gate de confirmação de ações pendentes" in code:
    print("ℹ️  routes.go já tem o patch — pulando")
else:
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
echo "🔍 Confirmando que cada patch aparece EXATAMENTE 1 vez..."
echo -n "agent_loop_edit em agent_loop_groq.go: "
grep -c 'case "agent_loop_edit":' agent_loop_groq.go
echo -n "gate em routes.go: "
grep -c "gate de confirmação de ações pendentes" routes.go
echo -n "execAgentLoopEdit em chat_agent_routes.go: "
grep -c "^func execAgentLoopEdit" chat_agent_routes.go

echo ""
echo "🔨 Rebuild (caminho correto, onde está o go.mod)..."
cd ~/hokma/backend
go build -o ~/hokma/hok .
echo "✅ Build OK"

echo ""
echo "🔄 Reiniciando backend..."
if [ -f ~/hokma/hokma.sh ]; then
  ~/hokma/hokma.sh restart
else
  pkill -f "hokma/hok" 2>/dev/null || true
  sleep 1
  cd ~/hokma
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
