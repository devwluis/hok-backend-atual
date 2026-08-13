# Adendo — Auditoria de Segurança, Limpeza de Paths e Recuperação de Skills
**HOK OS / Hokma Backend — srv1812236 (Hostinger KVM2)**
**Data da sessão:** 10 de agosto de 2026

---

## 1. Resumo executivo

1. **Limpeza de paths obsoletos** (`ecossistema` → `hokma`) — fechado.
2. **6 findings de segurança da auditoria original** — 5 já corrigidos em 08/08–09/08; 1 (`task_agent.go` / `trySkillForMessage` sem gate) finalizado nesta sessão.
3. **Recuperação de 103 skills** ausentes em produção — causa raiz: nunca migradas da Hetzner (não apagadas). Recuperadas de backup Hetzner truncado, backup automático adicionado.

**Nenhum item de segurança crítico em aberto ao final desta sessão**, ressalva de escopo em `pendingActionMap` para usuários anônimos (seção 5, risco baixo).

---

## 3.2 Fix aplicado: bash sem gate em `trySkillForMessage`

Duas funções faziam a mesma operação com gates diferentes: `handleTaskAgent` (correto, `pending_approval`) vs `trySkillForMessage` (executava `exec.Command("bash","-c",action)` direto, sem aprovação, disparado via `POST /` e `POST /chat/smart`).

**Correção**: `trySkillForMessage` passou a chamar `setPendingAction(..., "task_agent", action, diff)` em vez de executar direto, reaproveitando `resolveTaskAgentPendingAction` já existente. Import `os/exec` removido. Build validado, deploy feito, binário confirmado atualizado.

**Pendência**: validação funcional end-to-end (disparar skill real com bash via chat) ainda não feita — skills estavam vazias no momento do patch, restauradas na mesma sessão (seção 4).

---

## 3.5 e 3.6 — outros findings, já corrigidos antes desta sessão
- Gate de automação perdido no restart: falso — `pending_action.go` já persiste em SQLite, log de boot confirma recarregamento.
- Token de exemplo hardcoded: falso — `HOK_TOKEN` obrigatório via env, sem fallback, servidor recusa iniciar se ausente.

---

## 4. Recuperação de skills

`/root/hokma/backend/skills/` estava vazio (nasceu vazio na migração 05/08, `Birth` do diretório confirma). Recuperados 103 arquivos de `/root/hetzner_full_backup_20260708.tar.gz` (backup truncado, mas cobria a pasta skills inteira). Todos os `.json` validados sem corrupção. Backup automático diário adicionado ao script existente `/root/scripts/backup-memory-db.sh` (mesma rotação de 30 dias).

---

## 5. Isolamento `pendingActionMap` — ressalva de escopo

Chave composta `tenantID:userID:convID`, mutex, TTL 30min, persistência SQLite, testes automatizados passando (`TestTenantIsolation`, `TestTenantIsolationConcurrent`). Ressalva: sem `X-Conversation-Id` e sem JWT, dois usuários anônimos na mesma tenant colidem na mesma chave (`tenant:anonymous:default`). Risco baixo no modelo atual (single-tenant, token único). Revisitar se/quando virar multi-usuário via JWT real.

---

## 6. Pendências fora do escopo desta sessão
1. `aevo_drive_crawler_v6.py` ausente do disco, cron falha silenciosamente a cada 6h.
2. `rclone` remote `gdrive:` com token OAuth expirado — segunda via de backup se reconectado.
3. Validação funcional end-to-end do patch 3.2 (skill real com bash, ciclo completo de aprovação).

---

## 8. Achado principal — motor de self-modification por tenant já implementado

Backend já contém arquitetura de auto-edição isolada por tenant, não documentada antes:

```
tenants/owner/manifest.json  → scopes: backend/frontend/n8n_workflows, core_protected:true
tenants/owner/.git-worktree/ → aponta pra self-mod.git (repo bare)
self-mod.git/                → histórico real de commits, incluindo 1 revert testado (80f1ef1)
sandbox/                     → existe, propósito não investigado
```

**Motor central `self_mod.go`** (`executeSelfMod`): executa comando → identifica arquivo modificado → commita via `selfmod_commit.sh` → roda `smoke_test.sh` → se falhar, reverte via `selfmod_revert.sh` → audita em `self_modifications` (`hokma.db`).

Recebe `*PendingAction` — já conectado ao mesmo gate de aprovação testado nesta sessão. Infraestrutura para "auto-edição sob aprovação do usuário final" já existe estruturalmente; falta decidir como o usuário final (não só `owner`) aciona esse fluxo.

**Pendências de investigação registradas em 08.3**: conteúdo real dos 3 scripts shell, histórico da tabela `self_modifications`, rota HTTP que aciona `executeSelfMod`, propósito de `/root/hokma/sandbox`.

### ATUALIZAÇÃO (mesma data, sessão de chat posterior)
Investigação dos 3 scripts concluída:
- `selfmod_commit.sh` — funcional, faz `git add -A` + `git commit` no worktree do tenant com metadata completa (solicitante, arquivo, timestamp, diff).
- **`selfmod_revert.sh` — BUG ENCONTRADO: não faz revert real.** Só imprime `echo "Revertido $HASH..."`, nenhum `git revert`/`git reset` executado. Rollback automático de `executeSelfMod` (item 5 do ciclo) não funciona de fato — corrigir antes de confiar no fluxo de auto-modificação.
- `smoke_test.sh` — validação rasa, só checa `GET /health`, não valida efeito específico do patch aplicado.
- Tabela `self_modifications` no SQLite: **0 linhas**, apesar do `executeSelfMod` supostamente fazer INSERT — não confirmado ainda se o código realmente persiste ou se nunca foi executado em produção via essa rota (`self_mod.go` completo ainda não lido nesta sessão de chat).

Multi-tenancy e integração Claude Code/Drive discutidas na mesma sessão (fora do escopo técnico deste adendo, ver `HOK_MASTER_CONTEXT.md` seção 1.2 sobre continuidade entre sessões).

---

*Documento gerado em 10/08/2026. Atualizado na mesma data com achados da sessão de chat (bug do selfmod_revert.sh). Complementa `HOK_MASTER_CONTEXT.md` nesta mesma pasta.*
