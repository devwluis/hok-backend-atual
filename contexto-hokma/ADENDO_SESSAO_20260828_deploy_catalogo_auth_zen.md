# Adendo — Sessão 28/08 · Deploy do catálogo/trava + autenticação Zen no servidor

**Origem:** opencode (terminal) **Data/hora:** 28-08-2026
**Referência:** ADENDO_SESSAO_20260828_catalogo_modelos_trava.md (correções).

---

## 1. Commit/push (aprovado)

- Backend: `6c97a6f..641a957 main -> main` (fix catalog + model-gate)
- Frontend: `68f8645..dd2595b main -> main` (badge de alerta da trava)

## 2. Deploy em produção

- Backup: `hokma.bak_pre_catalogfix_<ts>`, `memory.db.bak_pre_catalogfix_<ts>`
- `go build` OK → stop → cp → start → **active** + `/ping: {"status":"ok"}`
- Catálogo em produção: 512 modelos (62 free) — `opencode/mimo-v2.5-free
  free=True` ✓, `opencode/deepseek-v4-flash-free free=True` ✓, `xiaomi/mimo-v2.5`
  pago (separado) ✓; active: deepseek/deepseek-v4-flash-0731 | activeStatus: ok

## 3. Autenticação do OpenCode Zen no servidor

- **Já configurada**: `~/.local/share/opencode/auth.json` contém o provider
  `opencode` (api key — a mesma do Termius) e `opencode auth list` mostra
  OpenCode Zen api ✓. O opencode-serve.service roda como root (mesmo HOME).
- **Teste real**: `opencode run --model opencode/big-pickle "..."` →
  **"big-pickle (opencode/big-pickle)."** — o Zen RODA no servidor ✓
- **mimo-v2.5-free**: NÃO roda no servidor — erro do gateway do opencode:
  `[404] No allowed providers are available for the selected model. Providers
  serving xiaomi/mimo-v2.5-20260422: gmicloud, deepinfra, xiaomi, parasail,
  streamlake, novita, but your request's provider.only preference permits
  only: tencent.` → os modelos free são roteados ao provider tencent, que
  NÃO serve o mimo. Não é problema de auth — é disponibilidade do free
  (período promocional encerrado para a conta/workspace
  wrk_01KWA1RGF56NXA2E75150937SE — log também mostra "Insufficient balance"
  para modelos do gateway).
- deepseek-v4-flash-free: erro do servidor do gateway ("Unexpected server
  error") — mesmo contexto (workspace).

## 4. Modelo ativo / re-seleção

- O ativo está `deepseek/deepseek-v4-flash-0731` (OpenRouter, funcional —
  restaurado após os testes).
- Se selecionar o `opencode/mimo-v2.5-free` na lista: aparece como free no
  catálogo, mas o gateway retorna 404 → a TRAVA responde "Modelo expirou"
  (o texto do serve contém "404") sem trocar nada.
- Free que FUNCIONA no servidor hoje: `opencode/big-pickle`.

## Pendências

- Saldo/workspace do opencode (opencode.ai/workspace/wrk_01KWA1RGF56NXA2E75150937SE/billing):
  se o usuário quiser os free via gateway (deepseek-v4-flash-free etc), o
  workspace precisa de saldo/verificação — fora do escopo do servidor.