# Adendo — Decisão: avançar para Fase 3 (opencode serve, sem TUI)

**Origem:** Claude Web (chat) **Data/hora:** 27-08-2026\_061508 **Referência:** ADENDO\_ROTEIRO\_FASES\_TERMINAL\_OPENCODE\_SEM\_TUI\_20260824\_181300.md, ADENDO\_ATUALIZACAO\_ROTEIRO\_FASES\_PULAR\_PARA\_FASE3\_20260826\_070336.md, Adendo 2708 (terminal tela preta \+ bug do segundo toque).

---

## Motivo da decisão

O adendo de 27/08 03:48 confirmou a causa raiz do bug recorrente de "terminal preso em reconectar" \+ travamento do teclado mobile: burst de input (\~4KB+, típico de recomposição de IME/paste) faz o `opencode` (renderer OpenTUI) entrar em spin de 100% CPU e sair com exit code 0, derrubando em cascata pane → sessão tmux → processo fill → WebSocket. Issues confirmadas upstream no próprio opencode (\#2115, \#38932, \#33399).

Reverter para commits estáveis anteriores não resolveu — confirma que a causa não está no código do Hokma (frontend/backend), e sim na arquitetura atual (TUI do opencode rodando dentro de PTY/tmux, exposto via iframe cross-origin no Chat Web mobile).

Decisão de Washington: parar de aplicar mitigações pontuais nessa arquitetura e avançar diretamente para a Fase 3 do roteiro de 24/08 (`opencode serve` como backend real via API HTTP), em vez de continuar investindo em fixes cosméticos no `TerminalTTYDScreen.tsx`.

## Por que Fase 3 resolve na raiz (não só mitiga)

- Elimina o TUI inteiro do caminho do Chat Web: sem xterm.js, sem PTY, sem OpenTUI renderizando no celular, sem gesto de toque disputando foco com iframe cross-origin.  
- Resolve de tabela outros bugs crônicos já registrados nos adendos de 22-26/08: scroll mobile quebrado, teclado sumindo, duplicação de conteúdo, timeout sem captura de output no `tryTerminalExec` — todos sintomas da mesma arquitetura PTY-via-iframe.  
- `/session/{id}/summarize` nativo resolve o estouro de contexto que motivava o desenho de Fase 1 (Drive como memória externa).  
- Já confirmado tecnicamente viável no adendo de 26/08 (API REST completa via `/doc`, OpenAPI 3.1.0, auth via `OPENCODE_SERVER_PASSWORD`).

## Plano de implementação

**1\. Subir o serviço**

- `opencode serve` como serviço systemd próprio (padrão de `hokma.service`/`hok-terminal.service`), `Restart=always`.  
- `OPENCODE_SERVER_PASSWORD` no `.env` do servidor.  
- Porta interna (`127.0.0.1:PORTA`), sem exposição via nginx.

**2\. Módulo de integração no backend Go**

- Novo cliente HTTP (ex. `opencode_serve_client.go`) falando com `/session`, `/session/{id}/message`, `/session/{id}/prompt_async`, `/session/{id}/summarize`, `/event` (SSE).  
- Substitui `tryTerminalExec`/`findActiveSession` (origem do Bug 1 do adendo de 22/08 — captura de output via marcador de timestamp no PTY).

**3\. Sessão persistente real**

- Uma sessão opencode por `conv_id`, reaproveitando (ou estendendo) a tabela `session_mode` já desenhada no adendo de 24/08 para guardar o `session_id` do opencode.  
- Resumo de contexto nativo via `/session/{id}/summarize` — elimina o ciclo leitura/escrita no Drive da Fase 1\.

**4\. Frontend — descontinuar o iframe/TUI no Chat Web**

- `TerminalTTYDScreen.tsx` deixa de ser o canal principal do Chat Web (permanece disponível só para acesso manual/emergencial, como já decidido no adendo de 22/08 — Termius via SSH direto continua sendo a ferramenta principal para trabalho pesado).  
- Chat Web passa a consumir `/event` (SSE) ou polling para streaming de resposta — sem xterm.js.

**5\. Convivência com uso manual (SSH/Termius)**

- Opção A (recomendada para começar): sessão do `opencode serve` isolada da sessão manual do terminal — processos separados, zero conflito, sem contexto compartilhado.  
- Opção B (avaliar depois): mesma sessão via `--session-id` também usável pelo CLI manual — contexto compartilhado, requer validar se o CLI aceita anexar a uma sessão criada pelo server.

**Ordem sugerida de execução:**

1. Subir `opencode serve` isolado, validar autenticação e rotas principais via curl, sem tocar produção.  
2. Escrever o cliente Go \+ endpoint de teste (`/opencode/session` etc.), validado em porta separada (padrão já usado nas sessões anteriores).  
3. Só depois trocar o frontend do Chat para consumir a nova rota, mantendo o terminal antigo intacto até validação em produção.  
4. Decommission gradual do `TerminalTTYDScreen.tsx` como canal principal do Chat Web.

## Observação sobre este arquivo

Salvo a pedido de Washington via Claude Web, na pasta CaixaPreta-Hok, registrando a decisão de avançar para a Fase 3 do roteiro de 24/08 e o plano de implementação combinado, para orientar a próxima sessão de trabalho no terminal (Claude Code / opencode via SSH).  
