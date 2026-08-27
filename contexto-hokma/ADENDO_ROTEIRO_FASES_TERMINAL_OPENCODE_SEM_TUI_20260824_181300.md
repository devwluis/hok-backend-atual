# Adendo — Hok OS Pessoal: Roteiro de Fases (Terminal/OpenCode sem TUI + Drive como contexto)
**Origem:** Claude Web (chat)
**Data/hora:** 24-08-2026_181300

---

## Contexto

Este adendo consolida a discussão sobre alternativas ao gate TUI da ponte
Chat Web ↔ Terminal, e organiza a implantação em 3 fases. Escopo: projeto
Hok OS **pessoal/solo** (o Hok SaaS para terceiros é projeto separado, ver
ADENDO_ARQUITETURA_SAAS_INTEGRACAO_OPENCODE_HOK_20260824_173500.md e
ADENDO_CHECKLIST_DEFINICAO_PRONTO_HOK_PESSOAL_20260824_175800.md).

Problema de origem: o gate TUI da ponte Chat→Terminal (tmux send-keys)
bloqueia injeção quando há processo em foreground (ex.: opencode interativo).
Isso é proteção correta contra conflito de digitação, mas impede "conversar
com o opencode" via Chat Web quando ele já está rodando na sessão do
terminal pessoal.

Alternativas discutidas, da mais simples à mais robusta:
1. opencode em modo não-interativo (forceOpenCode já existe, sem TUI).
2. Sessão tmux dedicada só para o Hok Web (separada da sessão manual).
3. opencode como serviço com API própria / MCP real (fase 3 do adendo de
   integração total, 24/08).

Decisão: seguir pela Opção 1, usando o Google Drive (conta
gestordeanunciosbr@gmail.com, já com acesso via n8n) como memória externa
entre chamadas — reaproveitando o padrão já existente de ler/escrever
adendos em CaixaPreta-Hok, em vez de depender de uma feature nativa de
sessão persistente do opencode (que pode nem existir).

---

## Fluxo proposto (Opção 1 + Drive como contexto)

1. Chat Web envia prompt via `forceOpenCode` (modo não-interativo, sem TUI).
2. Antes da chamada, o backend lê o(s) adendo(s) mais recente(s) relevante(s)
   do Drive (via credencial n8n já existente) e injeta como contexto no
   prompt enviado ao opencode.
3. Opencode processa e responde/executa (sem disputa de TUI, pois não há
   sessão interativa envolvida).
4. Ao final, o backend (ou o próprio opencode, orientado a isso) escreve um
   novo adendo no Drive com o resultado — que vira o contexto da próxima
   chamada.

Trade-offs conhecidos, aceitos nesta decisão:
- Latência extra por chamada (leitura do Drive antes de rodar).
- Necessário controlar tamanho do contexto (não acumular histórico
  ilimitado — usar últimos N adendos ou resumo, não o histórico inteiro).
- Não é sessão real: estado interno do opencode (arquivos abertos, cache)
  não persiste entre chamadas, só o que está registrado em texto.
- Bom encaixe para o padrão de uso real hoje (tarefas grandes, documentadas,
  formato pedido→execução→adendo) — não para bate-papo rápido contínuo tipo
  chat, que ficaria arrastado pela leitura/escrita no Drive a cada troca.

---

## FASE 1 — Curto prazo (implantar inicialmente)

Objetivo: fechar as pendências já mapeadas do adendo de integração terminal
(bloqueantes) e validar a Opção 1 (forceOpenCode + Drive como contexto) para
uso real, sem grandes mudanças de arquitetura.

- [ ] Corrigir o bug do modo Plan no opencode (Item 1 do adendo de 24/08,
      145221) — bloqueante, trava real testada e confirmada.
- [ ] Testar `forceOpenCode` (modo não-interativo) como canal de "conversa"
      via Chat Web, sem depender de sessão TUI — confirmar que não aciona o
      gate TUI e que a resposta chega limpa.
- [ ] Implementar leitura do último adendo relevante do Drive antes de cada
      chamada `forceOpenCode` (reaproveitando a credencial n8n existente).
- [ ] Implementar escrita automática de um adendo de resultado ao final de
      cada chamada (ou orientar o opencode a fazer isso via prompt).
- [ ] Definir regra simples de limite de contexto (ex.: só o último adendo,
      ou os últimos N, nunca a pasta inteira) para evitar estouro de prompt.
- [ ] Seguir com os demais itens do adendo de integração terminal: seletor
      de modelo (`hok-model`), allowlist do modo autonomous, wrapper de modo
      de sessão — nesta ordem, conforme já definido no adendo de 145221.
- [ ] Fechar os critérios de segurança pendentes do checklist de "definição
      de pronto" (ver ADENDO_CHECKLIST_DEFINICAO_PRONTO_HOK_PESSOAL_
      20260824_175800.md), incluindo substituir o fluxo ad-hoc de extração
      de credencial do n8n_db.sqlite por integração via node nativo.

## FASE 2 — Médio prazo

Objetivo: melhorar a qualidade da "conversa sem TUI" e reduzir os
trade-offs aceitos na Fase 1, sem ainda migrar para arquitetura de serviço.

- [ ] Avaliar Sessão tmux dedicada (Opção 2) para casos em que a resposta
      via forceOpenCode não for suficiente (ex.: quando precisar ver
      execução interativa em tempo real sem esperar o ciclo
      leitura-Drive→chamada→escrita-Drive).
- [ ] Otimizar o contexto do Drive: em vez de reler o adendo inteiro a cada
      chamada, considerar um resumo condensado/rolling summary, atualizado
      incrementalmente, para reduzir latência e tamanho de prompt.
- [ ] Medir e documentar a latência real do ciclo leitura→chamada→escrita,
      decidir se é aceitável para o padrão de uso ou se justifica avançar
      para Fase 3 antes do previsto.
- [ ] Revisar se `opencode` ganhou alguma flag nativa de sessão persistente
      (`--session-id`, `--continue` ou equivalente) em atualizações do
      binário — se sim, avaliar substituir parte do fluxo Drive por isso.
- [ ] Consolidar aprendizados desta fase em adendo próprio antes de avançar.

## FASE 3 — Longo prazo

Objetivo: arquitetura robusta, sem depender de simulação de teclado nem de
ciclos de leitura/escrita em Drive para manter contexto — sessão real e
contínua entre Chat Web e opencode.

- [ ] Avaliar se `opencode` suporta modo daemon/serviço com API própria
      (stdin/stdout estruturado ou servidor HTTP local) — verificar
      documentação/flags do binário instalado.
- [ ] Desenhar a integração via essa API (ou via MCP, conforme já registrado
      como "fase 3" no adendo de integração total de 24/08) — sincronização
      viva bidirecional terminal ↔ Chat Web, sem gate TUI e sem depender do
      Drive como memória externa.
- [ ] Definir como isso convive com o uso manual do terminal (você
      continuar podendo abrir e usar o opencode interativamente, sem
      conflito com chamadas do Chat Web).
- [ ] Reavaliar, neste ponto, se compensa manter o padrão de adendo no
      Drive como log histórico (auditoria/documentação) mesmo que não seja
      mais o mecanismo de contexto operacional.

---

## Observação sobre este arquivo

Salvo a pedido de Washington via Claude Web, na pasta CaixaPreta-Hok, como
roteiro de fases para evoluir a interação Chat Web ↔ opencode sem o gate TUI,
usando o Google Drive (gestordeanunciosbr@gmail.com, acesso via n8n) como
memória de contexto na Fase 1. Este roteiro é do projeto Hok OS pessoal —
não se aplica ao Hok SaaS, que é projeto separado a ser iniciado somente
após os critérios do checklist de "definição de pronto" serem atendidos.
