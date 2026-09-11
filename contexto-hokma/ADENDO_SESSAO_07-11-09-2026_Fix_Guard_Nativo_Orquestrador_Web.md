# Adendo — Sessão 11/09/2026 · Fix do Guard Nativo (Orquestrador sem tools no chat web)

**Origem:** opencode (terminal) | **Data/hora:** 11-09-2026 07:57 (-03) | **Subagente:** Especialista N8N

---

## 1. Bug reportado

No chat web, uma sessão com **engine Orquestrador + modelo DeepSeek V4 Flash** reportou
que **nenhum tool schema estava anexado** — não conseguia chamar `n8n_diagnose_workflow`
nem gerar `pending_action`, embora as tools existam no backend.

## 2. Causa raiz

`runSmartTextCascade` (`smart_chat.go`) tinha um **GUARD NATIVO** na primeira linha que
retornava cedo para `buildFallbackChatWithModel` (chat puro, **sem tools**) sempre que o
modelo ativo era `deepseek-native/*` — **antes** de chegar a `tryOrchestrator` (e demais
engines). Com o ativo `deepseek-native/deepseek-flash`, toda mensagem do chat web perdia
tools, independente do engine selecionado.

Agravante: `tryOrchestrator` também reescrevia modelos "pagos" para `ModelB`
(`minimax/minimax-m3:free`, descontinuado) → caía no fallback `ModelC`.

**Prova (teste pré-fix):** `/chat/smart` + `forceOrchestrator:true` + modelo nativo →
`mode=chat`, `engine=chat`, reply "não tenho acesso a ferramentas". O controle
`/agents/orchestrate` com o MESMO modelo nativo funcionava com tools
(`callGroqAgentLoop` roteia nativo via `DS_URL` com tool-calling).

## 3. Fix aplicado (`smart_chat.go`, 2 mudanças)

**Mudança 1 — guard só no fluxo default:**
```go
forcedEngine := req.ForceOrchestrator || req.ForceClaudeCode || req.ForceOpenCode || req.ForceHermes
if !forcedEngine {
    if m := nativeModelSelection(req); m != "" {
        return *buildFallbackChatWithModel(msg, req, "", m)
    }
}
```

**Mudança 2 — isenção do nativo na reescrita free-only do orquestrador:**
```go
if req.Model != "" && !isFreeModel(req.Model) && !isNativeModelSlug(req.Model) {
    req.Model = ModelB
}
```
(mesmo padrão de `allowedPaidModels` em `ai.go`).

## 4. Deploy

| Etapa | Resultado |
|---|---|
| `go build` | ✅ `a12bf73fd419f699587836fa5fe8ebcd` |
| `go vet ./...` | ✅ exit 0 |
| `go test ./...` | ✅ `ok hokma_backend 24.296s` |
| Deploy (`./deploy.sh`) | ✅ health HTTP 200, hashes idênticos |
| Backups | `smart_chat.go.bak_20260911_065228_nativeguard`, `smart_chat.go.bak_20260911_070642_nativeorchestrator`, `hokma.bak_*` (deploy.sh) |

## 5. Verificação funcional (Testes A/B/C)

Tarefa: *"Use a tool n8n_list_workflows para listar os workflows do n8n."*

| Teste | Fluxo | Resultado |
|---|---|---|
| **A** | `/chat/smart` + `deepseek-native/deepseek-flash` + `forceOrchestrator:true` | ✅ `mode=orchestrator`, `engine=orchestrator`, **`model_used=deepseek-native/deepseek-flash`** (não ModelB/ModelC), tools funcionando (retornou os 12 workflows). Log: `isNative=true`, `finish=tool_calls` → `stop` |
| **C** | `/chat/smart` + `nemotron:free` + `forceOrchestrator:true` | ✅ `mode=orchestrator`, `model_used=nemotron:free` (modelo pedido, sem mais override pro nativo) |
| **B** (controle) | `/agents/orchestrate` + modelo nativo | ✅ `status ok`, `steps 2`, tracing `kind=tool tool=n8n_list_workflows`, retornou os 12 workflows |

**Os 3 pontos exigidos no Teste A foram confirmados:** mode=orchestrator, tools funcionando,
model_used = DeepSeek nativo.

## 6. Conclusão

Bug do chat web corrigido: com engine forçado, o guard nativo não intercepta mais e o
Orquestrador usa o DeepSeek nativo via `DS_URL` com tools. O comportamento default
(sem engine forçado) continua igual (nativo = chat puro).
