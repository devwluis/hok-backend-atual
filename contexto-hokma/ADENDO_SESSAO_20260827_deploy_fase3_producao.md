# Adendo — Sessão 27/08 11:30 · Deploy Fase 3 Etapa 3 em produção + fumaça

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_SESSAO_20260827_fase3_etapa3_validada.md (validação isolada 7/7).

---

## Deploy executado (aprovado por Washington)

1. **Push** do main para origin/hok-backend-atual (fast-forward `204f3cd..ce78bbe` —
   10 commits: 9 de sessões anteriores + Fase 3 Passo 1-2 + Etapa 3).
2. **systemd unit `opencode-serve.service`**: `/root/.opencode/bin/opencode serve
   --port 4100 --hostname 127.0.0.1`, `Restart=on-failure` (decisão do usuário —
   falha repetida aparece como serviço parado), `RestartSec=3`, senha via
   `Environment=OPENCODE_SERVER_PASSWORD` (gerada: openssl rand -hex 24; arquivo
   de trabalho em /tmp/opencode/serve_password.txt).
3. **`.env` de produção**: APENAS `OPENCODE_SERVE_URL=http://127.0.0.1:4100` e
   `OPENCODE_SERVE_PASSWORD=<gerada>` (append, sem ler o resto; nada mais mudou).
4. **Binário**: `go build -o hokma_test .` → `cp hokma_test hokma` → restart.
5. **Backups**: `hokma.bak_predep3_20260827_111157`, `.env.bak_predep3_...`,
   `memory.db.bak_predep3_20260827_112150` (BANCO REAL — ver abaixo).

## Achado importante durante o fumaça (não é bug)

- O **banco real de produção é `memory.db`** (6MB), não `hokma.db` (20KB, legado).
  Primeiros backups foram do banco errado — refeitos corretamente.
- `terminal_active` no memory.db = **hok-ttyd** (usuário com o terminal ABERTO no
  app durante o fumaça) → a decisão §2.1 aprovada (terminal visível → ponte ttyd
  cuida) fez as mensagens de teste caírem na ponte legada (`ttyd_bridge`), NÃO no
  opencode serve. **Comportamento correto conforme aprovado**, confirmado com
  instrumentação temporária (logs gate) e removida depois.
- O caminho serve foi validado ponta-a-ponta contra o serve DE PRODUÇÃO (senha
  real) rodando o binário de produção localmente na 8091 → `engine:
  opencode_serve` (resposta "LOCAL-OK").

## Resultado do checklist de fumaça em produção

| # | Item | Resultado |
|---|---|---|
| 1 | Units ativas | PASS (hokma + opencode-serve active) |
| 2 | /doc + health serve (senha real) | PASS (200) |
| 3 | journalctl sem panic/fatal | PASS |
| 4 | Rotas de teste ausentes (OPENCODE_SERVE_TEST não setado) | PASS (resposta = handleRoot genérico) |
| 5 | Conversa via serve no /chat/smart | **PARCIAL** — com terminal aberto, §2.1 manda para a ponte (correto); caminho serve validado via 8091. Falta teste com o terminal FECHADO no app |
| 6-8 | session_mode por conv | **PENDENTE** — 0 linhas no memory.db (serve não exercitado via chat ainda; será criado no teste com terminal fechado) |
| 9 | Fallback serve-down | validado no isolado (cenário 6/7); em produção o caminho legado está funcionando (ttyd_bridge nos logs) |
| 10 | Blocklist | PASS (via ponte, `terminal_exec_blocked` — comportamento legado correto com terminal aberto) |
| 11 | Aprovação | validado no isolado (cenário 5); em produção com terminal aberto o fluxo legado prevalece |

## Pendências
- **Teste do usuário**: no app, com o terminal FECHADO, enviar mensagem com
  keyword de terminal → deve responder via opencode serve (engine opencode_serve)
  e criar linha em session_mode no memory.db.
- Commit de eventuais ajustes pós-fumaça (working tree está limpo vs HEAD).
- drive_creds.env: pendente de autorização (ver passo a passo Google Cloud no
  relatório da sessão).