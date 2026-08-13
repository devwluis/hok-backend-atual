# HOK OS — CONTEXTO MESTRE CONSOLIDADO
### Documento único de contexto para qualquer LLM (Claude, Hermes, Claude Code) trabalhando neste projeto
### Consolida: Plano Mestre 26/07→10/08 + todos os adendos de sqlmigration (partes 1–4) + validação ponta a ponta do gate `autopatch_loop.go` (sessão 10/08, chat)
### Última atualização: 2026-08-10 · Servidor: `hokma` (Hostinger KVM2, Debian 13 "trixie", kernel 6.12.95)
### Arquiteto/criador único: Washington Ferreira (Hokmá / devwluis)

> **Como usar este documento**: é o ponto de partida único para qualquer sessão nova. Não reexplique o projeto do zero — leia isto primeiro. Diferencie sempre IMPLEMENTADO de EM DESENVOLVIMENTO de VISÃO FUTURA.

---

## 0. RESUMO EXECUTIVO (estado em 13/08/2026)

- **CRM (imoveischaves.com)**: concluído, validado em produção, pausado deliberadamente por poucos dias.
- **Segurança núcleo (arquitetura n8n)**: todos os bloqueios críticos originais fechados e validados.
- **Segurança segunda arquitetura**: autopatch, task_agent e repos fechados; `automation.go` **ARQUIVADO** em 10/08 (commit b6275a9, gate só em memória + token exposto no prompt IA); helpers `n8nRequest`/`validateWorkflowJSON` extraídos para n8n_shared_utils.go.
- **Migração `sqliteExec`**: 100% concluída, 12 arquivos, ~40 call-sites, zero pendência.
- **Ponte Telegram HOK ↔ usuário**: habitada/restaurada em 13/08 — o nó "Send Reply" enviava sempre o fallback "Hokma indisponivel" (campo errado); corrigido para `{{ $json.text || ... }}` (ver COMMIT_20260813_111958).
- **Decisões arquiteturais NÃO tomadas**: multi-tenancy real (Fase 3), destino da segunda arquitetura de automação, unificação `memory.db`/`hokma.db`.
- **Próximo item de prioridade média**: segurança SaaS da varredura 12/08 (fechar rotas sem auth na 8082, OwnerGate client-side, token no bundle público).

---

## 2. INFRAESTRUTURA (fatos confirmados)

| Componente | Detalhe |
|---|---|
| Servidor | Hostinger KVM2, hostname `hokma`, Debian 13 "trixie", kernel 6.12.95 |
| Backend | Go, binário `hokma`, porta 8082, systemd (`hokma.service`), `/root/hokma/backend/`, DB `memory.db` |
| `.env` correto | `/root/hokma/backend/.env` |
| Frontend | React/Vite/TS/Tailwind, nginx porta 3002, `/var/www/hok-os/`, `app.imoveischaves.com` |
| n8n | Docker, `http://127.0.0.1:5678`, `n8n.imoveischaves.com` |
| Git | `devwluis/hok-backend-atual`, `devwluis/hok-frontend-atual` |
| Auth interna | Header `X-Hok-Token` |
| SQLite ativo | `memory.db` (`modernc.org/sqlite`) |
| SQLite órfão | `hokma.db` — só `self_mod.go`, não consolidado |

---

## 3. ARQUITETURA NÚCLEO — MOTOR N8N (validada, produção)

14 tools do agente n8n: `n8n_list_workflows`, `n8n_create_workflow`, `n8n_update_workflow`, `n8n_activate_workflow`, `n8n_execute_workflow`, `n8n_delete_workflow`, `n8n_test_workflow`, `n8n_get_execution_errors`, `n8n_diagnose_workflow`, `n8n_get_workflow_detail`, `env_diagnose_config`, `read_file`, `bash_exec`, `add_imovel`.

Modelos: Agent loop n8n usa `deepseek/deepseek-v4-flash-0731`; CRM/persona Adriana `minimax/minimax-m3`; Claude Code CLI via MiniMax M2.5/M3 (`ANTHROPIC_BASE_URL`).

### 3.1 Ponte HOK ↔ Telegram (fluxo de conversa, restaurada 13/08)
A conversa do bot (`@hokma_alertas_bot`) **não passa pelo backend Go** (que só *envia* notificações via `sendMessage` em telegram.go/crm_telegram.go — sem webhook de entrada). Toda a entrada/saída é no **n8n**: webhook `hok-multi-ia` → workflow `HOK OS Multi IA Telegram` (SFd42XABa4HvpPXT). Flow: `Webhook → Parse → IF → (Typing + "HOK Agente Saude" messageAnAgent v3) → Send Reply`. O agente `hok-saude` (JBRgJSRbQUX8GlLc, deepseek-v4-flash, 6 tools de monitoramento) responde no campo **`text`**.

> ⚠️ Corrigido 13/08: o nó "Send Reply" lia `$json.reply || $json.response` (inexistentes) e enviava o fallback "Hokma indisponivel" — o HOK parecia mudo. Agora `={{ $json.text || 'Hokma indisponivel' }}`. Expressão n8n exige o prefixo `=` ANTES de `{{...}}` (sem `=`, vira texto literal — lição 11). Detalhes: COMMIT_20260813_111958.

---

## 4. GATE DE APROVAÇÃO — ARQUITETURA DE SEGURANÇA

4 camadas: tenant isolation (`pendingActionMap` chave `tenantID:userID:convID`), extração de contexto, diff preview obrigatório, aprovação explícita (`POST /actions/approve`/`reject`) com log `[AUDIT]`.

Status de todos os vetores conhecidos: todos fechados exceto `automation.go` (gate só em memória, não sobrevive restart) e IDOR em `handleGetConversations` (bloqueante só pra Fase 3).

### 4.7 Self-modification por tenant (achado 10/08, ver Adendo seção 8)
Existe motor `self_mod.go` (`executeSelfMod`) já conectado ao mesmo `PendingAction`, isolado por tenant via `tenants/{id}/.git-worktree` + repo bare `self-mod.git`. Scripts `selfmod_commit.sh`/`selfmod_revert.sh`/`smoke_test.sh` em `/root/hokma/scripts/`. Investigação em andamento (10/08, sessão chat): `selfmod_revert.sh` não faz revert real (só echo — bug encontrado), `smoke_test.sh` só valida `/health` (raso), tabela `self_modifications` com 0 linhas apesar de código existir.

---

## 5. SEGUNDA ARQUITETURA DE AUTOMAÇÃO (12 módulos, 3059 linhas)

Descoberta 08/08, nunca ativada em produção. Status por módulo: Triggers rodando sem carga útil; Autopatch e Task Agent com gate fechado e validado; Repos com auth+SQLi fechados; Automation com gate só em memória (pendente).

---

## 6. MIGRAÇÃO `sqliteExec` — CONCLUÍDA 100% (10/08)

12 arquivos migrados de `fmt.Sprintf`+`bash -c` (shell injection) e concatenação SQL para `modernc.org/sqlite` nativo com queries parametrizadas. Zero pendência.

---

## 7. ROADMAP — FASES

Fase 0 (núcleo n8n): ✅ completo. Fase 0.5 (segunda arquitetura): quase completo, falta `automation.go` + 17 paths `ecossistema` obsoletos. Fase 1 (HOK especialista): não iniciada. Fase 2 (automodificação segura): parcial. Fase 3 (multi-tenancy real): decisão pendente. Fase 4 (produto SaaS): não iniciada.

---

## 9. LIÇÕES DE PROCESSO (evitar repetir)

1. Heredoc >5000 chars trunca no Termius → base64 + hash, blocos ~1800 chars.
2. Hífen comum vs em-dash Unicode quebra sed/patches — validar `count==1`.
3. Nunca assumir nome de campo de struct — confirmar com grep.
4. Sempre `go build` antes de `systemctl restart`.
5. Colagem sucessiva por Termius pode duplicar buffer — conferir `wc -c`.
6. Depurar rota em camadas: porta real → método de auth real → dados de teste.
7. Validar gate ponta a ponta com artefato de teste descartável.
8. `bak_*` soltos quebram `go test ./...` — mover pra fora do projeto.
9. Resposta textual de sucesso não prova execução real — validar efeito concreto.
10. `...` em texto explicativo nunca é literal — sempre comando completo real.
11. n8n: a saída de um "Message an Agent" (v3) é `$json.text` (não `reply`/`response`); e expressão de campo exige o prefixo `=` antes de `{{...}}`, senão vira texto literal. Usar o export do workflow como fonte da verdade do formato.

---

## 10. PADRÃO DE PROMPT DE SISTEMA

```
Você é o HOK, assistente técnico do Washington no projeto HOK OS.
1. Leia este documento antes de propor patch — não invente campo/arquivo.
2. Patches >3000 chars sempre em base64, nunca heredoc direto.
3. Não repita erros da seção 9.
4. Ações em produção passam pelo gate — nunca sugira bypass.
5. Fale direto. Sem certeza de path/campo/valor: confirme antes.
6. "Executado com sucesso" no chat não é prova — valide o efeito real.
```

---

*Consolidado a partir de: PLANO_MESTRE_HOKMA_SAAS_CONSOLIDADO (26/07→10/08), adendo_sqlmigration partes 1–4 (10/08), sessão de chat 10/08 (gate autopatch_loop.go), e investigação em andamento do self-mod engine (10/08, ver arquivo ADENDO complementar nesta mesma pasta). Próxima atualização deve vir de sessão real de trabalho no servidor.*
