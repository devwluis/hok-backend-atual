# Adendo — Proposta do MODO AUTÔNOMO TOTAL (snapshot + rollback + isolamento)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_PROPOSTA_MODO_AUTONOMO_20260828.md e
ADENDO_SESSAO_20260828_modo_autonomo_implementacao.md (autônomo atual).

---

## 1. Levantamento do estado atual de recovery (comandos reais — não suposição)

| Mecanismo | Existe hoje? | Estado real |
|---|---|---|
| git (histórico) | Sim | Branch única `main` + remoto `origin/hok-backend-atual`. **Sem tags, sem stash.** Último commit: `92dd160` (28/08). O modo autônomo (autonomous.go, session_mode.go, gates...) está NO working tree, **não commitado** |
| Backups de binário | Sim | `hokma.bak_*` (ex.: `hokma.bak_pre_autonomous_*`, `hokma.bak_predep_hermesplan_*`) — manuais, por deploy |
| Backups de banco | Sim | `memory.db.bak_*` (ex.: `memory.db.bak_predep_*`, `.bak_pre_teste_fumaca_*`) — manuais, antes de mudanças pontuais |
| Backups de código | Sim | **235 arquivos** `.bak_*` no total (`.go.bak_<ts>` por arquivo, `.env.bak_*`) — manuais, por edição |
| Banco versionado | **Não** | `memory.db` (6.1 MB) está no `.gitignore` (`*.db`) — nunca foi pro git |
| .env versionado | Não | `.gitignore` (`*.env.*`) — nunca foi pro git |
| Volume do hermes | Não | `hermes-gateway` monta o volume anônimo `c9b33b8c…` → `/opt/data` + bind do host `/root/.hermes`. **NÃO versionados** — e o hermes AUTÔNOMO monta `/opt/data` read-write (pode mutar auth.json, config.yaml, SOUL.md, leads.db...) |
| n8n workflows | Não | Banco próprio (`n8n_data`/`n8n_data_v2`) — o agente n8n pode criar/alterar workflows |
| Restore/rollback automatizado | **Não existe** | Nenhum comando de rollback, nenhum script de recovery, nenhum watchdog |
| systemd | Parcial | `hokma.service` com `Restart=on-failure` — reinicia sozinho se cair, mas não desfaz mudanças |

**Conclusão**: hoje o recovery é manual e por arquivo (235 backups sem ponto de
restauração unificado). Para "tarefa grande autônoma" não existe rede de
segurança de desfazer — só de parar (budget/circuit breaker).

## 2. Snapshot automático antes de tarefa autônoma grande

### 2a. Estado do código (git)
- **Commit de checkpoint** do working tree inteiro + **tag com timestamp**
  (`snapshot/autonomous_<ts>`), antes de autorizar a tarefa. Não usar stash
  (frágil com mudanças parciais); um commit de checkpoint é idempotente e
  identificável.
- O checkpoint fica na branch atual (main) ou numa branch dedicada
  `checkpoint/…` — recomendação: **branch dedicada** para o snapshot não
  poluir o histórico de features, com merge trivial no rollback.

### 2b. Estado do banco (memory.db)
- **Cópia consistente via `sqlite3 .backup`** (não `cp` — captura WAL/shm
  corretamente) para `snapshots/<checkpoint_id>/memory.db`.
- Restauração idêntica ao backup (tabela autonomous_audit/session_mode
  incluídas — o rollback restaura o estado do modo também).

### 2c. Escopo — o que a tarefa autônoma PODE mutar (cobertura mínima)
1. `/root/hokma/backend` (código — git) ✓
2. `memory.db` ✓
3. Volume do hermes `c9b33b8c…` → `/opt/data` + bind `/root/.hermes`
   (tar via container: `docker run --rm -v <vol>:/d -v <dest>:/b alpine
   tar czf /b/optdata.tgz /d`) — **o hermes autônomo escreve nesse volume**
4. `.env` (cópia simples — não vai pro git)
5. **n8n workflows** (export via API do n8n) — OPCIONAL; só se o escopo da
   tarefa incluir o agente n8n (fora dos 4 agentes do gate)

**Fora do escopo**: postgres (CRM), redis, traefik/portainer — nenhum agente
toca esses (blocklist + ninguém tem acesso); não vale o custo.

### 2d. Quando disparar — flag explícita, NÃO mudar o autônomo atual
Recomendação: **novo modo `autonomous_total`** (3º botão não — um flag no
POST /session/mode: `mode:"autonomous_total"`), separado do autônomo atual:
- O autônomo atual (budget 5, tarefas pontuais) fica como está.
- `autonomous_total`: **sem budget de chamadas** (ou budget alto tipo 50 —
  "tarefa grande"), **com snapshot automático no início** e rollback
  disponível. Mais conservador: exige flag explícita (o usuário decide
  conscientemente), nunca é o default.

## 3. Mecanismo de rollback

### 3a. Automático vs manual — análise de riscos
| Abordagem | Prós | Contras |
|---|---|---|
| **Automático (CB → reverte sozinho)** | Ninguém precisa estar disponível; volta ao estável sempre | **Mascara o erro** (perde o estado do bug — o log do hermes some com o volume revertido); pode reverter NO MEIO de uma mudança válida (o CB disparou por 3 erros em uma sub-ação, não pela tarefa toda); o agente pode re-executar e re-quebrar em loop |
| **Manual (CB → para + avisa + comando pronto)** | Controle total; o usuário decide SE e QUANDO; dá pra inspecionar o que foi feito antes de reverter; o erro fica documentado | Exige o usuário disponível |

**Recomendação**: **manual por padrão** (o CB atual já para; o rollback é 1
comando), com **flag `auto_rollback:true` no POST /session/mode** para quem
quiser o automático (com aviso no log). O automático só desfaz código+banco
via snapshot; o que aconteceu ANTES do CB fica auditado na autonomous_audit.

### 3b. Comando de recovery simples
- Frase no chat: "volte pro checkpoint antes da tarefa X" → o backend lista
  os checkpoints (`snapshots/`) → restaura o escolhido:
  - Código: `git reset --hard <tag>` (+ limpar untracked do snapshot)
  - Banco: `sqlite3 memory.db ".restore 'snapshots/<id>/memory.db'"`
  - Volume hermes: `docker run --rm -v <vol>:/d -v <snapshot>:/b alpine
    sh -c 'rm -rf /d/* && tar xzf /b/optdata.tgz -C /d'`
  - `.env`: `cp snapshots/<id>/.env .env`
- Endpoint dedicado: `POST /recovery/rollback {checkpoint_id}` (X-Hok-Token).

### 3c. Camada de segurança FORA do agente (se o serviço cair)
- **Script standalone `/root/hokma/recovery.sh <checkpoint_id>`** — bash puro
  (git + sqlite3 + docker), NÃO depende do binário hokma rodando nem do Go.
  Funciona mesmo com o serviço caído: para o sistema, restaura, reinicia.
- Watchdog: `systemd timer` diário que verifica `git status`/integridade e
  registra; o `Restart=on-failure` do serviço já cobre queda do processo.

## 4. Nível de isolamento para tarefas grandes — branch vs clone

| Abordagem | Protege código | Protege banco/volume | Custo | Risco residual |
|---|---|---|---|---|
| **Branch git de trabalho** (`task/<nome>`) | Sim — o agente trabalha numa branch; o merge pro main só após revisão | Não (o agente roda com o mesmo banco/volume) | Baixo (git nativo, zero infra) | Banco/volume continuam expostos durante a tarefa |
| **Clone do ambiente em container** | Sim | Sim (banco/volume clonados no container) | Alto (replicar hermes + credenciais + n8n + …) | Config diverge do real; integração é manual |
| **Snapshot+rollback direto na produção** (item 2/3) | Parcial (depende do rollback ser rodado) | Parcial (idem) | Baixo | Janela entre o dano e o rollback |

**Recomendação (em camadas)**: usar os 3 juntos conforme a tarefa:
1. **Branch `task/…`** para QUALQUER tarefa de código (o agente commita na
   branch; o dano nunca chega no main sem review) — custo ~zero;
2. **Snapshot automático** (código+banco+volume) no início do
   `autonomous_total`;
3. **Rollback manual** (1 comando) + script standalone como última camada.
O clone em container fica descartado por ora (custo alto, benefício marginal
dado o snapshot+rollback).

## 5. Recomendação final (resumo)

- **Novo modo `autonomous_total`** (flag explícita; snapshot automático;
  sem budget por chamada ou budget alto; rollback disponível).
- **Snapshot**: commit+tag git em branch `checkpoint/…`, `sqlite3 .backup`
  do memory.db, tar do volume hermes + /root/.hermes, cópia do .env
  (n8n opcional).
- **Rollback manual por padrão** (o CB para + avisa + comando pronto),
  flag `auto_rollback` opcional; **script standalone recovery.sh** como
  camada fora do agente.
- **Branch `task/…`** para código de tarefas grandes (isolamento nativo);
  clone em container descartado.

**Nada implementado — proposta para revisão. Decisões para Washington:**
1. Nome/forma de ativar: `mode:"autonomous_total"` no POST /session/mode?
2. Budget do total: sem limite de chamadas ou alto (ex: 50)?
3. Rollback: manual por padrão + flag auto_rollback — ok?
4. Escopo do snapshot: incluir o volume hermes (/opt/data + /root/.hermes)?
   Incluir n8n (export de workflows)?
5. Branch de trabalho `task/…` para código — adotar sempre?