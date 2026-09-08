# ADENDO — Allowlist mínima de modelos pagos (OpenRouter / deepseek-v4-flash-0731) — 08/09/2026

**Data:** 2026-09-08 20:10 UTC
**Operador:** opencode (mimo-v2.5-free)

## Diagnóstico

O usuário reportou que `deepseek/deepseek-v4-flash-0731` (OpenRouter, pago) não aparecia no seletor — apesar de estar no catálogo backend (confirmado via /models/catalog: grupo OpenRouter, free=False, label "DeepSeek: DeepSeek V4 Flash 0731").

### Confirmação via git
- `git log --all -S '0731'` → **vazio** (nenhum commit com allowlist)
- `git log --all -S 'allowlist'` em hok-models.ts/ChatScreen.tsx → **vazio**
- O único commit relevante: `6705ee3` ("temp: esconde modelos pagos do picker — reversível via flag SHOW_PAID_MODELS")

**Conclusão:** a "allowlist mínima" do item 6 nunca chegou a ser implementada (não existe no código nem no histórico git). Foi reimplementada do zero.

### Causa raiz do modelo invisível
1. `getPaidModels()` retornava `[]` quando `SHOW_PAID_MODELS=false` (hok-models.ts:179)
2. O grupo "PAGO" nem era renderizado (ChatScreen.tsx:440 — `if (SHOW_PAID_MODELS && paid.length)`)

## Implementação

### `hok-models.ts`
```typescript
export const PAID_MODEL_ALLOWLIST: string[] = ["deepseek/deepseek-v4-flash-0731"];

export async function getPaidModels(force = false): Promise<HokModel[]> {
  const models = await getModels(force);
  if (!SHOW_PAID_MODELS) {
    return models.filter((x) => PAID_MODEL_ALLOWLIST.includes(x.id));
  }
  return models.filter((x) => !x.free && x.provider !== "OpenCode Zen");
}
```

### `ChatScreen.tsx`
Novo agrupamento por provider quando SHOW_PAID_MODELS=false: `modelsList.paid` (já filtrado pela allowlist) agrupado por provider → grupo "OpenRouter" (badge PAGO) com exclusivamente o 0731.

## Evidência real (DOM do seletor renderizado — porta pública 3002)

```
CATÁLOGO DE IA
OpenRouter            [PAGO]        ← GRUPO NOVO ✓
DeepSeek: DeepSeek V4 Flash 0731    ← VISÍVEL E SELECIONÁVEL ✓
HOK                   [FREE]
Auto                  [FREE]
OpenCode Zen          [FREE]  (30 modelos, incl. Big Pickle, Ox Alpha Free)
OpenRouter            [FREE]  (21 modelos)
AIHubMix              [FREE]
OpenCode Go           [FREE]  (Ox Alpha Free)
OpenCode Zen          [ZEN]   (só pagos, sem duplicação)
```

### Seleção real testada
Após clicar em "DeepSeek: DeepSeek V4 Flash 0731", o header do chat mudou de "Auto" para:
```
⚡ IA | DeepSeek: DeepSeek V4 Flash 0731
```

Screenshots salvos em:
- `/tmp/hokma_tmp/selector.png`
- `/tmp/hokma_tmp/selector_selected.png` (após seleção)

## Testes

- Frontend: build OK (`index-vJvt7fI5.js`)
- Nenhum outro modelo pago vaza (593 pagos fora da allowlist não aparecem)
- SHOW_PAID_MODELS continua `false`

## Status

- **Frontend deployado** em produção (`/var/www/hok-os/`) — index-vJvt7fI5.js
- **Backend:** NÃO alterado nesta tarefa (allowlist é 100% frontend)
- Aguardando aprovação final do usuário
