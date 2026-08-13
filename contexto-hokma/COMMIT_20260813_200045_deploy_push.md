# COMMIT — Deploy da guarda anti-XXE + push dos repos (backend e contexto)

Data: 2026-08-13 20:00 · Por: Washington + opencode (sessão remota via SSH)
Escopo: backend Go (/root/hokma/backend) + repo de contexto (/root, branch clean_master)

---

## 1. Contexto de partida
Sessão retomada a partir de COMMIT_20260813_194825_endurecimento_seguranca.md
(pendências: rebuild com a guarda, push do backend 5389be2, push do contexto).

## 2. Descoberta: commit 5389be2 nao compilava isolado
- git worktree add em 5389be2 + go build -> FALHOU:
  n8n_tools.go:83: assignment mismatch: 3 variables but n8nRequest returns 2 values.
- Causa: n8nRequest commitado com assinatura antiga ([]byte, error), mas
  n8n_tools.go (commitado junto) ja usava a nova ([]byte, int, error).
  O restante do fix estava no working tree (27 arquivos modificados, da sessao paralela).
- Decisao (aprovada pelo usuario): commitar o working tree e pushar ambos.

## 3. Commit 4dd98c7 — security+fix consolidado
security+fix: endurece auth (constant-time, roleAuthorized, N8N_TOKEN fail-closed),
consolida n8nRequest 3 retornos, auto-aprovacao chat trivial, fixes self-mod e webhook
- 27 arquivos, +502/-180.
- Inclui (entre outros): requireHokAuth com subtle.ConstantTimeCompare,
  roleAuthorized (JWT de cliente nao acessa admin), N8N_TOKEN sem valor
  hardcoded (webhook fail-closed), auto-aprovacao de prompts triviais do
  Claude Code e de comandos de leitura (bash_exec auto), fixes de self-mod
  (hash de commit, smoke test com arquivo), runSmartText com context.Context.

## 4. Validacao antes do deploy
- go build do worktree em 4dd98c7 -> OK (/tmp/hokma_4dd98c7).
- go test -run "TestGuardWorkflowXML|TestParseMCPWorkflowVerdict|TestGuardrailWorkflowId"
  -> PASS (guarda anti-XXE, veredito MCP, guardrail de workflow id).

## 5. Deploy em producao
- Backup do binario anterior: hokma.prev_20260813_1959.
- systemctl restart hokma.service -> active, /health OK,
  log: "Hokma v22 -> http://localhost:8082" (19:59:51).
- Nota: o binario anterior (13:26) NAO tinha a guarda nem o endurecimento
  de auth — a producao ficou horas sem essas protecoes. Agora esta alinhada ao main.

## 6. Push (pendencias 2 e 3 do commit anterior)
- Backend: b6275a9..4dd98c7  main -> main (devwluis/hok-backend-atual).
- Contexto: branch clean_master criada no remoto (HEAD ebc791b2f)
  — foi necessario git config credential.helper store no repo /root
  (o helper so existia no repo do backend).

## 7. Pendencias que seguem em aberto
- Rotacionar token do n8n (exposicao previa no settings do Claude Code).
- Seguranca SaaS da varredura 12/08: 8082 exposta sem auth (/introspect,
  /status), OwnerGate client-side + token no bundle publico, CORS *, timeout
  em ExecuteApprovedCommand.
- Sessao de calibracao de persona/chat (diagnostico feito em paralelo nesta
  mesma sessao: skill router intercepta conversa normal, SOUL.md inexistente,
  rate limit Groq 100k/dia com fallback OR "Insufficient Balance") — planos
  apresentados ao usuario, execucao pausada por priorizacao desta sessao.

---

*Gerado a partir de sessao real no servidor (13/08/2026, 20:00). Complementa
COMMIT_20260813_194825 e os demais COMMIT_* da pasta.*
