# Adendo — Deploy do fix terminal_active em produção + fumaça (27/08)

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_BUG_20260827_terminal_active_nunca_limpo.md (proposta aprovada).

---

## Deploy executado (aprovado por Washington)

1. **Backend** (`terminal_routes.go`): build `hokma_test` → backup binário
   (`hokma.bak_predep_ttydfix_20260827_123825`) → substituir → restart.
   Backups também de `.env` e `memory.db.bak_predep_ttydfix_20260827_123825`.
2. **Frontend** (`TerminalTTYDScreen.tsx`): `vite build` (PORT=3055, BASE_PATH=/)
   → `dist/public/*` → `/var/www/hok-os/` (backup `/var/www/hok-os.bak_20260827_123948`)
   → `nginx reload`. Bundle novo: `index-Cy5XPIhw.js` (tsc 0 erros).

## O que foi deployado
- `handleTerminalTTYDClose`: limpa o registro de sessão ativa junto do kill-session
- Novo `POST /terminal/ttyd/detach`: limpa o registro SEM matar a sessão tmux
- `detachTab` (⤴) do frontend chama o detach (fire-and-forget)
- TTL defensivo de 7 min: coluna `updated_at` (CREATE + ALTER migratório),
  `saveTerminalActive` grava, `loadTerminalActive` expira registro antigo

## Validação isolada (antes do deploy) — 4/4 PASS
1. Registro via fluxo do app (active) → criado com updated_at
2. Detach → registro limpo + sessão tmux viva
3. Close (X) → registro limpo + sessão morta
4. TTL: registro fresco → ponte (exceção §2.1); registro envelhecido >7min →
   `opencode_serve` responde (exceção não dispara)
+ `go vet` e testes existentes limpos

## Fumaça em produção — 4/4 PASS (ciclo via API, sessão hok-ttyd intacta)
| Passo | Resultado |
|---|---|
| 1. Abrir terminal (active) | registro `hok-ttyd` criado (12:42:18) |
| 2. Detach | `{"ok":true}`, registro 0 linhas, sessão hok-ttyd **VIVA** |
| 3. Mensagem com keyword no chat | **`opencode_serve`** — "FIX-TTYD-OK"; session_mode criada (`smoke_fix_ttyd_1 → ses_fbcc1d3c6ffeY6FfAzQ0VZRBJs`) |
| 4. Reabrir terminal (active) | re-registro `hok-ttyd` (12:42:43) |
| — | 0 panics/fatal no journal; hokma + opencode-serve ativos |

## Estado final
- Fix do `terminal_active` em produção: o registro agora reflete "terminal
  aberto agora" (detach/close limpam; TTL expira app morto).
- Exceção §2.1 do opencode serve volta a valer só com terminal aberto — o
  caminho `opencode_serve` via chat funciona após fechar/detach do terminal.
- Nota: o registro ficou com `hok-ttyd` (passo 4 re-registrou) — comportamento
  correto (terminal "aberto"); o botão ⤴ no app agora limpa o registro.
- Etapa 3 (opencode serve no chat) permanece validada ponta a ponta.