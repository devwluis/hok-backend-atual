# Adendo — Sessão 28/08 14:40 · Deploy do gate plan do Hermes (isolamento físico) + smoke real

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_20260828_gate_plan_hermes_isolamento.md (implementação + E2E isolado).

---

## Contexto

Aprovação do deploy do gate de modo plan do Hermes (isolamento físico via
Docker), após revisão do diff completo do `hermes_client.go` por Washington.

## Diff revisado (git diff hermes_client.go)

Três funções novas revisadas por inteiro:
- `callHermesWithMode` — plan → `docker run` efêmero isolado; normal → `docker
  exec` com `--yolo` (comportamento preservado fora do plan)
- `hermesDataVolume` — resolve o volume `/opt/data` via `docker inspect`
  (campo `Name`, com fallback de extração do path `.../volumes/<name>/_data`),
  cache protegido por mutex, **sem hardcode**
- `hermesIsolatedArgs` — `--read-only` + `--tmpfs` + volume ro + `--rm` +
  prompt em base64; `--mount type=volume,src=<nome resolvido>` (o bug do exit
  125 — path vs nome — foi o motivo do campo Name)

(+ `callHermesArgs`, `hermesVerifyOutput`/`backtickPathRe` da rodada anterior.)

## Deploy (padrão das sessões)

- Backups: `hokma.bak_predep_hermesplan_*`, `.env.bak_predep_hermesplan_*`,
  `memory.db.bak_predep_hermesplan_*` (+ os `.bak_*_isolate`/`.bak_*_plangate` das
  rodadas anteriores)
- `go build -o hokma_test .` → parar → substituir → iniciar → hokma ativo, 8082 OK
- Nada mais tocado; opencode-serve intacto

## Smoke test REAL em produção (porta 8082)

Mensagem: `forceHermes:true, mode:"plan"` + "Crie o arquivo
plan-hermes-prod.txt com o conteudo X e confirme":

| Verificação | Resultado |
|---|---|
| `mode` da resposta | `hermes_plan` (gate ativo) |
| Reply do hermes | "sistema o identificou como arquivo protegido... não tenho permissão" — bloqueio real |
| Arquivo no host (`/opt/data/plan-hermes-prod.txt`) | não existe |
| Arquivo no container original (`docker exec hermes-gateway`) | não existe |
| `find /` (host) | nada |
| Panics | 0 · hokma + opencode-serve ativos |

**Pendência FECHADA**: o gate de modo plan do Hermes está hermético em
produção, no mesmo nível dos outros 3 agentes (Claude Code, OpenCode CLI,
OpenCode serve). Nada foi pusheado (aguardando aprovação de commit/push).