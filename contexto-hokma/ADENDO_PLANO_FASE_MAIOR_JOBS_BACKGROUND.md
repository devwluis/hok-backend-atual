# Adendo — Plano da FASE MAIOR (jobs em background): itens 2+3 pendentes

**Origem:** opencode (terminal) **Data/hora:** 27-08-2026
**Referência:** relatório de investigação dos 4 itens (sessão 27/08) — status do terminal, interrupção do "pensando" ao sair da aba, tarefas longas sem indicador, label do engine.

---

## Decisão de Washington (27/08)

- **Item 4 (label dos engines)**: corrigido e DEPLOYADO isolado (fase rápida) —
  ver ADENDO da sessão (deploy engineLabel).
- **Itens 2+3 (desacoplamento do processamento)**: ficam para a PRÓXIMA SESSÃO —
  mudança de ~6-9h com risco médio (troca de r.Context() por ctx próprio).
  Este adendo registra o plano completo como etapa pendente, para retomar com calma.

## Causa raiz (itens 2+3 — mesma origem)

O processamento do chat roda SÍNCRONO dentro do request HTTP
(`handleSmartChat` → `runSmartText(r.Context(), ...)`):
- trocar de aba no app desmonta o ChatScreen (AppShell renderiza só a tela ativa)
  → o estado local (loading/bolha "pensando") é destruído; o fetch continua em
  background mas a resposta chega "para ninguém";
- sair do app/navegador → o browser aborta o fetch → r.Context() cancelado →
  engines que respeitam o ctx (agent loop, opencode CLI) são interrompidos;
- o sendWatchdog de 180s desliga o loading mesmo com o processamento ativo →
  tarefas longas perdem a indicação de trabalho.

## Plano — job em background com ID + polling (aprovado para retomada)

### Backend (~3-4h)
1. **Novo `chat_jobs.go`**: `chatJob{ID, ConvID, TenantID, UserID, Status(running|done|error),
   Reply, Mode, Skill, Engine, ModelUsed, CreatedAt, FinishedAt}` +
   `map[string]*chatJob` + `startChatJob` / `getChatJob` / `listChatJobs(convID)`.
2. **`handleSmartChat`** (smart_chat.go): com `"async": true` no request (ou novo
   `POST /chat/job`): cria o job, retorna `{jobID, status:"running"}` na hora; a
   goroutine roda `runSmartText` com **`context.Background()` + timeout próprio
   (~10 min)** — não usa mais `r.Context()` → sobrevive à desconexão. Ao
   terminar: grava a resposta em `conv_messages` + status do job.
   Novos endpoints: `GET /chat/job?conv_id=X` (status) e `GET /chat/job/{id}`
   (resultado). Fluxo síncrono atual permanece como compat/fallback.
3. Persistência: memória na v1 (restart do hokma durante o job → resposta fica
   na sessão serve; fallback: frontend recupera do histórico da conversa).

### Frontend (~2-3h)
4. **`chat-stream.ts`/`ChatScreen.tsx`**: envio com `async: true` → recebe
   `{jobID}`; bolha "processando" dirigida pelo status do job (polling 2s em
   `GET /chat/job`); **substitui o sendWatchdog de 180s** (loading só desliga
   quando o job termina/erro).
5. **Retomada ao voltar**: no mount do ChatScreen, `GET /chat/job?conv_id=X` —
   job running → retoma polling + bolha; resposta pronta → recarrega a conversa
   do backend (persistência em conv_messages).

### Esforço total: ~6-9h · Riscos médios
- trocar `r.Context()` por ctx próprio exige revisar usos (agent loop —
  `RunAgentLoop(ctx)`, CLI opencode — `exec.CommandContext`);
- job em memória: restart do hokma durante o job (fallback documentado);
- polling concorrente entre abas/dispositivos: inofensivo (leitura).

## Próxima etapa (quando retomar)
Implementar conforme acima, validar isolado (troca de aba durante processamento
→ job continua; voltar → retoma; tarefa longa > 3 min → bolha permanece), depois
deploy + smoke + adendo.