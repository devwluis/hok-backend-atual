# ADENDO_SESSAO_20260910_Card_DeepSeek_Edicao_Total.md

## Resumo

Adicionada UI de edição inline do "Total carregado" no card Créditos DeepSeek (Settings). Antes o ajuste do baseline só existia via curl em `POST /deepseek/credits/baseline`. Agora um ícone de lápis abre um campo inline no próprio card, salva via o endpoint existente e atualiza o card automaticamente. Backend NÃO foi alterado (endpoint já existia).

## Implementação

### Frontend — `src/hooks/use-deepseek-credits.ts`
Nova função `setBaseline(totalLoaded: number)` no retorno do hook: POST em `${serverUrl}/deepseek/credits/baseline` com `{total_loaded}` + header `X-Hok-Token`; em caso de erro lança exceção; em sucesso chama `refresh()`. Retorno agora `{ data, loading, error, refresh, setBaseline }`.

### Frontend — `src/components/screens/SettingsScreen.tsx`
- `CreditGridCell` ganhou prop opcional `action?: ReactNode` (renderizada junto ao label; não afeta os cards OpenRouter/OpenCode).
- Ícone `Pencil` na célula "Total carregado" → abre edição inline abaixo do grid (input numérico pré-preenchido + botões Salvar/Cancelar).
- Estado local: `editingDS`, `dsTotalInput`, `dsSaving`, `dsSaveError`.
- Validação client-side: valor numérico `>= 0` (aceita vírgula como decimal); inválido → erro inline sem chamar a API.
- `Enter` salva, `Esc` cancela. Após salvar, `setBaseline` faz `refresh()` → card atualiza.
- Estilo consistente com o resto da tela (input `rounded-xl border` + foco âmbar, botão âmbar).

## Validação

### Build / typecheck
- `vite build` OK (`index-BsYnq5UC.js`).
- `tsc --noEmit`: apenas os 2 erros PRÉ-EXISTENTES em `SelectionHandles.tsx` (já documentados); nenhum erro nos arquivos alterados.

### Deploy frontend
- Copiado para `/var/www/hok-os` (backup `hok-os.bak_dsedit_*`); nginx serve `index-BsYnq5UC.js` (200).
- Bundle confirmado contendo os textos novos ("Ajustar total carregado", "total já carregado") e o path `credits/baseline`.

### Endpoint (via https, confirmando o proxy nginx da rota)
- `POST /deepseek/credits/baseline {"total_loaded":20.00}` → 200; GET refletiu `total_loaded=20`, `spent=15.33`.
- `POST {"total_loaded":-5}` → **400** "total_loaded deve ser >= 0".
- Restaurado para `total_loaded=4.73` (estado anterior do card): `spent=0.06`.

### Fluxo visual (pencil → editar → salvar → card atualiza)
- Calls subjacentes validadas (endpoint + hook + bundle). Confirmação visual final no navegador pendente do usuário.

## Commit / Push
- Frontend `9b08c16` (feature/preview-tab, hok-frontend-atual): "feat: edição inline do Total carregado no card DeepSeek (override do baseline)" — `use-deepseek-credits.ts` + `SettingsScreen.tsx`. Push OK.
- Backend: nenhuma mudança nesta entrega.

## Status final
- [x] implementação (hook + card inline)
- [x] build + typecheck (sem regressões)
- [x] deploy frontend
- [x] validação do endpoint (salvar, negativo, restauração)
- [x] commit + push
- [x] adendo enviado via webhook n8n
- [ ] confirmação visual no navegador (usuário)