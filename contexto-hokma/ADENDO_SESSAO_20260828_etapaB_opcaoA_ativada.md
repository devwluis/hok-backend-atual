# Adendo — Sessão 28/08 12:15 · Etapa B opção A ATIVADA: card de aprovação em produção

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_20260827_card_aprovacao.md, ADENDO_SESSAO_20260827_etapaB_permissions_nativas.md.

---

## Contexto

Ativação da config `ask` (Etapa B, opção A) — o card de aprovação passa a valer
no uso real: leitura/inspeção segue automática (once), escrita/execução sensível
pede aprovação no chat, blocklist continua bloqueando antes do serve.

## DESCOBERTA CRÍTICA — onde a config realmente mora

O binário do opencode foi atualizado automaticamente para **1.18.25**
(autoupdate em backend/opencode.json) e passou a priorizar o config do
diretório **`/root/hokma/backend/.opencode/opencode.json`** (com `bash: allow /
edit: allow`) — que VENCIA a config global (`~/.config/opencode/opencode.json`)
e o `opencode.json` do projeto. A primeira tentativa de ativação (editar a
global) NÃO surtiu efeito; o achado veio do log de debug do opencode
(loading paths: `.config/opencode/config.json`, `.config/opencode/opencode.json`,
`.config/opencode/opencode.jsonc`, `backend/.opencode/opencode.json`,
`backend/.opencode/opencode.jsonc`, `~/.opencode/opencode.json`,
`~/.opencode/opencode.jsonc`).

## O que foi feito

1. **`.opencode/opencode.json`** do projeto: `bash: ask, edit: ask, webfetch:
   allow` (backup `.bak_..._preask`). `opencode.json` do projeto restaurado
   após um teste de formato (backup `_permtst` preservado).
2. **Restart do opencode-serve** → config efetiva confirmada via `/config`:
   `{"edit":"ask","bash":"ask","external_directory":{"~/secrets/**":"deny",
   "*":"allow"},"webfetch":"allow"}`.
3. **Smoke em produção 4/4**:
   | Teste | Resultado |
   |---|---|
   | `echo` (leitura) | once automático (AUDIT no journal) → executou |
   | `mkdir` (escrita) | **CARD** `opencode_serve_pending` → "sim" → executou (dir criado) |
   | `rm -rf /` | bloqueado (opencode_serve_blocked) |
   | Panics | 0 |

## Efeito no uso diário

- Leitura/inspeção (`echo`, `ls`, `cat`, `pwd`, `grep`, `ps`...): **automática**
  (once, sem card) — fluxo fluido preservado.
- Escrita/execução sensível (`mkdir`, editar arquivo, `git commit`, instalar,
  `systemctl`...): **card de aprovação no chat** (Aprovar/Rejeitar); TTL 120s →
  reject automático; sessão zumbi pós-reject → recriação automática (pendência 1).
- Encadeamento da mesma execução: 1ª aprovada → subsequentes aprovam junto
  (recent-approve 90s).
- n8n/automações internas: **não afetadas** (não usam o serve).

## Rollback

Restaurar `backend/.opencode/opencode.json` para `allow` (backup `.bak_..._preask`)
+ `systemctl restart opencode-serve` (~10s) — comportamento anterior completo.

## Status

Etapa B (opção A) **ativa em produção**. Card de aprovação funcionando no fluxo
real (async/jobs + retomada). Backups: `.opencode/opencode.json.bak_..._preask`,
`opencode.json.bak_..._permtst`, e os backups de rotina do deploy anterior.