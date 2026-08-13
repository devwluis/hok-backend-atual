# DECISÃO DE FOCO — 2026-08-12
## SaaS HOK: especialista em automações + auto-edição sob comando do usuário

Autor: Washington Ferreira (sessão de chat com agente de código, 12/08/2026)

## 1. Foco definido pelo usuário
- **CRM pausado** (imoveischaves.com): a lista de imóveis no Drive era alimentação do CRM. Não priorizar.
- **Objetivo: projeto SaaS HOK** com dois pilares:
  1. **HOK especialista em automações complexas** (n8n como núcleo)
  2. **HOK auto-editor de si mesmo**, sob comando explícito e total do usuário (self-modification)

## 2. Estado atual relevante (varredura 12/08/2026)

### Pilar 1 — Automação (n8n)
- Motor com 14 tools validado em produção (n8n_list/create/update/activate/execute/delete/test_workflow, get_execution_errors, diagnose, get_workflow_detail, env_diagnose_config, read_file, bash_exec, add_imovel)
- Guardrail determinístico de args (validateArgsBeforePending) em produção
- Módulo automation.go ARQUIVADO em 10/08 (n8n_shared_utils.go extraiu helpers) — segunda arquitetura (triggers/autopatch/task_agent/repos) mantida
- Crawler do Drive (aevo_drive_crawler_v6.py) AUSENTE — cron falha a cada 6h; era alimentação do CRM (pausado) — decidir: remover cron ou recuperar

### Pilar 1 — Ponte HOK ↔ Telegram (conversa do bot) — RESTAURADA 13/08
- A conversa passa pelo n8n (webhook `hok-multi-ia` → workflow `HOK OS Multi IA Telegram`
  SFd42XABa4HvpPXT), NÃO pelo backend Go (telegram.go só envia `sendMessage`, sem webhook de entrada).
- Agente `hok-saude` (JBRgJSRbQUX8GlLc, deepseek-v4-flash, 6 tools de monitoramento) responde
  ao chat via "Message an Agent" (v3), com resposta no campo `text`.
- **Bug corrigido 13/08:** nó "Send Reply" lia `$json.reply || $json.response` (inexistentes) →
  enviava sempre o fallback "Hokma indisponivel" (HOK parecia mudo). Fix: `={{ $json.text || 'Hokma indisponivel' }}`.
- Estado: conversa usuário ↔ HOK funcionando (execução 19263 validada). Ver COMMIT_20260813_111958.

### Pilar 2 — Auto-edição (self-mod)
- ✅ **CONECTADO e validado em produção em 12/08** (corrigido o roteamento ToolName vs ActionType):
  `executeSelfMod` (commit + smoke real + revert + audit em `self_modifications`) é alcançado;
  ver COMMIT_20260812_104500_automod_completo.md.
- selfmod_revert.sh corrigido em 11/08 (revert real); smoke_test.sh reforçado (go build + healthcheck).
- Pendência restante (não-bloqueante): smoke_test.sh não valida `.ts/.tsx` do frontend.

### Segurança que bloqueia a visão SaaS (da varredura 12/08)
- 8082 exposta na internet; /introspect e GET / (status) sem auth
- OwnerGate 100% client-side + hash SHA-256 no bundle + HOK_TOKEN antigo hardcoded no bundle público
- Sem timeout em comandos aprovados (ExecuteApprovedCommand)
- Fila de device global sem isolamento de tenant
- CORS * + X-Forwarded-For confiável
- Credenciais Google do crawler (config/aevo-*) + cópias do backend em /tmp/hokma_build_auth e /tmp/hokma_build_test2 (legíveis por qualquer usuário local)

## 3. Próximos passos propostos (ordem sugerida)
1. ✅ Conectar o motor de auto-edição (decisão ToolName vs ActionType) — **FEITO 12/08**, validado (COMMIT_20260812_104500).
2. ✅ Reforçar smoke_test.sh — **FEITO 12/08** (healthcheck + go build), exceto validação de `.ts/.tsx`.
3. 🔴 Fechar rotas sem auth + porta 8082 + limpar /tmp com cópias sensíveis — **PENDENTE/prioridade**.
4. Decidir destino do crawler do Drive (remover cron ou recuperar fonte).
5. Fase 1 (especialista): ampliar arsenal n8n com workflows de automações complexas reutilizáveis.
6. ✅ Ponte hok-saude → conversa Telegram — **RESTAURADA 13/08** (Message an Agent respondendo; bug do fallback corrigido).
