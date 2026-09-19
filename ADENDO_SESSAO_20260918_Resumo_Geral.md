# ADENDO SESSÃO 20260918 — Resumo Geral HOK OS

## Data: 2026-09-18

## 1. Rclone gdrive — RESOLVIDO
- Causa: rclone config sem client_id/client_secret (usava shared default aposentado)
- Fix: configurado com credenciais de `drive_creds.env` + token OAuth2 renovado via Python
- Arquivo: `/root/.config/rclone/rclone.conf`
- Teste: `rclone lsd gdrive:` ✅ lista pastas normalmente

## 2. N8N_TOKEN rotacionado
- Antigo: `N8N_TOKEN_REDACTED`
- Novo: `N8N_TOKEN_REDACTED`
- Backup: `.env.bak_before_rotation_20260918_065209`
- N8N update 2.34.4 → 2.39.8 (container recriado, 12 workflows ativados)
- Old container mantido: `n8n_oficial_OLD_20260918` (rollback disponível)

## 3. Backend fixes (git commit)
- Commit `0cfd9f9`: hermes_chat.go, hermes_client.go, models_catalog.go, models_routes.go
- Commit `37625fe`: limpeza de 9 binaries test antigos
- Hokma binary rebuild + systemctl restart ✅

## 4. Backup completo
- `/root/backups/pre_commit_20260918_061639/` (3.7G)
- Recovery tag: `pre-hermes-fix-20260918`

## 5. Limpeza
- Bak files: 118 → 10 (mantidos 10 mais recentes)
- Test binaries: 9 removidos

## 6. Workana scraper test
- Execução 34400 SUCCESS via n8n API (triggerToStartFrom: Schedule Scraper)

## 7. Adendo rotacionamento + upload Drive
- Criado em `CaixaPreta-Hok/Especialista-N8N/adendo_rotacao_credenciais_20260918.md`
- Link: https://drive.google.com/file/d/1RqAwY9Hjpvpj9KlYLgZc2DDs0GIR_je6/view?usp=drivesdk

## 8. Fase 1 — generatePlan() patch
- Nova função `generatePlan()` em `agent_orchestrator.go:343`
- 3 call sites modificados (run_engine orquestrador, mutant orquestrador, run_engine subagent)
- Handler extração plano com fallback para parsePlan
- Zero alteração em gates de segurança
- Backup: `agent_orchestrator.go.bak_20260918_104907`

## 9. Fase 2 — Codebase Memory (SQLite FTS5)
- Script: `scripts/codesearch.py` (Python stdlib apenas)
- Index: 392 files, 44,308 lines, 6.7MB (FTS5 + VACUUM)
- Causa raiz 535MB: tabelas auxiliares FTS5 não vacuumadas → fix com VACUUM
- Automação: needs_rebuild() com comparação mtime, integrar via subprocess.run(["python3", "scripts/codesearch.py", "--init"], check=False) após go build + go vet no fluxo de patch
- .gitignore: .codesearch/ adicionado

## 10. E2E tests + service verification
- Hokma (8082): 401 ✅, N8N (5678): 200 ✅, Nginx (3002): 200 ✅, OpenCode (4100): 401 ✅
- Hermes proxy (3000): desativo (start.sh não é entrypoint)

## Pendências
- HOK_TOKEN rotation (alto risco)
- Proxy persistência (start.sh → entrypoint)
- Fase 3: terse_mode
