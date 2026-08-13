# COMMIT — Ferramentas (tools) adicionadas ao agente hok-saude
## Data: 2026-08-12 22:00 · Por: opencode (sessao SSH direta)
## Evidencia: DB n8n (agents table) + docker restart

### O que foi feito
1. Verificado estado real do agente hok-saude (id JBRgJSRbQUX8GlLc):
   - Existente na tabela `agents` do SQLite do n8n
   - availableInMCP=1, setupCompletedAt preenchido, ativo
   - Ja possuia 1 tool: `chat_hok_backend` (HTTP Request para backend HOK)
   - Workflow Telegram ja atualizado com no "Message an Agent"

2. Adicionadas 5 novas tools ToolWorkflow:

   | Tool | Workflow ID | Funcao |
   |------|-------------|--------|
   | monitor_saude | b5IRX5AfXs33fo5B | Status CPU/memoria/disco/uptime |
   | alerta_ram | lkmLVfUsVR34XawV | Uso de RAM e alertas |
   | monitor_disco | 0wMjXBboCkrM2kv0 | Uso de disco + auto-recuperacao |
   | backup_sqlite | SppCNi9G8wMUZKA4 | Backup do banco SQLite |
   | n8n_self_heal | focUzRrif6NTQHQB | Auto-recuperacao do n8n |

3. Atualizado schema do agente no DB + docker restart n8n_oficial

### Estado atual do agente hok-saude
- Modelo: openrouter/deepseek/deepseek-v4-flash
- Credential: T0Xd5mw8fldmAHR5 (OpenRouter)
- Instrucoes: PT-BR, agente de saude

### Tools instaladas (6 total):
1. chat_hok_backend (httpRequest) — chat com backend HOK
2. monitor_saude (toolWorkflow) — saude do servidor
3. alerta_ram (toolWorkflow) — RAM monitor
4. monitor_disco (toolWorkflow) — disco monitor
5. backup_sqlite (toolWorkflow) — backup DB
6. n8n_self_heal (toolWorkflow) — auto-recuperacao n8n

### Pendente para proxima sessao
- Validar execucao real do agente via Telegram (enviar mensagem ao bot)
- Adicionar memory de longo prazo (agents_memory_entries vazia)
- Adicionar skills/subagentes
- Configurar roteamento de modelos (custo)
- Fechar portas de seguranca pendentes (8082 exposta, OwnerGate client-side)
