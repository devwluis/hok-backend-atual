# Instruções para agentes LLM (opencode / Claude Code / outros)

## ANTES DE QUALQUER TAREFA
Leia o contexto atual do sistema em:
  `/root/hokma/HOK_STATE.md`  (auto-gerado a cada 15min — NÃO editar)

Ele contém: infra, rotas ativas, gates de aprovação, commits recentes, pendências
e padrões de falha conhecidos. Use-o em vez de perguntar ao usuário o estado do sistema.

Se o arquivo não existir, gere com: `sh /root/hokma/backend/scripts/hok_state.sh`

## Regras de trabalho (projeto HOK OS — backend Go)
1. Idioma: responda sempre em português brasileiro (pt-BR).
2. Antes de editar um .go: backup com timestamp (cp arquivo.go arquivo.go.bak_$(date +%Y%m%d_%H%M%S)).
3. Patches grandes (>3000 chars) → sempre base64, nunca heredoc direto (trunca no Termius).
4. Confirme campos de struct com grep antes de assumir nome (erro comum documentado).
5. Ações em produção passam pelo gate de aprovação — nunca sugira bypass.
6. Deploy: build isolado (go build -o hokma_test .) antes de sobrescrever o binário;
   confirme com o usuário antes de systemctl restart/stop/start em produção.
7. Adendos de sessão: salvar em backend/contexto-hokma/ E disparar webhook do Drive.