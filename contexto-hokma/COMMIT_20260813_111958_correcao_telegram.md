# COMMIT — Restauração da comunicação HOK ↔ Telegram (via n8n)
Data: 2026-08-13 11:19 · Por: Washington + Claude Code (sessão local, direto no servidor)
Escopo: n8n (workflow "HOK OS Multi IA Telegram"), sem alterações no código Go

---

## 1. Sintoma reportado pelo usuário
"Perdemos a comunicação minha e de Hok" — no grupo `Hokmá` do Telegram, o bot só respondia

> **Hokma indisponivel**
> *This message was sent automatically with n8n*

O HOK parecia mudo, mas na verdade **nunca esteve mudo** — o agente respondia o tempo todo; o problema era no nó de envio.

## 2. Diagnóstico (evidência)
- Todos os serviços UP: `hokma.service` (ativo 24h), `n8n_oficial` (13h), nginx, traefik.
- Bot `@hokma_alertas_bot` (`TELEGRAM_TOKEN` válido), webhook aponta para
  `https://n8n.imoveischaves.com/webhook/hok-multi-ia` — `pending_update_count: 0` (entrega OK).
- Workflow `HOK OS Multi IA Telegram` (SFd42XABa4HvpPXT) **ativo**, webhook responde 200.
- Flow: `Webhook → Parse → Should Process(IF) → (Typing + "HOK Agente Saude" messageAnAgent) → Send Reply`.

### Causa raiz — campo de saída errado no "Send Reply"
O nó `HOK Agente Saude` é um `n8n-nodes-base.messageAnAgent` (typeVersion 3,
`useStructuredOutput: false`) e entrega a resposta no campo **`$json.text`**.

O nó `Send Reply` (telegram v1.2) usava:

```
text: ={{ $json.reply || $json.response || 'Hokma indisponivel' }}
```

Como `reply` e `response` **não existem** na saída do agente, a expressão caía **sempre**
no fallback `'Hokma indisponivel'`. Confirmado em TODAS as execuções (4+), onde o agente
respondia corretamente e o "Send Reply" enviava só o fallback:

| Execução | Agente respondeu | Enviado de fato |
|---|---|---|
| 19234 | "Oi! Tudo bem? 😊 ..." | ❌ Hokma indisponivel |
| 18959/18901/18897 | "Olá! ..." | ❌ Hokma indisponivel |

- Execução 18886 (error, isolada): `Bad Request: chat not found` — caso pontual de chat
  inválido temporário, NÃO é o problema de fundo.

## 3. Correção aplicada (no n8n, via API — conexões do workflow preservadas, ativo)
1ª tentativa (falhou): salvei `{{ $json.text ... }}` **sem** prefixo `=` → n8n enviou o
template como **texto literal** (apareceu `{{ $json.text || 'Hokma indisponivel' }}` no grupo).
> Lição 2-extra: no n8n, o prefixo `=` ANTES de `{{...}}` é o que marca "avaliar como
> expressão". Sem ele, vira string literal. O formato exportado é `={{ expr }}`.

2ª tentativa (validada): o que o n8n armazena/interpreta corretamente:
```
antes:  ={{ $json.reply || $json.response || 'Hokma indisponivel' }}
depois: ={{ $json.text || 'Hokma indisponivel' }}
```
Mantive o fallback de segurança (`|| 'Hokma indisponivel'`).

Comandos usados: `GET/PUT /api/v1/workflows/SFd42XABa4HvpPXT` (auth `X-N8N-API-KEY`),
payload com `name/nodes/connections/settings`; `active: True` preservado.

## 4. Validação E2E (evidência)
- Após o fix, usuário mandou "Oi" no grupo → recebeu a **resposta real do agente**.
- Execução 19263: status `success`.
  - `HOK Agente Saude` -> "Olá! 😊 Como posso ajudar você hoje? Sou o agente de saúde do HOK OS... "
  - `Send Reply` (enviado) -> **mesmo texto do agente** (antes era "Hokma indisponivel").

✅ Comunicação HOK ↔ Telegram RESTAURADA.

## 5. Notas / aprendizados para próximas sessões
- **Regra fixa:** nó que consome resposta de um "Message an Agent" (v3, sem structured
  output) deve ler `$json.text`; confirmar campo real na execution data antes de assumir
  (lição 3 do HOK_MASTER_CONTEXT). Registrado na memória do projeto.
- **Regra n8n:** expressão em campo de texto usa o formato `={{ ... }}` (usar o export do
  workflow como fonte da verdade do formato).
- O `Typing` e o `Send Reply` usam um único bot (`Hokma Alertas`, credencial `RsxUnPy7wMDSO5hK`)
  no supergrupo `Hokmá` (`-1004304067839`); continua válido o ponto da ponte agente↔Telegram
  da sessão 12/08 (agora com resposta real chegando).

## 6. Pendências em aberto (não tocadas nesta sessão)
- `/notify` (telegram.go) e `notifyNewLeadTelegram` (crm_telegram.go) só enviam; não há
  webhook de entrada no Go (toda a conversa passa pelo n8n).
- Segurança SaaS pendente da varredura 12/08: 8082 exposta sem auth (`/introspect`, `/status`),
  OwnerGate client-side + token no bundle público, CORS `*`, timeout em `ExecuteApprovedCommand`.
- Validação da execução 18886 (`chat not found`) se reaparecer.

---

*Gerao a partir de sessão real no servidor (13/08/2026). Complementa HOK_MASTER_CONTEXT.md
e os COMMIT_* anteriores da pasta.*
