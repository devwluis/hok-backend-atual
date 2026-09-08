# ADENDO — Fix Claude Code CLI (OpenRouter key) — 08/09/2026

**Data:** 2026-09-08 20:35 UTC
**Operador:** opencode (mimo-v2.5-free)

## Diagnóstico

O Claude Code CLI na VPS (~/hokma) estava com "API error, retrying, attempt X/10" ao responder no terminal (hok-ttyd).

### Causa raiz
A chave `ANTHROPIC_AUTH_TOKEN` em `~/.claude/settings.json` era uma chave OpenRouter **inválida/revogada** — retornava `401 authentication_error: "User not found"` em TODOS os endpoints (messages, chat/completions, auth/key).

O settings.json já apontava para OpenRouter (`ANTHROPIC_BASE_URL=https://openrouter.ai/api`, modelo `deepseek/deepseek-v4-flash-0731`) — a config MiniMax já tinha sido substituída antes.

### Evidências do diagnóstico
| Teste (com chave do settings.json) | Resultado |
|------------------------------------|-----------|
| POST openrouter.ai/api/v1/messages | 401 "User not found" |
| POST /api/v1/chat/completions | 401 "User not found" |
| GET /api/v1/auth/key | 401 |
| **Mesma chamada com chave do opencode auth.json** | **200 OK** ✓ |

## Provedores verificados (endpoints Anthropic-compat)

| Provider | Endpoint | Status | Modelos Claude reais |
|----------|----------|--------|---------------------|
| **OpenRouter** | `https://openrouter.ai/api/v1/messages` | ✅ **200 OK** | ✅ sim (sonnet-4.5, opus-4.6 testados) |
| AIHubMix | `https://aihubmix.com/v1/messages` | ⚠️ Endpoint existe, conta SEM SALDO (403) | — |
| OpenCode Zen | `https://opencode.ai/zen/v1/messages` | ⚠️ OpenAI-compat, não Anthropic nativo; conta sem saldo (CreditsError) | — |

## Correção aplicada (Opção A — aprovada)

1. Backup: `~/.claude/settings.json.bak_20260908_203210`
2. Copiada `openrouter.key` de `/root/.local/share/opencode/auth.json` → `ANTHROPIC_AUTH_TOKEN` (via Python, sem expor valor)
3. `ANTHROPIC_BASE_URL=https://openrouter.ai/api` mantido
4. `ANTHROPIC_MODEL=deepseek/deepseek-v4-flash-0731` mantido
5. JSON validado com `python3 -m json.tool`, escrita atômica (.tmp + mv), chmod 600

## Teste real

```
$ claude -p "oi"
Oi! 👋 Estou aqui em /root/hokma (branch clean_master). O que você precisa hoje?
```

```
$ claude -p "Em uma frase: qual é o modelo de IA que você está usando agora?"
Estou rodando no modelo DeepSeek V4 Flash (deepseek/deepseek-v4-flash-0731) via Claude Agent SDK.
```

**Sem erro de API.** ✓

## Achado separado (backend Go .env — NÃO alterado)

A chave `OR_KEY`/`OPENROUTER_API_KEY` no `/root/hokma/backend/.env` **também retorna 401** ("User not found") — é DIFERENTE da chave válida do opencode auth.json. O backend Go usa essa chave para o catálogo de modelos (que funciona sem auth real na listagem) e para o pool de chamadas OpenRouter — que PODE estar falhando silenciosamente nos modelos pagos.

**Pendente de decisão do usuário**: atualizar ou não a OR_KEY do .env para a chave válida do auth.json (componente separado — não mexido).