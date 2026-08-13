# COMMIT — Revisão da implementação n8n Agents (madrugada 12/08) vs. vídeo "n8n Agents Preview"
Data: 2026-08-12 10:55 · Por: opencode · Evidência: DB n8n + pacotes do container n8n 2.34.4

## O que a madrugada implementou (fato)
1. Agente "hok-saude" (id JBRgJSRbQUX8GlLc) criado via API, publicado (activeVersionId 579aeae8):
   - model: openrouter/deepseek/deepseek-v4-flash (evolução: phi-4 → gpt-oss-20b:free → final)
   - instructions PT-BR + personalisation; credential OpenRouter T0Xd5mw8fldmAHR5
   - tools=[] skills=[] availableInMCP=0 setupCompletedAt=NULL
2. 5 execuções de teste reais via API (05:41-05:45): 4 success / 1 error (modelo antigo);
   última com deepseek-v4-flash = success. 5 threads criadas.
3. Mapeamento (SEM implementar): ponte agente→Telegram via MCP nativo.

## Gap vs. vídeo (o que NÃO foi feito)
| Recurso do vídeo | Estado | Gap |
|---|---|---|
| Disparo por workflow/chat n8n/Telegram/Slack | órfão (só API) | CRÍTICO: workflow "HOK OS Multi IA Telegram" chama HOK backend via HTTP; agente não participa |
| Tools (MCP, HTTP, código, nodes, workflows como tool) | tools=[] | CRÍTICO: agente não tem NENHUMA ferramenta de ação |
| Skills / subagentes / knowledge base | só instructions | MÉDIO |
| Memória de longo prazo | não configurada | CRÍTICO p/ visão SaaS |
| Roteamento de modelos (custo) | 1 modelo fixo | MÉDIO |
| Monitoramento (sessions/tracing/logs) | infra nativa existe (agent_execution: timeline, tokens, cost; agents_messages/observations) | BAIXO — validar no UI |

## Infra DISPONÍVEL no n8n 2.34.4 (confirmado nos pacotes, pronto p/ usar)
- nodes-langchain/dist/nodes/agents: Agent (V1/V2/V3) + AgentTool ("Message an Agent" — agente como ferramenta)
- tools/: ToolHttpRequest, ToolCode, ToolWorkflow (workflow inteiro como tool!), ToolVectorStore, ToolWikipedia, ToolCalculator...
- mcp/: McpClient, McpClientTool, McpRegistryClientTool, McpTrigger
- trigger/: ChatTrigger, ManualChatTrigger
- MCP nativo do agente: DESABILITADO (settings MCP vazios) → requer PATCH /rest/mcp/settings (mcp.access.enabled=true) + availableInMCP=true

## Conclusão
Implementação = "hello world" validado (criar/publicar/testar agente via API).
FALTA o núcleo da visão: conectar o agente a um canal (Telegram) + dar tools reais.
Prioridade 1: rota Telegram com "Message an Agent" (AgentTool) OU MCP nativo; depois tools (ToolWorkflow com workflows HOK existentes).

## Riscos
- setupCompletedAt=NULL: agente criado por API sem onboarding completo — validar UI.
- n8n:latest (2.34.4) — Agents é Preview; API pode mudar em upgrades.
- deepseek-v4-flash sem :free no OpenRouter — custo ~$0.14/M (baixo, mas não zero).
