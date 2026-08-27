# Adendo — Atualização do Roteiro de Fases: consideração de pular Fase 1/2 → Fase 3
**Origem:** Claude Web (chat)
**Data/hora:** 26-08-2026_070336
**Referência:** ADENDO_ROTEIRO_FASES_TERMINAL_OPENCODE_SEM_TUI_20260824_181300.md

---

## Contexto desta atualização

Depois de registrado o roteiro original (Fase 1: forceOpenCode + Drive como
contexto; Fase 2: sessão tmux dedicada + resumo; Fase 3: opencode como
serviço com API própria), uma investigação direta no terminal (25/08)
revelou que a Fase 3 já está tecnicamente disponível e pronta para uso,
não apenas "a avaliar":

- `opencode serve` sobe um servidor headless real, com API HTTP completa
  (confirmado via `/doc`, spec OpenAPI 3.1.0), incluindo rotas de sessão
  (`/session`, `/session/{id}/message`, `/session/{id}/prompt_async`,
  `/session/{id}/summarize`, `/session/{id}/permissions/{id}`, `/event`
  para streaming), gerenciamento de MCP nativo (`/mcp/*`), VCS, comandos,
  agentes e skills.
- Existe autenticação nativa via variável de ambiente
  `OPENCODE_SERVER_PASSWORD` (não documentada no `--help`, descoberta pelo
  warning do log ao subir o servidor sem ela).
- O endpoint `/session/{id}/summarize` resolve nativamente o problema de
  estouro de contexto que motivava parte do desenho da Fase 1/2 (resumo
  manual via adendo).

Diante disso, Claude (assistente) sugeriu considerar pular diretamente
para a Fase 3, em vez de implementar o ciclo leitura/escrita do Drive
(Fase 1) e a sessão tmux dedicada (Fase 2) como passos intermediários —
já que a peça mais robusta já existe pronta, nativa do opencode, sem
precisar ser construída do zero.

Washington concordou em explorar esse caminho, mas pausou a decisão final
de arquitetura para primeiro fechar problemas de estabilidade da ponte
Chat Web ↔ Terminal atual (bugs de scroll, teclado, comportamento de
fechamento de aba — ver adendos de sessão de 25/08).

---

## Achado desta sessão (26/08): scroll funciona no PC, não no celular

Depois de múltiplos ciclos de correção do bug de rolagem na aba Terminal
do Hok Web (ver ADENDO_SESSAO_20260825_terminal_scrollback_fix.md e
adendos subsequentes do mesmo dia), Washington testou separadamente em
desktop (PC) e mobile (celular):

- **PC: rolagem funcionando corretamente** — gesto de mouse/wheel opera
  como esperado, confirma o que já havia sido reportado como testado.
- **Celular: rolagem continua não funcionando**, mesmo após os fixes
  aplicados e reportados como "resolvido e comprovado" nas sessões
  anteriores.

Isso confirma que os relatos anteriores de resolução validaram
corretamente o caminho desktop, mas não o caminho mobile/touch de forma
equivalente — apesar de o próprio relato ter mencionado testes com
eventos touch simulados via script. A suspeita registrada em sessão
anterior (diferença entre evento touch sintético e gesto real de dedo)
permanece a hipótese mais provável, ainda não descartada nem confirmada
tecnicamente.

Pendências diretamente relacionadas, ainda em aberto nesta mesma frente:
- Bug do teclado sumindo ao digitar em abas 2+ no mobile.
- Bug do botão X não encerrando sessão de forma consistente entre a aba
  "main" e as abas seguintes (comportamento deveria ser uniforme,
  encerrando a sessão em todas).

---

## Estado da decisão de fase

Não decidido ainda de forma definitiva. Duas frentes seguem em paralelo:

1. **Estabilização da ponte atual** (scroll mobile, teclado, fechamento de
   aba) — prioridade imediata antes de qualquer mudança de arquitetura
   maior.
2. **Avaliação de pular para Fase 3** (opencode serve como backend real da
   integração Chat Web ↔ Terminal) — considerada tecnicamente viável e
   vantajosa, mas ainda não iniciada como implementação; depende de
   decisão explícita de Washington sobre quando priorizar.

---

## Observação sobre este arquivo

Salvo a pedido de Washington via Claude Web, complementando o roteiro de
fases original com o achado de que a Fase 3 pode ser adotada mais cedo do
que planejado, e registrando o estado atual (bug de scroll mobile
persistente, PC funcionando) como contexto para a próxima sessão de
trabalho no terminal.
