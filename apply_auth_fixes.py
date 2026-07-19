#!/usr/bin/env python3
import re

AUTH_BLOCK = "\tif !requireHokAuth(w, r) {\n\t\treturn\n\t}\n"

FIXES = [
    ("patch.go", "handleFsPatch", "var req PatchRequest"),
    ("fs_routes.go", "handleRebuild", "Cria arquivo de sinal para o watchdog"),
    ("routes.go", "handleVision", "var req VisionRequest"),
    ("routes.go", "handleFiles", 'if r.Method == "GET"'),
    ("routes.go", "handleMemories", "switch r.Method {"),
    ("routes.go", "handleLogs", "out := executeCommand(fmt.Sprintf("),
    ("routes.go", "handleCodex", 'if r.Method == "GET"'),
    ("routes.go", "handleSkills", "skillsDir := ROOT_PATH"),
    ("n8n_routes.go", "handleN8NProxy", 'if r.Method != "POST"'),
    ("self_heal.go", "selfHealHandler", "var req AgentLoopReq"),
    ("conversations_routes.go", "handleConversations", 'path := strings.TrimPrefix(r.URL.Path, "/conversations")'),
    ("rollback.go", "handleFsRollback", "var req struct {"),
    ("rollback.go", "handleFsBackupList", 'filePath := r.URL.Query().Get("path")'),
    ("agent_history.go", "handleAgentHistory", "limit := 20"),
    ("debug_routes.go", "handleDebugResources", 'result := map[string]interface{}{'),
]

def extract_func(src, func_name):
    m = re.search(r'func\s+' + re.escape(func_name) + r'\s*\([^)]*\)\s*(?:\*?\w+\s*)?\{', src)
    if not m:
        return None, None
    start = m.end() - 1
    depth = 0
    for i in range(start, len(src)):
        if src[i] == '{':
            depth += 1
        elif src[i] == '}':
            depth -= 1
            if depth == 0:
                return start, i
    return None, None

def apply_fix(filepath, func_name, anchor):
    with open(filepath, 'r') as f:
        src = f.read()
    bstart, bend = extract_func(src, func_name)
    if bstart is None:
        print(f"❌ {filepath}::{func_name}: função não encontrada")
        return False
    body = src[bstart:bend+1]
    count = body.count(anchor)
    if count != 1:
        print(f"⚠️  {filepath}::{func_name}: anchor achado {count}x (esperado 1) — PULADO, revisar manualmente")
        return False
    if "requireHokAuth" in body:
        print(f"ℹ️  {filepath}::{func_name}: já tem requireHokAuth — pulado")
        return False
    idx = bstart + body.find(anchor)
    line_start = src.rfind('\n', 0, idx) + 1
    new_src = src[:line_start] + AUTH_BLOCK + src[line_start:]
    with open(filepath, 'w') as f:
        f.write(new_src)
    print(f"✅ {filepath}::{func_name}: auth inserida")
    return True

ok = sum(apply_fix(f, fn, a) for f, fn, a in FIXES)
print(f"\n{ok}/{len(FIXES)} correções aplicadas.")
