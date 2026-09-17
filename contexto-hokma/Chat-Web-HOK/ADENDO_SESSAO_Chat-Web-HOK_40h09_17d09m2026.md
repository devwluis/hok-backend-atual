# Adendo — Sessão Chat-Web-HOK / 40:09/17/09/2026

## Diagnóstico Completo da Interface de Chat

### Escopo
Diagnóstico da interface de chat do HOK OS (ChatScreen, backend, provedores de IA), sem aplicação de fixes — apenas mapeamento de issues por prioridade.

### Resultado: 17 issues encontradas
- 🔴 Críticos: 3
- 🟡 Importantes: 8
- 🟢 Cosméticos: 6

Ver relato completo em: `/root/hokma/backend/contexto-hokma/DIAG_CHAT_INTERFACE_20260917.md`

---

## Fix Aplicado: OpenCode Serve (Item 1)

### Problema
`opencode-serve.service` INATIVO há +1 semana. Porta 4100 sem listener. Engine `opencode_serve` da cascata de chat desabilitado.

### Causa raiz
Serviço morto desde 10/Set (signal=TERM, MainPID=0). Possível kill manual ou pressão de memória (último pico 723MB).

### Ação
```bash
systemctl start opencode-serve.service
```

### Resultado
| Verificação | Resultado |
|-------------|-----------|
| Porta 4100 escutando | ✅ `LISTEN 0 512 127.0.0.1:4100` |
| Processo | ✅ PID 1713825, 311MB RSS, 18% CPU |
| POST /session | ✅ Retornou `ses_f50b63b6cffeMQWC65jZQ5rVse` |
| HOK_STATE.md | ✅ Atualizado para active |

---

## Fix Aplicado: Remoção Gemini Vision + OpenAI Vision + Roteamento DeepSeek Vision (Item 2)

### Problema
Todas as 3 APIs de visão falhavo nos logs:
- OR Vision: `User not found` (Gemini key inválida)
- Gemini Vision: `invalid authentication credentials` (GEMINI_KEY revogada)
- OpenAI Vision: `Incorrect API key` (chave `gsk_...` era Google, não OpenAI)

### Ações
1. **Removido** `callGeminiVision` de `ai.go` (56 linhas)
2. **Removido** `callOpenAIVision` de `ai.go` (9 linhas, já dead code)
3. **Reescrito** `callDeepSeekVision` para chamar API DeepSeek diretamente (DS_URL + DEEPSEEK_API_KEY, modelo `deepseek-flash`, formato vision OpenAI-compatible)
4. **Atualizados** comentários em `smart_chat.go` (2 blocos) e `terminal_routes.go` (1 bloco)

### Validação
| Teste | Resultado |
|-------|-----------|
| `go build` | ✅ PASS |
| DeepSeek API key | ✅ Válida (sk-a6be7e0abff340138c4f42581edb5619) |
| DeepSeek Vision (imagem) | ✅ Processou imagem 1x1 PNG corretamente |
| `callGeminiVision` no código | ✅ 0 referências |
| `callOpenAIVision` no código | ✅ 0 referências |

### Chaves confirmadas
- **DEEPSEEK_API_KEY**: `sk-a6be7e0abff340138c4f42581edb5619` — válida, mantida
- **GEMINI_KEY**: revogada — código removido
- **OPENAI_KEY**: vazada (`gsk_fYDx...vkk7` era Google, não OpenAI) — código removido

---

## Fix Aplicado: Migração ModelA → ModelB (Item 3)

### Problema
`activeModel = ModelA` (deepseek/deepseek-chat) — sem crédito OpenRouter.
Log: "Insufficient Balance" (08:52:26). ModelA como padrão causava falha no chat.

### Ações
1. **`ai.go:488`**: `activeModel = ModelA` → `activeModel = ModelB`
   - ModelB = `nvidia/nemotron-3-super-120b-a12b:free` (globals.go:27)
   - Comment atualizado: "free verificado; ModelA sem crédito OR"
2. **`ai.go:493`**: Comentário "Falla para ModelA" → "Falla para ModelB"
3. **DB** (memory.db app_settings): `activeModel=z-ai/glm-5.2:free` → `activeModel=nvidia/nemotron-3-super-120b-a12b:free`

### Resultado
- Novo modelo padrão: `nvidia/nemotron-3-super-120b-a12b:free` (ModelB)
- Backend precisa de restart para carregar novo modelo na memória
- Build ✅ PASS

---

## Item 3 — Resumo das Mudanças

---

## Arquivos Modificados
- `/root/hokma/backend/ai.go` — Remoção Gemini/OpenAI Vision, nova callDeepSeekVision, activeModel ModelA→ModelB
- `/root/hokma/backend/smart_chat.go` — Atualização de comentários
- `/root/hokma/backend/terminal_routes.go` — Atualização de comentário
- `/root/hokma/backend/memory.db` — activeModel atualizado para ModelB

## Backups Criados
- `/root/hokma/backend/ai.go.bak_20260917_092658_gemini_removal`
- `/root/hokma/backend/smart_chat.go.bak_20260917_092658_gemini_removal`
- `/root/hokma/backend/terminal_routes.go.bak_20260917_092658_gemini_removal`

## Processos Impactados
- `opencode-serve.service` — REINICIADO (estava dead há +1 semana)
- Backend hokma — precisa de RESTART para carregar ModelB na memória
