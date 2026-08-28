# Adendo — Sessão 28/08 14:30 · GATE PLAN REFORÇADO do Hermes (isolamento físico Docker)

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_20260828_gate_plan_3_agentes.md (gate parcial do hermes).

---

## Objetivo

Reforçar o gate de modo Plan do Hermes: o hermes ainda conseguia executar
dentro do próprio container em plan (escrevendo em /opt/data numa rodada de
teste). A proteção anterior (hermesVerifyOutput) só DETECTAVA depois — não
IMPEDIA antes. Pedido: flag nativa OU isolamento físico via Docker, no mesmo
nível hermético dos outros 3 agentes.

## 1. Investigação — por que o hermes escreveu sem --yolo + flag nativa?

- **`hermes --help`** (no container): opções relevantes — `-t/--toolsets`
  (habilita toolsets), `--safe-mode` (só troubleshooting: desativa config/
  plugins/MCP — NÃO desabilita ferramentas de escrita), `--yolo`
  (auto-aprova). **NÃO existe flag nativa de plan/dry-run/read-only** no
  hermes-agent (diferente do `--permission-mode plan` do Claude Code e do
  `--agent plan` do OpenCode).
- **Código** (`/opt/hermes/toolsets.py`): `_HERMES_CORE_TOOLS` (base sempre
  presente) inclui `write_file`, `patch`, `terminal`, `process`,
  `execute_code` — sem --yolo o hermes ainda pode tentar escrever; a
  restrição que existe é o "File-mutation verifier" DELE (pós-fato) e o
  HERMES_WRITE_SAFE_ROOT=/opt/data. Existe `disabled_toolsets` no config,
  mas é config (não confiável como gate).
- **Conclusão**: sem flag nativa → **isolamento físico via Docker** (mais
  forte que qualquer flag).

## 2. Abordagem implementada — isolamento físico (hermes_client.go)

Em modo plan, em vez de `docker exec hermes-gateway`, o backend roda um
**container efêmero isolado**:

```
docker run --rm --read-only \
  --tmpfs /tmp --tmpfs /run --tmpfs /opt/data \
  --mount type=volume,src=<volume do /opt/data>,dst=/opt/data-ro,readonly \
  --entrypoint /bin/sh nousresearch/hermes-agent \
  -c "echo <prompt-b64> | base64 -d > /tmp/p.txt; \
      cp -a /opt/data-ro/. /opt/data/ 2>/dev/null; \
      /opt/hermes/bin/hermes -z \"$(cat /tmp/p.txt)\" -m <model> --provider openrouter"
```

- **`--read-only`**: rootfs read-only (o hermes não escreve em /etc, /opt...)
- **volume `/opt/data` montado read-only**: config/auth para LEITURA; escrita
  negada no volume real
- **`--tmpfs /opt/data`**: o home de trabalho em tmpfs DESCARTÁVEL — qualquer
  "escrita" do hermes some com o `--rm`
- **`--rm`**: container removido ao final — fisicamente impossível persistir
  algo no host ou em qualquer volume
- prompt em **base64** (seguro para o shell `-c`); s6-overlay pulado via
  `--entrypoint` (o preinit dele falha em read-only)
- volume resolvido em runtime (`docker inspect hermes-gateway` → campo
  **`Name`** do mount — `--mount type=volume` exige o NOME, não o path do
  Source; bug corrigido: a 1ª versão usava o Source → exit 125)

Funções novas/alteradas: `callHermesWithMode` (plan → isolado), 
`hermesIsolatedArgs` (monta o docker run), `hermesDataVolume` (resolve o
volume, cacheado). `hermesVerifyOutput` mantido como camada de auditoria
(defesa em profundidade).

## 3. Testes

- **E2E isolado (porta 8099)**:
  | Cenário | Resultado |
  |---|---|
  | Hermes + `mode:"plan"` + "crie o arquivo plan-hermes-persist2.txt" | mode `hermes_plan`; hermes respondeu que o sistema bloqueou; **nenhum arquivo criado** (nem host, nem container original) |
  | Idem com nome "permitido" (plano-hermes-tmpfs.txt) via docker run manual | write negado (proteção dele + rootfs ro); **nada persistiu** em /opt/data do host, no container original, nem em qualquer path do host (find) |
- **Suíte Go**: `go build` + `go vet` limpos; `go test` completo ok (24.7s);
  testes do gate (plan_gate_test.go) continuam passando.
- **Produção**: NADA deployado/reiniciado/pusheado; hokma + opencode-serve
  ativos. Backups: `hermes_client.go.bak_<ts>_isolate` (+ os `.bak_<ts>_plangate` da rodada anterior).

## 4. Status — Hermes agora no mesmo nível hermético?

**SIM (nível físico)**: mesmo sem flag nativa, o isolamento via Docker garante
que o Hermes **não consegue persistir nada** em modo plan — rootfs read-only +
volume real read-only + home tmpfs descartável + `--rm`. A limitação restante é
cosmética: o hermes pode *responder* "criado" (alucinação) — mas o
`hermesVerifyOutput` (camada de auditoria) anexa o aviso quando o caminho
citado não existe, e fisicamente nada é escrito.

## 5. Pendências

- Deploy/restart/push: **aguardando aprovação de Washington**.
- O custo: cada chamada de plan do hermes sobe um container efêmero (~1-2s de
  startup) — aceitável para o modo plan.