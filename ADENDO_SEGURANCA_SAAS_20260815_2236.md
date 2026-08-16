# Adendo de Segurança SaaS — 2026-08-15

Correção de dívida de segurança do SaaS Hokma. Escopo: backend Go (`/root/hokma/backend`).
CRM fora de escopo. `auth_routes.go` não tocado (sessão paralela/open-code).

## 1. bashExecTool — REDESENHADO (maior risco) ✅
**Problema:** a tool `bash_exec` do agente rodava shell `bash -c` livre como root,
com apenas uma denylist de substring sobre o comando bruto — trivivel de burlar
(base64, variável, concatenação, `/proc/self/environ`, SSH keys, exfil por curl).
Descoberto durante a auditoria: no loop principal o `bash_exec` nem passava pela
denylist — ia direto pro `bash -c` pós-aprovação.

**Fix implementado (Opção A' — allowlist + argv fixo, commit `5ebeec8`):**
- `bash_exec` do agente agora roda SÓ chaves de uma allowlist, via `exec.Command`
  com argv FIXO (sem shell/pipes/redirect/variáveis). Fora da lista → fail-closed.
- `cmd` virou enum no JSON Schema da tool (o LLM nem propõe shell livre).
- `resolveFsExecPendingAction` ramifica por ToolName: `bash_exec` (agente) só roda
  allowlist; `fs_exec` (`/fs/exec`, comando do humano) mantém fluxo bruto aprovado;
  auto-modificação (human-a aprovada) segue intacta.
- Removido o case direto de `bash_exec` em `executeTool` (footgun latente).
- Defesa-em-profundidade: guard em `bashExecTool` (auto-mod) bloqueia
  `.env/.ssh/id_rsa/memory.db//proc/self/environ/base64 -d`.
- Teste de regressão `bash_allowlist_security_test.go`: fail-closed p/ comandos
  fora da lista e injeção via meta-char.

Exemplos de bypass da denylist ANTIGA que foram fechados: `cat /proc/self/environ`
(leitura de env), `base64 -d | bash` (encoding), `rm -r${IFS}-f /` (variável),
`X=rm; $X -rf /` (indireção), concatenação `"r""m -rf /"`, leitura `~/.ssh/id_rsa`
(`.ssh` não estava na denylist do bash, só na do read_file).

## 2. Chave Groq — VERIFICADO (sem chave exposta) ⚠️
- **Não existe `config.yaml`** no repo. `GROQ_KEY` já vem de env (`main.go:20`).
- Busca por `gsk_*` em arquivos e histórico git: **zero ocorrências**.
- Groq **ainda é usado** só para **ASR** (transcrição de áudio, `callGroqASR` em
  `smart_chat.go`). `callGroq` (texto/LLM) é **código morto** — não é chamado.
- Conclusão: nada a migrar/remover; chave já é gerenciada por `.env`.

## 3. /fs/exec duplicado — VERIFICADO (sem duplicação) ⚠️
- Grep completo: **1 definição** (`fs_routes.go:216 handleExec`) e **1 registro**
  (`main.go:127`).
- Handler único já é o protegido: `requireHokAuth` + restrição a `127.0.0.1/::1` +
  fluxo de aprovação (`registerFsExecPendingAction`). Nada a remover.

## 4. Credencial hardcoded (hokma-mobile-chat, artefato Replit) ✅
- Artefato `/root/hokma-web/artifacts/hokma-mobile-chat` contém token hardcoded
  em `src/hooks/use-chat.ts` e `src/pages/dashboard.tsx`.
- **Confirmado não-vivo**: o `HOK_TOKEN` atual (.env) é um valor de 64 chars,
  DIFERENTE do vazado. Não aparece no frontend deployado (`/var/www/hok-os`).
  Não-versionado, não-deployado.
- **Remediado**: texto puro removido dos 2 arquivos (substituído por placeholder
  "obter de env"). 0 ocorrências restantes em /root.
- **Nota:** por ser credencial já não-viva, não houve necessidade de rotacionar o
  `HOK_TOKEN` atual em produção (rotacioná-lo causaria outage sem ganho).

## Problemas encontrados no caminho
- `bash_exec` do agente estava rodando `bash -c` bruto SEM passar pela denylist
  (a denylist só valia para auto-modificação). O corte na allowlist fechou isso.
- Testes exigem `HOK_TOKEN` definido no processo (`init()` do main.go faz
  `log.Fatal` se ausente) — padrão já usado nos testes existentes.
- Settings do harness atualizadas para reduzir prompts de ações seguras (git
  commit, build/test/vet, kill/pkill de processo de teste, edição de arquivos no
  escopo do projeto), mantendo trava em push/deploy/rm/produção.

## Pendente de decisão
- Rotação forçada do `HOK_TOKEN` atual (não recomendada: token vivo é outro e a
  credencial vazada não está em uso). Decisão do dono.