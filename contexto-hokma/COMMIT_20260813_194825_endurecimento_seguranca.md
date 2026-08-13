# COMMIT — Endurecimento de segurança (MCP n8n, segredos) + limpeza de lixo + ferramentas LLM

Data: 2026-08-13 19:48 · Por: Washington + Claude Code (sessão local, no servidor)
Escopo: backend Go (`/root/hokma/backend`) + config do Claude Code (`~/.claude`) + repo de contexto (`/root`)

---

## 1. MCP n8n: guarda anti-XXE em metadata de workflow

### Contexto e motivação
O payload de workflow n8n (nodes[*].parameters — metadata livre gerada pelo
LLM/atacante) é encaminhado ao servidor MCP (`validate_workflow`) e à API REST
do n8n. Strings desse payload podem embutir XML malicioso. Três riscos cobertos:

1. **XXE no downstream**: se o MCP server / n8n core parsear esse XML com parser
   vulnerável, `<!DOCTYPE>` / `<!ENTITY ... SYSTEM|PUBLIC>` pode ler arquivos
   locais, fazer SSRF ou DoS ("billion laughs"). → Bloqueio na origem.
2. **Reflexão para o LLM**: XML malicioso em metadata, ecoado de volta no
   relatório de validação, pode carregar prompt injection. → Bloqueio na origem
   impede que chegue ao relatório.
3. **Nodes/params XML inseguros**: entidades externas bloqueadas independente do
   tipo do node.

### Implementação
- **Novo `n8n_xml_guard.go`**: `guardWorkflowXML(payload)` (enforcer único) varre
  `name`/`type`/`parameters` (recursivo) procurando `<!DOCTYPE>`/`<!ENTITY>` com
  `SYSTEM`/`PUBLIC`/entity param. Reentrante, não muta o payload. Reporta findings
  legíveis por node/campo/caminho.
- **Gatilhos (3 pontos de fronteira)**: `n8nCreateWorkflow`, `n8nUpdateWorkflow`,
  `n8nValidateWorkflowViaMCP` — bloqueiam (return errJSON / veredito inválido)
  antes de qualquer envio ao MCP/REST.
- **Testes `n8n_xml_guard_test.go`** + `mcp_n8n_validate_test.go` (veredito do
  MCP): payload limpo, DOCTYPE em `jsCode`, DOCTYPE em `name`, billion laughs
  aninhado, `SYSTEM` em array de `options`, XML normal sem DOCTYPE passa.

### Commit
**`5389be2`** (`security: guarda anti-XXE...`) — 5 arquivos no branch `main` local.
> Incluiu junto o diff MCP pendente já existente no working tree (parseMCPWorkflowVerdict,
> decodeVerdict, n8nHostURL, status HTTP, reparo de nodes de roteamento) — não dava
> para separar por arquivo. Push aguardando confirmação.

**Validação**: `go build` ✗ isolado OK (`hokma_test`), `go vet` OK, testes da guarda
+ `TestParseMCPWorkflowVerdict` PASS (com `HOK_TOKEN=teste` — o main.go exige a env
para o lock de sessão ao rodar testes).

## 2. Ponto de atenção: `HOK_TOKEN` e lock de sessão
`main.go:50` faz `log.Fatal` se `HOK_TOKEN` não estiver definida → `go test` sem a env
falha (pré-existente, não relacionado a esta sessão). Em produção vem do systemd.
Não alterado.

## 3. Limpeza de segredos no config do Claude Code
- **JWT de acesso ao n8n removido do `~/.claude/settings.local.json`** (6 linhas de
  `curl` na allowlist com `X-N8n-Api-Key`/`Authorization: Bearer`). Backup temporário
  que continha o token também deletado. JSON validado; 15 itens da allowlist preservados.
  ⚠️ **Recomendado rotacionar o token no n8n** por exposição prévia em texto plano.
- `sk-` em `config.json`/`settings.json` (OpenRouter/DeepSeek) mantidos — são a API
  key operacional necessária.

## 4. Limpeza de lixo na raiz `/root`
Removidos (após aprovação do usuário): testes/junk de modelo, exports de workflow
(`all_wf.json`, `workflows_backup.json`), ~30 scripts um-off (`fix_*`, `patch_*`,
`deploy*`, `conserta_tudo.sh`, `add_getCRMModel.py`), 2 dirs de backup de sessão
(`n8nspecialist_fix_backup_*`, `session4_fix_backup_*`).
**Mantidos** por escolha: `update_agent.py`, `diag_tokens.py`, backups/segredos/dirs
de código e n8n.

## 5. Modelo LLM consolidado em DeepSeek v4 flash
Removido o **seletor multi-LLM**: scripts `usar-claude`, `usar-openrouter`,
`usar-minimax`, `perguntar-minimax` + template `settings.json.open.template`.
Decisão do usuário: usa somente **Hok orquestrando Hermes + Claude Code** com
**DeepSeek v4 flash**. Stack nuvem via OpenRouter (2 clouds), distância da opção
"modelo local" avaliada.

### Hardware vs modelo local
2 núcleos, 7.8 GiB RAM (só ~2.2 disponível), sem GPU → **modelo local inviável**;
recomendado manter nuvem + mitigar (sem colar segredos no chat, telemetria
não-essencial já desligada `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, não colar
dados de produção). Se privacidade virar requisito real → servidor dedicado com GPU.

## 6. Pendências em aberto
- 🔄 Rotacionar token do n8n (exposto previamente no settings do Claude Code).
- 📤 Push do repo de contexto (`clean_master`, commit `371910005`) — aguarda `gh auth` / push manual.
- 📤 Push do backend (`5389be2` → `origin/main`) — aguarda confirmação.
- Segurança SaaS da varredura 12/08 segue pendente (8082 exposta sem auth, OwnerGate
  client-side, CORS `*`, timeout em ExecuteApprovedCommand).

---

*Gerado a partir de sessão real no servidor (13/08/2026). Complementa HOK_MASTER_CONTEXT.md
e os COMMIT_* anteriores da pasta.*