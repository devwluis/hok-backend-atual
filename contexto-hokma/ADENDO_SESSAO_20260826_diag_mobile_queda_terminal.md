# Adendo — Sessão 26/08 (diagnóstico mobile bugs A/B/C + queda do terminal + análise persistência)

## Resumo
Sessão focada em: (1) diagnóstico dos 3 bugs mobile (A/B/C) com instrumentação de telemetria,
(2) análise da queda do terminal Hok Web e (3) análise de persistência do tmux/ttyd/fill.

---

## 1. Diagnóstico dos bugs A/B/C (em andamento)

### Estado real de produção confirmado
- Backend `hokma` = 24/08 15:07 (sem rota diag).
- Frontend `/var/www/hok-os/assets/index-C0wBOc0x.js` (25/08 19:17) **não tem nenhuma**
  das correções de 26/08: 0 ocorrências de `tuiWheelMode`, `mouse_any`, `sbMouse`,
  `gestureTouchCount`, `term-gesture-diag`. O chip de diagnóstico do adendo anterior
  **nunca entrou em produção**.

### Instrumentação nova (26/08 v2)
- Buffer circular `window.__TERM_DIAG` capturando a cadeia inteira:
  `touchstart` → `swipe` → `sbApi` (request + response + erro) → `touchend`.
- Painel flutuante toggleável (botão ↺, só mobile) com botões **copiar** / **postar** / **limpar**.
- Backend: nova rota `POST /terminal/ttyd/diag` (mesma autenticação de token efêmero
  do scroll), salva JSON em `/tmp/hok-term-diag/`.

### Teste do backend isolado
```
{"file":"/tmp/hok-term-diag/hok-ttyd_20260826_191134_b.test123.json","status":"ok"}
```

### Deploy do diagnóstico (apenas observação, sem mudança de comportamento)
- Frontend: `index-CPNe_1OO.js` servido, `index.html` corrigido, bundle antigo removido.
- Backend: `systemctl stop hokma && cp hokma_test hokma && systemctl start hokma`
  (binário backupado como `hokma.bak_pre_diag_20260826`).
- Ambos confirmados ativos em produção.

### Próximo passo
Aguardando coleta do usuário com dedo real nos 3 cenários (A/B/C).

---

## 2. Queda do terminal Hok Web (19:52) — não foi minha ação

### Cadeia real
- Sessão tmux `hok-ttyd` saiu com **exit code 0** em 19:52:44. Log do ttyd:
  `started process, pid: 3503458` → `process exited with code 0, pid: 3503458`
  → `WS closed from 127.0.0.1, clients: 0`.
- O ttyd 7681 (PID 3233052) **sobreviveu** e está ativo.
- A sessão "t1" (hok-terminal-1) sumiu antes, sem trace no log.

### Nenhum comando meu causou
Meus comandos foram ls/find/grep/read/edit/cp/vite build/systemctl hokma/curl/go build.
Nenhum matou tmux, ttyd ou sessões. Sem OOM killer no syslog recente
(o único foi um `node` em outro contexto, há dias).

### Estado real dos serviços
| Serviço | Estado | PID | Observação |
|---|---|---|---|
| `hokma.service` | active (19:13:49) | 3499835 | com rota diag |
| `hok-terminal.service` (ttyd 7681) | active (24/08 11:14) | 3233052 | ttyd vivo, sem sessão acoplada |
| ttyd 7789 | rodando | 3233998 | bash |
| ttyd 7682 | rodando (10:20) | 3452926 | hok-terminal-2 |
| tmux server | vivo | 3345099 (PPID=1) | 2 sessões restantes |

### Recuperação
Ao reconectar, o wrapper `tmux-tab.sh` roda `tmux new-session -A -s hok-ttyd` →
**cria sessão vazia nova**. A sessão anterior não é recuperável.

---

## 3. Análise de persistência (proposta, nada aplicado)

### Camadas separadas
| Camada | Sobrevive? | Como |
|---|---|---|
| tmux server | Sim (PPID=1, 2 dias) | Sessões desanexadas sobrevivem |
| Processo fill (bash/opencode/claude) | Não | Morre com o ttyd |
| Estado da conversa | Não | Vive só na memória do fill |

### `Restart=always` já existe
Já está em `hok-terminal.service` (`Restart=always, RestartSec=3`). Faz o ttyd voltar,
mas **vazio** — o wrapper `-A` cria sessão nova, não restaura. Não resolve o contexto.

### Persistir transcript do opencode/Claude Code
Depende de suporte interno de cada ferramenta. O tmux sozinho não pode restaurar
estado interno de um processo que morreu.

### Recomendação (trade-offs)
- **Opção A** — tornar o fill independente do ttyd (`tmux new-session -d -s hok-ttyd`
  + ttyd anexa à sessão existente). Se o ttyd cair, a sessão tmux sobrevive e o
  opencode continua rodando; ao reconectar, vê o estado exato. Baixo risco.
  Não resolve crash do próprio opencode.
- **Opção B** — dump periódico de contexto em disco + auto-retomada. Ideal mas
  depende de cada ferramenta.
- **Opção C** — apenas monitoramento. Não resolve nada.

**Recomendação: Opção A. Nada aplicado — aguarda aprovação.**

---

## 4. Worktree "mulheres-na-cadeia"
Branch paralelo em `/root/.claude/worktrees/mulheres-na-cadeia` (commit `1f3942e5c`,
criado 26/08 19:37), 267 arquivos, +27k linhas em relação a clean_master.
Nenhum processo a usa. Sem relação com a queda do terminal.

---

## Regras cumpridas
- Backup antes de cada edição (`TerminalTTYDScreen.tsx.bak_20260826_diag2`;
  `hokma.bak_pre_diag_20260826`).
- Build isolado (`go build -o hokma_test .`, `vite build`) antes do deploy.
- Deploy do diagnóstico apenas com aprovação explícita.
- Nenhuma mudança de infraestrutura aplicada sem aprovação.