# ADENDO_SESSAO_20260910_Card_DeepSeek_Total_Carregado.md

## Resumo

Correção do card "Créditos DeepSeek" (tela Settings). A API DeepSeek NÃO expõe "total carregado" histórico — só o saldo atual (`total_balance`, `topped_up_balance`, `granted_balance`, todos RESTANTES). Por isso o card antigo rotulava `topped_up_balance` como "Carregado", o que era enganoso (cai com o consumo). Agora o backend mantém snapshots locais para derivar **Total carregado** (fixo) e **Gasto até o momento**, e o card foi alinhado ao layout do OpenRouter.

## Solução

### Persistência (SQLite)
Nova tabela `deepseek_balance_snapshots` (em `db.go`):
```
id, ts, currency, balance, topped_up, granted, total_loaded, spent
```

### Algoritmo (`deepseek_credits.go` → `deepSeekComputeState`)
- **1º snapshot**: `total_loaded = balance` (baseline automático).
- **Chamadas seguintes**: salto POSITIVO de saldo = recarga → `total_loaded += delta`; consumo (saldo cai) NÃO altera o total.
- `spent = max(0, total_loaded - balance)`.
- **Throttle**: grava snapshot só se o saldo mudou (>0.0001) OU passou >1h.
- Bônus (`granted`) incluído no `total_loaded` (hoje 0 — sem efeito prático, cobre o futuro).

### Endpoint de override manual (saída de emergência)
```
POST /deepseek/credits/baseline   {"total_loaded": <float>}
```
Força o `total_loaded` de referência (caso a detecção automática erre).

### Frontend
- `use-deepseek-credits.ts`: tipo com `total_loaded`/`spent`.
- `SettingsScreen.tsx`: 3 células idênticas ao card OpenRouter — **Total carregado** (neutro) / **Gasto até o momento** (vermelho) / **Saldo atual** (verde). Bônus movido para a linha de detalhe.

## Validação

### Binário isolado (porta 9911, DB em /tmp — regra de processo da sessão passada)
| Teste | Resultado |
|---|---|
| 1ª chamada | `total_loaded=4.78`, `spent=0` (baseline) ✓ |
| 2ª chamada | estável, sem snapshot duplicado (throttle) ✓ |
| Override `total_loaded=10` | `total_loaded=10`, `spent=5.22` ✓ |
| Recarga (saldo 2.00→4.78) | `total_loaded` 10→12.78 ✓ |
| Consumo (saldo 20→4.78) | `total_loaded` fica 20, `spent` sobe ✓ |

### Produção (smoke)
- `GET /deepseek/credits`: retorna `total_loaded`, `spent`, `balance`, `granted_balance` ✓
- `POST /deepseek/credits/baseline {}` → **400** "campo total_loaded obrigatorio" ✓

## Bug encontrado e corrigido durante a sessão
A 1ª versão do endpoint aceitava `{}` (campo ausente virava `0`) e **resetava o baseline para 0**. Corrigido com `*float64` + validação de obrigatoriedade (retorna 400 se ausente). O snapshot ruim criado em produção (`total_loaded=0`) foi removido do `memory.db`, restaurando o baseline válido (`total_loaded=4.73`).

## Deploy
- Backend `./deploy.sh`: build `0cd5f506`, tests OK, health 200, hashes idênticos.
- Frontend: `pnpm build` + copiado para `/var/www/hok-os` (backup `hok-os.bak_dsbalance_*`); nginx serve `index-TxFAjCA-.js` (200).
- Corrigido também o `deploy.sh`: o health check não tinha retry e deu falso negativo no 1º deploy (porta ainda não aberta). Agora reintenta até 15s.

## Commits
- Backend `f7d095e` (main, hok-backend-atual): "feat: card Créditos DeepSeek com total carregado / gasto / saldo (snapshots locais + baseline override)" — `db.go`, `deepseek_credits.go`, `main.go`.
- Backend `deploy.sh`: "fix: health check do deploy.sh com retry" (arquivo estava untracked).
- Frontend `507832a` (feature/preview-tab, hok-frontend-atual): "feat: card Créditos DeepSeek com Total carregado / Gasto até o momento / Saldo atual".
- Push OK nos dois repos (devwluis).

## Pendência / observação
- Baseline automático começa do saldo ATUAL (~4.73), não do total histórico real. O usuário vai usar `POST /deepseek/credits/baseline {"total_loaded": X}` para ajustar ao valor real já carregado. Após isso, o card mostra o gasto correto e o algoritmo detecta recargas futuras automaticamente.

## Status final
- [x] build isolado + go test OK
- [x] deploy backend (deploy.sh) + frontend
- [x] validação da lógica (baseline/recarga/consumo/override)
- [x] smoke test produção
- [x] corrigido bug do baseline vazio + snapshot de prod limpo
- [x] commits separados + push
- [x] adendo enviado via webhook n8n