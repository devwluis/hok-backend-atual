# oca — OpenCode Autonomous CLI wrapper

Wrapper CLI standalone que traz o **Modo Autônomo Total** do HOK OS para o
opencode puro. Funciona direto no terminal via Termius — sem depender do
backend `hokma.service`.

Implementa 4 mecanismos de segurança:

1. **Snapshot automático** — git tag + tar de `~/.config/opencode` + `~/.local/share/opencode`
2. **Budget** — contador de ações (default 50), bloqueia opencode quando esgota
3. **Circuit breaker** — detecta ações idênticas (3x em 10min), bloqueia opencode
4. **Rollback manual** — `oca rollback <id>` restaura tudo

Inspirado na mesma lógica do `autonomous.go` + `recovery.go`/`recovery.sh`
do backend HOK (28/08), mas **standalone** (sem DB, sem HTTP, sem frontend).

---

## Instalação

Binário em `/root/bin/opencode-autonomous/oca`, symlink em `/root/bin/oca`.

```bash
cd /root/bin/opencode-autonomous
go build -o oca .
ln -sf /root/bin/opencode-autonomous/oca /root/bin/oca
```

State vive em `/root/.local/share/opencode-autonomous/`:
- `state/current` — sessão ativa
- `state/history/<id>.json` — sessões encerradas
- `snapshots/<id>/` — config_opencode.tgz + data_opencode.tgz + META.json + git tag

---

## Uso

### `oca start` — inicia sessão autônoma

```bash
oca start .                                # sessão no cwd, mensagem default
oca start /path/to/repo                     # sessão em outro repo
oca start . -m "refactor the auth handler"  # mensagem inicial pro opencode
oca start . --budget 10 --auto-rollback     # 10 ações + rollback automático
oca start . --cb-max 5 --cb-window 15       # CB dispara após 5x em 15min
oca start . --notes "teste refactor X"      # nota descritiva
```

**O que acontece**:
1. Verifica se já tem sessão ativa (recusa se sim)
2. `git commit --allow-empty` + cria tag `snapshot/auto_<timestamp>`
3. Tar.gz de `~/.config/opencode` + `~/.local/share/opencode`
4. Gera `opencode.json` restritivo (25 bash deny + 10 edit deny)
5. Spawna `opencode run --auto --format json` com `XDG_CONFIG_HOME` apontando pro config restritivo
6. Lê stdout JSON linha-a-linha, cada `tool_use`:
   - decrementa budget (avisa quando restam 2)
   - calcula hash do input, manda pro CB
   - se budget=0 ou CB=3x → SIGTERM no opencode
7. Salva estado em `state/history/<id>.json`
8. Se `--auto-rollback` + bloqueio → rollback automático

### `oca rollback` — restaura snapshot

```bash
oca rollback auto_20260830_194436              # confirma com prompt
oca rollback auto_20260830_194436 --yes        # sem prompt
oca rollback auto_20260830_194436 --dry-run    # mostra o que faria
```

Restaura:
- Código do repo: `git reset --hard snapshot/<id>` + `git clean -fd`
- `~/.config/opencode`: remove + extract do tar.gz
- `~/.local/share/opencode`: remove + extract do tar.gz

### `oca status` — sessão ativa

```
id:        auto_20260830_194436
repo:      /tmp/oca-test
pid:       4182652
budget:    3/20
started:   2026-08-30T19:44:36Z
config:    /root/.local/share/opencode-autonomous/tmp/auto_20260830_194436/opencode.json
auto-rollback: OFF
```

### `oca list` — histórico

```
ID                        REPO                      AÇÕES      STATUS
auto_20260830_194436      /tmp/oca-test             3/20       completed
auto_20260830_194216      /tmp/oca-test             3/20       completed
auto_20260830_192256      /tmp/oca-test             3/5        completed
```

### `oca config` — debug da blocklist

Mostra o JSON do config restritivo + contagem de regras.

---

## Blocklist (default)

**Bash deny** (25 regras):
- `rm -rf /`, `rm -rf ~`, `rm -rf *`, `rm -rf .X`
- `sudo ...`
- `dd if=...`, `mkfs`, `mkfs.ext`, `fdisk`
- `curl ... | sh/bash`, `wget ... | sh/bash`
- `chmod -R 777`, `> /dev/sd`
- `shutdown`, `reboot`, `halt`, `poweroff`, `init 0`
- `systemctl stop/disable/mask hokma`, `hok-terminal`, `opencode-serve`
- `rm -rf /root/hokma`, `~/.opencode`, `~/.config/opencode`, `~/.local/share/opencode`

**Edit deny** (10 regras):
- `~/.ssh/**`, `~/.opencode/**`, `~/.config/opencode/**`, `~/.local/share/opencode/**`
- `/etc/passwd`, `/etc/shadow`, `/etc/sudoers`
- `~/.bashrc`, `~/.zshrc`, `~/.profile`

---

## Limitação conhecida

O opencode **não tem hook pré-execução** — quando o agente chama `bash`/`edit`, o tool já rodou. O wrapper só consegue:
1. Observar cada tool_use via JSON stream
2. Reagir DEPOIS (SIGTERM quando budget/CB dispara)
3. Bloquear ANTES via config restritivo (a blocklist deny rules que vem do opencode nativo)

É o mesmo modelo do HOK autônomo — limitação arquitetural, não do wrapper.

---

## Exemplo end-to-end (testado em `/tmp/oca-test`)

```bash
$ cd /tmp/oca-test
$ oca start . -m "run 'echo CB_PROBE' three SEPARATE times" --budget 20 --cb-max 3
[oca] criando snapshot em /tmp/oca-test...
[oca] ✓ snapshot criado: auto_20260830_194436 (tag=snapshot/auto_20260830_194436, ...)
[oca] ✓ config restritivo: ... (25 bash deny + 10 edit deny)
[oca] iniciando opencode (model=, budget=20)...
[oca] opencode iniciado: pid=4182652
[oca]   ação #1: bash({"command":"echo CB_PROBE"}) (restam 19)
[oca]   ação #2: bash({"command":"echo CB_PROBE"}) (restam 18)
[oca] ⛔ CIRCUIT BREAKER: bash({"command":"echo CB_PROBE"}) repetiu 3x em 10min — encerrando opencode
[oca] opencode saiu: exit=-1
[oca] ✓ sessão encerrada: id=auto_20260830_194436, ações=3, status=circuit_breaker

$ oca rollback auto_20260830_194436
isso vai SOBRESCREVER código em git e restaurar ~/.config/opencode + ~/.local/share/opencode.
tem certeza? digite 'sim' pra continuar: sim
[oca] === restore snapshot auto_20260830_194436 ===
[oca] ✓ código restaurado: git reset --hard snapshot/auto_20260830_194436
[oca] ✓ /root/.config/opencode restaurado
[oca] ✓ /root/.local/share/opencode restaurado
[oca] === restore completo ===
```

---

## Item 6 — Persistência de estado (resume após queda)

**O estado completo da sessão é salvo em disco a cada tool_use** (`state/current`, atomic rename).
Se o terminal cai, o contexto da conversa reinicia, ou o Termius perde conexão —
o estado (budget, ações, CB events, snapshot) **persiste**. Basta rodar `oca resume`.

### Schema de `state/current`

```json
{
  "id": "auto_20260830_220000",
  "mode": "run",
  "repo_path": "/home/user/projeto",
  "config_path": "/root/.local/share/opencode-autonomous/tmp/<id>/opencode.json",
  "config_dir": "/root/.local/share/opencode-autonomous/tmp/<id>",
  "budget": 50,
  "actions_used": 12,
  "started_at": "2026-08-30T22:00:00Z",
  "pid": 17024,
  "process_start_pid": 17024,
  "auto_rollback": false,
  "cb_window_mins": 10,
  "cb_max_repeat": 3,
  "blocked_reason": "",
  "actions": [
    {"n": 1, "tool": "bash", "hash": "abc...", "summary": "bash(ls)", "ts": "2026-08-30T22:00:05Z"},
    {"n": 2, "tool": "bash", "hash": "def...", "summary": "bash(cat README.md)", "ts": "2026-08-30T22:00:08Z"}
  ],
  "cb_events": [
    {"hash": "abc...", "summary": "bash(ls)", "at": "2026-08-30T22:00:05Z"}
  ],
  "updated_at": "2026-08-30T22:00:08Z",
  "last_heartbeat": "2026-08-30T22:00:08Z",
  "resume_count": 0,
  "opencode_session_id": "ses_abc123",
  "last_seen_part_ts": 1788115182914
}
```

### Comandos novos

| Comando | O que faz |
|---|---|
| `oca resume [path]` | Detecta sessão órfã (PID morto + heartbeat antigo > 60s). Mostra ações já feitas, CB events, status. Reanexar com `--yes`. |
| `oca resume --budget N` | Adiciona N ações extras ao budget (default = restante do state) |
| `oca abort-stale [--yes]` | Limpa `state/current` se órfão. **NÃO deleta snapshot** (rollback continua possível). |
| `oca checkpoint [path] [--notes "..."]` | Cria snapshot manual sem iniciar sessão. Útil antes de mudanças arriscadas. |
| `oca start --force` | Ignora `state/current` órfão e cria snapshot novo. |

### Fluxo: Termius caiu no meio da sessão

```bash
# === ANTES da queda (em outro terminal/Termius) ===
$ oca start /home/user/projeto --budget 50
[snapshot criado: auto_20260830_220000]
[ação #1: bash(ls)]                    [state/current atualizado]
[ação #2: bash(cat README.md)]         [state/current atualizado]
... (12 ações) ...
# ⚡ Termius cai aqui — opencode --continue fica orfão

# === DEPOIS — usuário reabre Termius ===
$ oca start /home/user/projeto
⚠ sessão anterior órfã detectada: auto_20260830_220000
  ações usadas: 12 / 50
  CB events: 8 (janela 10min)
  last heartbeat: 2026-08-30T22:05:30Z
  para retomar:    oca resume
  para limpar:     oca abort-stale --yes
  para ignorar:    oca start --force (cria snapshot novo, descarta state)
ERRO: sessão órfã (use --force ou oca resume)

$ oca resume --yes
sessão órfã detectada:
  id: auto_20260830_220000
  ações usadas: 12 / 50
  CB events: 8 (janela 10min)
  started: 2026-08-30T22:00:00Z
  last heartbeat: 2026-08-30T22:05:30Z
  última ação: #12 bash(grep pattern) (22:05:30)
  opencode session id: ses_abc123
✓ sessão retomada (resume #1, budget=50)

# (em seguida, 'oca start' detectaria state existente e poderia retomar o opencode --continue)
```

### Detecção de órfão (algoritmo)

```
stale = PID morto (kill -0 falha)
      OU heartbeat antigo (> StateStaleThreshold = 60s)
```

Se `stale=true`, `oca start` recusa com mensagem instrutiva.
`oca status` mostra `(STALE — resume disponível)` quando aplicável.

### O que **NÃO** persiste

- **Conteúdo do tmux** (processo morre, sessão tmux sobrevive mas estado do shell não)
- **Sessão opencode em si** (opencode --continue fica órfão se o wrapper morreu; resume pode reabrir mas opencode pode criar session nova)
- **STDIN do opencode** (digitação em andamento é perdida)

O **budget, ações, CB events e snapshot** — que é o que importa para segurança — **persiste 100%**.

---

## Testes

```bash
cd /root/bin/opencode-autonomous
go test ./...              # TestBudgetTrackerBasic + TestCircuitBreakerBasic + TestSnapshotCreateRestore
```

### Evidência de testes reais (30/08/2026)

| Cenário | Comando | Resultado |
|---|---|---|
| Snapshot + 1 ação | `oca start . -m "execute 'ls'" --budget 5` | ✓ snapshot criado, 1 ação interceptada, exit=0 |
| 4 ações distintas | `oca start . -m "...ls, cat, cat, echo..." --budget 10` | ✓ 4 ações, CB não dispara (hashes diferentes) |
| Circuit breaker | `oca start . -m "ls três vezes como bash tool calls separados" --cb-max 3` | ✓ 3ª `ls` → `⛔ CIRCUIT BREAKER` → opencode morto |
| Budget esgotado | `oca start . -m "5 comandos separados" --budget 2` | ✓ avisa "1 restante" → 2ª ação → `⛔ BUDGET ESGOTADO` → morto |
| Rollback real | `oca rollback auto_20260830_214439 --yes` | ✓ `git reset --hard` + tar restores (tag preservada) |
| Dry-run | `oca rollback <id> --dry-run` | ✓ mostra plano sem modificar nada |
| Status | `oca status` (sem sessão ativa) | ✓ "nenhuma sessão ativa" |
| List | `oca list` | ✓ 14 sessões no histórico (completed/cb/budget) |
| Config | `oca config` | ✓ 26 bash deny + 11 edit deny |

Process group isolation: `Setpgid=true` → `pid=pgid` → `kill -<pid>` mata o grupo inteiro (opencode + filhos), encerrando em <2s com `--timeout 90`.

---

## Arquivos

```
/root/bin/opencode-autonomous/
├── go.mod, go.sum
├── main.go          # cobra CLI: start/rollback/status/list/config
├── config.go        # RestrictiveConfig + blocklist (25 bash + 10 edit deny)
├── snapshot.go      # CreateSnapshot + RestoreSnapshot (git + tar)
├── budget.go        # BudgetTracker (consume com warnAt)
├── circuit.go       # CircuitBreaker (sliding window por hash)
├── intercept.go     # RunOpenCodeWithIntercept (--format json stream)
├── state.go         # SessionState + state/current + history/
├── utils.go         # JSON helpers + log
├── oca              # binário compilado (4.4 MB)
├── wrapper_test.go  # tests BudgetTracker + CircuitBreaker
└── snapshot_test.go # test snapshot create+restore end-to-end
```