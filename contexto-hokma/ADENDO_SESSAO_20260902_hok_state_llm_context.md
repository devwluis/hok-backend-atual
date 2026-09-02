# ADENDO — SESSÃO 02/09 (rodada 4) — HOK_STATE.md: contexto compacto auto-gerado para LLMs

Sessão dedicada à dor real de "LLM precisa reexplicar contexto" + truncamento/quebra de patch.
Decisão (após avaliação honesta): NÃO integrar o kernel cognitivo (hok-lab, exploration/competition)
no backend — é over-engineering e colide com os gates. Foco no formato compacto de contexto.

## O que foi criado
- `backend/scripts/hok_state.sh` — gerador determinístico (NÃO usa LLM, não alucina).
  Extrai fatos reais do repo: rotas (`http.HandleFunc`), arquivos com gate de aprovação
  (`setPendingAction`/`pending_approval`), últimos 5 commits, status dos serviços.
- `/root/hokma/HOK_STATE.md` — saída (163 linhas): Infra + 100 rotas + 11 arquivos com gate +
  commits + pendências + padrões de falha.
- `/root/hokma/PENDENCIAS.md` e `/root/hokma/PADROES_FALHAS.md` — mantidos À MÃO (o script só
  incorpora; não inventa pendência).
- `hok-state.timer` (systemd) — regenera a cada 15min (OnBootSec=2min + OnUnitActiveSec=15min).

## Ciclo
```
PENDENCIAS.md ──┐
PADROES_FALHAS ─┼→ hok_state.sh (timer 15min) ─→ HOK_STATE.md
git/grep ───────┘
```

## Por que
- LLM lê HOK_STATE.md em ~2s em vez de o Washington reexplicar todo o contexto (causa truncamento).
- Fatos (rotas/commits) sempre atuais via script; julgamento (pendências/lições) fica à mão.
- "Padrões que já falharam" evita repetir erros documentados (heredoc truncando, em-dash Unicode,
  campo de struct assumido, token n8n expirado).

## Arquivos alterados
- `backend/scripts/hok_state.sh` (versionado, commit `30a3b8c`, push em hok-backend-atual).
- `/root/hokma/HOK_STATE.md`, `PENDENCIAS.md`, `PADROES_FALHAS.md` (fora do repo — dados vivos).
- `/etc/systemd/system/hok-state.{service,timer}`.

## Pendências
- (opcional) versão Go do gerador se o .sh ficar insuficiente.
- (opcional) prompt de sistema simples integrando o HOK_STATE.md como instrução padrão.
- Itens abertos listados em PENDENCIAS.md (autopatch_loop gate, automation.go token, reconnect
  Google Sheets, rate-limit glm-5.2:free).