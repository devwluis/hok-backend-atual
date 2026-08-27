# Adendo — Bug estrutural: registro terminal_active nunca é limpo ao fechar/desanexar a aba

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** ADENDO_SESSAO_20260827_deploy_fase3_producao.md (deploy Etapa 3), decisão §2.1 (bridge ttyd para terminal visível).

---

## Contexto

Durante o fechamento da pendência do fumaça da Fase 3 Etapa 3 (testar o caminho
`opencode_serve` via chat em produção), descobrimos que a exceção §2.1 do
`tryOpenCodeServe` (terminal visível → ponte ttyd cuida) nunca deixava de
disparar: a mensagem de teste caía sempre na ponte legada, mesmo com o terminal
do app fechado/desanexado.

## Causa raiz (confirmada em código)

1. O backend registra a sessão ativa do ttyd em `terminal_active`
   (`saveTerminalActive`, chamado no fluxo do `POST /terminal/token` —
   terminal_routes.go:749-796).
2. **Não existe NENHUM caminho que limpe o registro**: busca por
   `DELETE FROM terminal_active` / `removeTerminalActive` no backend →
   vazio. O `handleTerminalTTYDClose` (botão X do app) mata a sessão tmux
   (`tmux kill-session`) mas NÃO limpa o registro.
3. O frontend também não limpa: `detachTab` (botão ⤴ "sair sem encerrar")
   só remove a aba da visualização local; `closeTab` (X) chama o close do
   backend + detachTab — nenhum dos dois remove o registro.
4. Efeito: o registro `terminal_active` persiste **indefinidamente** após o
   primeiro uso do terminal. Enquanto a sessão tmux correspondente existir
   (`tmux has-session` OK), a exceção §2.1 do `tryOpenCodeServe` dispara
   para TODAS as mensagens com keyword de terminal/forceOpenCode — o
   caminho `opencode_serve` via chat fica efetivamente **inutilizável
   depois do primeiro uso do terminal**, mesmo com o app fechado.

## Comportamento esperado

O registro `terminal_active` deve refletir "terminal **aberto/anexado agora**",
não "terminal usado alguma vez":

- Registro criado/atualizado quando o usuário ABRE/anexa o terminal no app.
- Registro **removido** quando o usuário fecha a aba (detach) ou encerra a
  sessão (kill).
- A exceção §2.1 só deve valer enquanto o terminal está de fato aberto no app.

## Proposta de fix (NÃO implementada — aguardando revisão/aprovação)

1. **Backend**: limpar o registro em `handleTerminalTTYDClose` (junto do
   `kill-session`) e adicionar limpeza no endpoint de desanexo — verificar se
   o frontend chama algo ao detachar (hoje não chama; se não houver endpoint,
   o detach pode passar a chamar um `POST /terminal/ttyd/detach` novo que só
   limpa o registro, sem matar a sessão).
2. **Frontend**: em `detachTab`, disparar a limpeza (novo endpoint ou
   parâmetro no close existente).
3. **Alternativa/defensiva**: TTL no registro (ex.: expirar `terminal_active`
   se `updated_at` for antigo, ou o `loadTerminalActive` validar que a sessão
   está `attached` via `tmux ls` em vez de apenas `has-session`) — cobre
   também abas fechadas sem o frontend notificar (ex.: app morto).

## Validação desta sessão

- Limpeza manual autorizada (opção A): `DELETE FROM terminal_active WHERE
  id=1` — registro vazio (0 linhas), sessão tmux hok-ttyd e processo bash
  parado INTACTOS (não mexidos), backup do memory.db feito
  (`memory.db.bak_pre_teste_fumaca_<ts>`).
- Aguardando a mensagem de teste do usuário para validar o caminho
  `opencode_serve` em produção.
---

## RESULTADO FINAL — Fumaça da Etapa 3 fechado (12:24 UTC, 27/08)

### Teste do usuário via app (~12:21 UTC)
- Mensagem com palavra de terminal SEM keyword de gatilho do serve (não continha
  "comando/roda/executa/shell") → o `tryOpenCodeServe` NÃO disparou (gatilho por
  design: keyword de terminal OU forceOpenCode) → caiu no `tryOpenCode` (CLI):
  log `opencode_invoke:opencode-go/deepseek-v4-flash-vision-exp ok` às 12:21:09
  na tabela `logs`; resposta "/root/hokma" = cwd do CLI (ROOT_PATH); NENHUMA
  linha em session_mode (serve não foi chamado).
- Comportamento esperado (não é bug da Etapa 3): o fluxo CLI do tryOpenCode é
  superfície pré-existente (Fase 1), mantida; a Fase 3 substituiu a ponte PTY.

### Validação ponta a ponta via curl (12:24 UTC) — APROVADO
- Registro `terminal_active` limpo (opção A, reaplicada — o app re-registra ao
  reabrir o terminal: logs 12:00:44, 12:19:14/29, 12:21:26; o registro persiste
  e só sai com DELETE manual — mesmo bug documentado acima).
- Mensagem "Rode o comando pwd no servidor e me diga o diretório" →
  **engine: opencode_serve, mode: opencode_serve** — resposta
  `/root/hokma/backend` (cwd do serve) — sem eco de shell.
- **session_mode criada**: `smoke_e3_serve_2 → ses_fbcd2a6ddffehSjmK5RceqHqKA`
  (updated_at 12:24:00).
- Pendência do checklist de fumaça (itens 5-8) **FECHADA**: caminho
  `opencode_serve` validado em produção, ponta a ponta.

### Observação final
- Para o usuário exercitar o caminho serve pelo app: usar mensagem com keyword
  de gatilho (ex.: "Rode o comando ... no servidor") e SEM o terminal aberto —
  ou aguardar o fix do registro `terminal_active` (proposta registrada acima).
