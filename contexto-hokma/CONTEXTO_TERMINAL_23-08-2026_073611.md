# CONTEXTO_TERMINAL — Verificação pós-bateria + TESTE 1 validado (23/08/2026)

## 1. Verificação de integridade (pós-queda de bateria)
| Item | Estado |
|---|---|
| Frontend HEAD | 484fab8 (ttyd definitivo) ✓ |
| Teclado overlay | Implementado no working tree (+161/−26), produção já servia build com overlay |
| Backend rota /terminal/ttyd/key | Implementada (+83 linhas), binário produção inclui ✓ |
| hokma.service | active; restarts registrados = deploys legítimos |
| hok-terminal.service | active, 0 falhas em 12h |

Correções aplicadas: commits dos trabalhos não-finalizados:
- backend 3fdacb5 (rota injeção de teclas via tmux send-keys)
- frontend 55e6871 (teclado estendido overlay sobre ttyd)

## 2. Validação TESTE 1 (E2E headless Chromium real)
| Teste | Resultado |
|---|---|
| Barra colada na base sem teclado | ✓ (y+h = viewport exato) |
| ↑ navega histórico | ✓ |
| ^C interrompe sleep | ✓ |
| Símbolo \| digitado | ✓ |
| C-A opencode PARADO 60s | 12 snapshots = 1 hash único — ZERO oscilação ✅ |
| C-B scroll manual 20s | responde à rolagem, sem travar ✅ |
| C-C troca aba Chat↔Terminal | iframe+overlay persistem, LIVE ✅ |

## 3. Descoberta ambiental relevante
/tmp (tmpfs 3.9GB) estava 100% cheio: 433 × .so de ~14MB = 3.6GB vazados pelo
runtime Bun do opencode (extrai libs a cada execução, nunca limpa). Efeitos:
Chromium de teste crashava (font service sem espaço). Limpos → testes estáveis.
RECOMENDAÇÃO FORTE: cron `find /tmp -name '.*.so' -mmin +1440 -delete` e/ou
reportar upstream opencode. Cada abertura de opencode no terminal web = +14MB.

## 4. Correções adicionais desta rodada
- Whitelist send-keys por regex (aceita combos C-x/M-x) — commit eca7411.
- Rota theme aceita {osc} pronto além de paleta estruturada.

## 5. Pendências
1. 🔴 Credencial Google Drive account 2 EXPIRADA no n8n (401 desde 22/08 05:43).
   Reconectar em Credentials na UI n8n; depois reenviar adendos acumulados:
   CONTEXTO_TERMINAL_23-08-2026_054341.md e este arquivo.
2. DNS terminal.imoveischaves.com: CONFIRMADO FUNCIONANDO pelo dono (401 sem
   token via tunnel+proxy ✓). Pendência removida.
3. Validação visual completa do teclado no celular real (barra ancorada ao
   teclado via visualViewport — fix deployado, requer dispositivo).
