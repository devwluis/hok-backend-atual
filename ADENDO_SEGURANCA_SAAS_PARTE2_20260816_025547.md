# Adendo de Segurança SaaS — Parte 2 — 2026-08-16

Fechamento das últimas duas pendências de segurança do SaaS Hokma. CRM fora de
escopo. `auth_routes.go` não tocado (sessão paralela/open-code). Complementa o
Adendo Parte 1 (2026-08-15).

## 1. /terminal e /shell — DENYLIST ENDURECIDA (Opção AH) ✅
**Problema:** `handleTerminal` (routes.go) rodava `bash -c` bruto com denylist
fraca (`rm -rf /`, `mkfs`, `dd if=`, `format`, fork-bomb) — mesma classe de
bypass mapeada no bash_exec do agente (encoding, concatenação, variável, case,
caminhos alternativos).

**Quem usa:** humano **dono/admin autenticado** — `requireHokAuth` exige
`X-Hok-Token == HOK_TOKEN` ou JWT role `owner/admin`. O agente NÃO chama
`/terminal`. Portanto, modelo de ameaça = dono autenticado, não LLM → não exigiu
allowlist estrita (A'), e sim defesa-em-profundidade (AH).

**Fix implementado (commit `651cbf7`):**
- `terminalCommandBlocked()` normaliza uma COPIA do comando (lowercase, remove
  aspas/backtick, `${var}/$var/$(...)/$IFS`, colapsa espaços — quebra
  concatenação e encoding) e checa contra blocklist estendida:
  `/proc/self/environ`, `.env`, `.ssh`, `id_rsa`, `.pem`, `memory.db`,
  `base64`, `eval`/`exec`, pipas `| bash/sh/zsh`, `chmod -R 777`, `chown -R root`,
  `chattr`, `/dev/sd`, `dd if=`, `rm -rf /`.
- Comando ORIGINAL segue executado; terminal continua livre para o dono.
- Regra ampla `format` removida (falso-positivo: bloqueava `git log --format`).
- Teste `terminal_blocklist_security_test.go`: bypass-block + legit-allow.
- `go vet`/build/suite OK.

**O que se perde:** quase nada — comandos legítimos do dono seguem (pipas/curl
para download permitidos; só `curl|bash`/pipe-para-shell bloqueado).

## 2. Resíduo de token no dist compilado ✅
**Problema:** o bundle compilado
`/root/hokma-web/artifacts/hokma-mobile-chat/dist/public/assets/index-R4B6iQEW.js`
continha o token hardcoded antigo do mobile-chat (mesmo sanado nos sources na
Parte 1, o BUILD tinha o valor gravado).

**Confirmações:**
- Artefato (`hokma-mobile-chat`) NÃO é servido em produção — nginx serve apenas
  `/var/www/hok-os` (raiz de assets). Não-versionado, não-deployado.
- Sem necessidade de manter o dist (sources já saneados, regenerável).

**Ação:** removida a pasta `dist/` do artefato morto (build antigo com token).
Varredura server-wide pós-remoção:
- **`/root` (exceto `.git`): 0 ocorrências** do token.
- **`/var/www` (produção): 0 ocorrências**.
- Sources + builds + dists + node_modules varridos. Token **totalmente purgado**.
- Sem mudança no repo git do backend (o dist era de diretório fora do git).

## Problemas encontrados
- O token vazado estava DUPLICADO entre sources (Parte 1) e build (Parte 2) —
  sanear source sozinho não bastava; o bundle compilado precisou ser removido.
- `handleTerminal` tinha 2 registros de rota (`/terminal`+`/shell`) para o MESMO
  handler — não era duplicação de segurança, mas config confusa que agora está
  documentada.

## Pendente
- Commits locais da Parte 2: `651cbf7` (terminal) + este adendo. **Sem push** —
  envio para `hok-backend-atual` apenas com aprovação explícita do dono.