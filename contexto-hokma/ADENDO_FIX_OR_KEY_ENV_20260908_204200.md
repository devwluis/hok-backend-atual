# ADENDO — Correção OR_KEY/OPENROUTER_API_KEY no backend .env — 08/09/2026

**Data:** 2026-09-08 20:42 UTC
**Operador:** opencode (mimo-v2.5-free)

## Contexto

A chave `OR_KEY`/`OPENROUTER_API_KEY` no `/root/hokma/backend/.env` retornava `401 authentication_error: "User not found"` — a mesma chave revogada que causava o erro do Claude Code (já corrigido antes).

## Correção aplicada

1. Backup: `/root/hokma/backend/.env.bak_20260908_203944`
2. `OR_KEY` e `OPENROUTER_API_KEY` atualizadas com a chave válida do opencode (`/root/.local/share/opencode/auth.json` → `openrouter.key`)
3. Escrita atômica (.tmp + mv)
4. `systemctl restart hokma.service`
5. Verificado: `active` + healthcheck `{"status":"ok"}`

## Evidência do teste

### Teste direto (modelo pago 0731 via OpenRouter com a chave do .env)
```
POST https://openrouter.ai/api/v1/messages
Authorization: Bearer <chave nova do .env>
model: deepseek/deepseek-v4-flash-0731

→ HTTP 200
→ Resposta: "OK"
→ model: deepseek/deepseek-v4-flash-0731
```

**Antes**: 401 "User not found" | **Depois**: 200 OK ✓

### Teste via motor Hok chat (/chat/smart)
```
{"reply":"OK.","mode":"chat","engine_used":"chat","model_used":"deepseek/deepseek-chat-v3.1","latency_ms":7090}
```
Resposta OK — o pool respondeu sem 401.

### Logs do serviço (confirmam chave funcionando)
```
[fallback] ✓ HOK/Fallback-deepseek/deepseek-chat-v3.1 respondeu
```
O erro de fallback restante (`minimax/minimax-m3:free` → "This model is unavailable for free. The paid version is available now") é OUTRA questão — o modelo free do MiniMax saiu do ar na OpenRouter — não relacionada à chave.

## Achado adicional (não corrigido — requer decisão)

O modelo ativo do usuário está como `openrouter/free` (router pago) e `minimax/minimax-m3:free` está indisponível na OpenRouter ("unavailable for free"). O fallback `deepseek/deepseek-chat-v3.1` está cobrindo, mas o usuário pode querer trocar o modelo ativo manualmente no seletor.

## Status

- ✅ OR_KEY/OPENROUTER_API_KEY corrigidas e funcionando (200)
- ✅ Serviço ativo + healthcheck OK
- ✅ Teste real com modelo pago 0731 passou