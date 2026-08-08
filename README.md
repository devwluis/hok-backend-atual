# HOKMA Backend

Backend em Go para o ecossistema HOKMA — plataforma de agentes de IA com automação de workflows, CRM e integrações multiplataforma.

## Visão Geral

O HOKMA Backend é um servidor HTTP em Go que expõe uma API para agentes de IA conversacionais, automação via n8n, CRM, e integrações com Telegram e WhatsApp. Suporta múltiplos provedores de LLM (OpenRouter, Cerebras, DeepSeek, Groq, OpenAI, Gemini) e um sistema de *skills* para extensibilidade.

## Stack

- **Linguagem:** Go 1.26+
- **Banco de dados:** SQLite (`modernc.org/sqlite`)
- **Auth:** JWT (`golang-jwt/jwt/v5`)
- **Criptografia:** `golang.org/x/crypto`
- **Automação:** n8n (webhooks externos)

## Funcionalidades Principais

### Agentes de IA
- Suporte a múltiplos provedores de LLM: OpenRouter, Cerebras, DeepSeek, Groq, OpenAI, Gemini
- Integração com Claude Code (via cliente Hermes)
- Sistema de *skills* executáveis por agentes
- Memória persistente por conversa
- Modos de operação configuráveis (chat, automação, CRM)

### CRM
- Gestão de empreendimentos e imóveis
- Rastreamento de progresso de clientes
- Fontes de contexto dinâmico
- Filtros e debounce de eventos

### Integrações
- **Telegram:**abot com handlers dedicados
- **WhatsApp:** rotas para bridge de dispositivos
- **n8n:** webhooks para automação de workflows
- **Hermes/Claude Code:** execução de código remotamente

### Ferramentas Internas
- Autenticação e patches automáticos
- Self-healing de pipelines
- Checkpoints de pipeline
- Proactive agent para tarefas em background
- Rollback de alterações

## Estrutura do Projeto

```
/
├── main.go              # Ponto de entrada, servidor HTTP
├── types.go             # Structs principais (request/response)
├── ai.go                # Orquestração de chamadas LLM
├── smart_chat.go        # Lógica de chat com agentes
├── agent_loop*.go       # Loop principal do agente
├── hermes_client.go     # Cliente Claude Code
├── crm_*.go             # Rotas e lógica de CRM
├── n8n*.go              # Integração n8n
├── telegram.go          # Handler Telegram
├── whatsapp_routes.go   # Rotas WhatsApp
├── skills*.go           # Sistema de skills
├── internal/chat/       # Módulo interno de chat
├── config/              # Configurações
├── database/            # Scripts e dumps de BD
├── skills/              # Definições de skills
├── chat/                # Artefatos de chat
├── n8n/                 # Workflows n8n
└── ebm_manual/          # Manuais EBM
```

## Variáveis de Ambiente

| Variável | Descrição | Padrão |
|---|---|---|
| `PORT` | Porta do servidor | `8082` |
| `DB_PATH` | Caminho do banco SQLite | `/root/HOKMA/backend/memory.db` |
| `ROOT_PATH` | Diretório raiz | `/root/HOKMA` |
| `N8N_TOKEN` | Token para webhooks n8n | — |
| `OR_KEY` | Chave OpenRouter | — |
| `CEREBRAS_API_KEY` | Chave Cerebras | — |
| `DS_KEY` | Chave DeepSeek | — |
| `GROQ_KEY` | Chave Groq | — |
| `OPENAI_API_KEY` | Chave OpenAI | — |
| `GEMINI_KEY` | Chave Gemini | — |
| `HOK_TOKEN` | Token de autenticação | — |

## Rodando

```bash
go build -o HOKMA_backend .
./HOKMA_backend
```

Ou com variáveis de ambiente:

```bash
PORT=8082 DB_PATH=./memory.db HOK_TOKEN=seu_token ./HOKMA_backend
```

## Rotas Principais

### Chat / Agente
- `POST /chat` — Chat com o agente principal
- `POST /api/chat/agent` — Chat com o agente de rotas
- `POST /api/chat/smart` — Smart chat (com contexto ampliado)

### CRM
- `POST /api/crm/ai` — Ação de CRM via IA
- `POST /api/crm/add` — Adicionar imóvel
- `POST /api/crm/empreendimentos` — Listar empreendimentos
- `POST /api/crm/conversation` — Gerir conversa CRM

### Automação / n8n
- `POST /api/n8n/trigger` — Dispara workflow n8n
- `POST /api/n8n/webhook` — Webhook de retorno n8n
- `POST /api/triggers` — Triggers internos

### Contexto / Memória
- `GET/POST /api/conversations` — Conversas armazenadas
- `GET/POST /api/memory` — Memória persistente
- `POST /api/context/sources` — Fontes de contexto

### Skills
- `POST /api/skills/exec` — Executar skill
- `GET /api/skills/state` — Estado dos skills

### Debug / Admin
- `POST /api/debug/patch` — Aplicar patch
- `POST /api/debug/rollback` — Rollback
- `GET /api/debug/self-heal` — Self-healing
- `POST /api/debug/introspect` — Introspecção

### Integrações
- `POST /telegram` — Webhook Telegram
- `POST /api/whatsapp` — Rotas WhatsApp
- `POST /api/hermes/execute` — Execução via Hermes

## Modelo de Dados (principais)

**ClientRequest** — Request padrão do cliente:
```json
{
  "message": "string",
  "model": "openrouter/cerebras/deepseek/groq/openai/gemini",
  "messages": [{"role": "user|assistant", "content": "string"}],
  "api_key": "string (opcional, usa env se vazio)",
  "stream": false,
  "action": "string",
  "mode": "string"
}
```

**ClientResponse:**
```json
{
  "response": "string",
  "model": "string",
  "skill_used": "string",
  "mode": "string"
}
```

## Skills

O sistema de skills permite que o agente execute ações definidas em `skills/*.go`. Cada skill é um módulo autocontido que pode ser invocado durante o processamento de uma mensagem.

## Desenvolvimento

### Limpeza de Backups
Arquivos `.bak_*` e `.bak` no diretório raiz são backups automáticos. O script `fix_bug2_v2.sh` aplica correções pontuais.

### Checkpoints
`pipeline_checkpoint.go` permite salvar e restaurar o estado de pipelines de longa execução.

### Autopatch
`autopatch_loop.go` aplica correções automaticamente em loops de fundo.
