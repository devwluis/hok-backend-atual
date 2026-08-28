# Adendo — Sessão 28/08 · Correção definitiva do catálogo de modelos (Zen/OpenRouter) + trava de segurança

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** tabela de divergências apresentada e confirmada por Washington
antes da aplicação.

---

## Auditoria (itens 1) — tabela de divergências (dados reais das APIs)

- **OpenRouter (398 modelos)**: o catálogo HOK usa a própria API como fonte —
  **0 IDs divergentes** e **0 flags free erradas**.
- **OpenCode Zen (64 modelos)**: **64/64 divergentes** —
  1. formato do ID: HOK usava `mimo-v2.5-free` (sem prefixo); o formato
     oficial do config é **`opencode/<model-id>`** (ex: `opencode/gpt-5.5`);
  2. free: TODOS marcados pago; os gratuitos por nome (`mimo-v2.5-free`,
     `deepseek-v4-flash-free`, `laguna-s-2.1-free`, `hy3-free`,
     `ling-3.0-flash-fin-free`, `nemotron-3-ultra-free`,
     `nemotron-3.5-lightning-free`, `muse-spark-1.2-contributor-free`)
     estavam como PAGO (a API Zen não retorna o campo free — grátis é
     "período promocional", pode virar pago/sair do ar sem aviso).
- **Caso mimo**: `mimo-v2.5-free` só existe no catálogo Zen (roda via
  opencode) — o slug real OpenRouter é `xiaomi/mimo-v2.5` e
  `xiaomi/mimo-v2.5-pro` (PAGOS). O HOK marcava o Zen como pago e o mesmo
  "mimo" aparecia duplicado (Zen pago + Xiaomi pago) — a confusão reportada.

## Correções aplicadas

1. **models_catalog.go**:
   - Zen: ID **`opencode/<model-id>`** (formato oficial); `-free` no ID →
     `free=True` (gratuito promocional); TTL do Zen **24h → 1h**
     (sincronização automática — a infra de refresh/ticker 5min já existe);
   - fonte CLI: `opencode/<id>` consistente (dedup com a API Zen).
2. **model_gate.go** (novo) — TRAVA DE SEGURANÇA:
   - `classifyModelStatus`: 402→"Modelo agora é pago"; 404/410→"Modelo
     expirou"; 400/not valid→"Modelo indisponível"; transitórios (429/5xx)
     → sem trava (cascata normal).
   - `modelForOpenRouter`: só slugs OpenRouter reais passam aos motores que
     falam direto com o OpenRouter (hermes/claude/routeModel); tiers Zen/Go
     (`opencode/*`, `opencode-go/*`) → bloqueio "não suportado por este
     motor" (sem fallback).
   - aplicada em: routeModel (blocos geral + deepseek + cerebras), tryHermes
     (normal/plan/autonomous), callHermes (removeu o fallback modelB quando
     o erro é de modelo), tryClaudeCode, tryOpenCode, opencode serve (erros
     que vêm como TEXTO da resposta).
3. **syncActiveModel** — só loga, **não persiste mais** (o modelo ativo só
   muda via /models/select; initActiveModel já restaura no boot).
4. **/models/catalog**: campo `activeStatus` (ok/expired — sumiu da lista).
5. **Frontend (ModeSelector)**: badge de alerta "Modelo expirou/agora é
   pago/indisponível — troque na lista de IA" (via activeStatus).

## Evidências reais (E2E isolado 8099)

### (a) mimo funcionando corretamente
- Catálogo: `[OpenCode Zen] opencode/mimo-v2.5-free free=True` ✓ (e
  `opencode/deepseek-v4-flash-free free=True`; `xiaomi/mimo-v2.5` pago —
  separado)
- Seleção persistida: `active: opencode/mimo-v2.5-free | activeStatus: ok` ✓
- Trava por motor (hermes com o mimo):
  `reply: "Modelo indisponível — o modelo ativo (opencode/mimo-v2.5-free) não
  é suportado por este motor. Escolha outro modelo na lista de IA."`
  `mode: model_unavailable` + `[AUDIT] model_gate engine=hermes ... status=unavailable — seleção do usuário mantida`
  (ANTES da correção, o hermes caía no fallback modelB e respondia com outro
  modelo — o "trocar sozinho".)

### (b) modelo expirado forçado (deepseek/nao-existe-xyz999 → OpenRouter 400)
- `reply: "Modelo indisponível — escolha outro modelo na lista de IA para continuar."`
- `active: deepseek/nao-existe-xyz999 | activeStatus: expired` — **NÃO trocou** ✓
- `[AUDIT] model_gate engine=chat model=deepseek/nao-existe-xyz999 status=unavailable — seleção do usuário mantida, envio bloqueado até troca manual`
- 402/404/410 cobertos por testes unit (classify) — o mesmo caminho de bloqueio.

### Observação de ambiente
- O `opencode auth list` do servidor tem só **OpenRouter** (sem Zen) — o
  serviço opencode serve não roda modelos Zen (o mimo roda no Termius do
  usuário porque lá o auth do Zen existe). Com o catálogo corrigido, o mimo
  fica selecionável; o servidor precisa do auth Zen (opencode auth login)
  para o serve rodar o mimo — fora do escopo do backend.

## Testes
- Suíte Go completa PASS (24.5s → 9.6s) — 5 testes novos
  (TestClassifyModelStatus, TestModelForOpenRouter, TestModelBlockReply,
  TestHermesModelResult, TestModelBlockIfExpired).
- Build + vet limpos.

## Pendências
- Código aplicado e testado (E2E isolado) — **deploy em produção (restart do
  hokma) aguardando aprovação**. Commit/push (backend + frontend) também.
- Lembrete: o modelo ativo de produção foi restaurado para
  `deepseek/deepseek-v4-flash-0731` após os testes; o antigo `mimo-v2.5-free`
  (ID sem prefixo) precisa ser re-selecionado na lista (agora
  `opencode/mimo-v2.5-free`) se o usuário quiser voltar a ele.