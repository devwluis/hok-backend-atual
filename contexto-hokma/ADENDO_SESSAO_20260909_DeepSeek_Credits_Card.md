# ADENDO_SESSAO_20260909_DeepSeek_Credits_Card.md

## Resumen

Implementación del card "Créditos DeepSeek" en la pantalla Settings de HOK OS, espejando el card existente de Créditos OpenRouter. Incluye campo de API Key DeepSeek en la sección de conexiones y una nueva ruta backend de consulta de saldo.

## Contexto

El usuario pidió un card de "Créditos DeepSeek" debajo del card "Créditos OpenRouter" existente, siguiendo el mismo patrón visual (Total Carregado / Gasto hasta el momento / Saldo Actual). Durante la investigación se confirmó que la API de DeepSeek (`api.deepseek.com/user/balance`) SÍ expone el saldo de la cuenta, pero NO expone uso histórico por mes/semana/día (eso vive solo en el dashboard web). Por eso el card usa los campos que existen: Carregado (`topped_up_balance`), Bônus concedido (`granted_balance`) y Saldo atual (`total_balance`).

## Cambios

### Backend (Go)
- **Nuevo `deepseek_credits.go`**: handler `handleDeepSeekCredits` con:
  - Auth `X-Hok-Token` (requireHokAuth) + CORS (setCORS)
  - Error 500 si `DEEPSEEK_API_KEY` no está definida; 502 si la llamada falla
  - Llamada autenticada `GET https://api.deepseek.com/user/balance` con `Bearer $DEEPSEEK_API_KEY`
  - Helper `fetchDeepSeekJSON` (espelho de `fetchOpenRouterJSON`, timeout 15s)
  - Respuesta: `{balance, currency, granted_balance, topped_up_balance, is_available, source}`
- **Modificado `main.go`**: registro de la ruta `GET /deepseek/credits`

### Frontend (React/Vite)
- **Nuevo `hooks/use-deepseek-credits.ts`**: hook `useDeepSeekCredits()` — espelho del de OpenRouter (GET `/deepseek/credits` con header `X-Hok-Token`, expone `{data, loading, error, refresh}`)
- **Modificado `SettingsScreen.tsx`**:
  - Import del nuevo hook
  - `SERVER_KEY_MAP`: agregado `DeepSeek: "deepseekKey"` (activa el selo "Configurado no servidor" vía `deepseekKeyConfigured` que ya devolvía GET /settings)
  - `KEYS`: agregada entrada `{ k: "DeepSeek", placeholder: "ds_•••", description: "API Key DeepSeek (para el panel de créditos)" }`
  - **Nuevo card "Créditos DeepSeek"** justo debajo del card OpenRouter, usando los helpers existentes `CreditCardHeader` y `CreditGridCell`:
    - **Carregado** → `topped_up_balance`
    - **Bônus concedido** → `granted_balance`
    - **Saldo atual** (destacado) → `total_balance`
    - Línea de detalle: "Moeda: USD · disponible/indisponible"
  - Ícono `Zap` (ya importado en el archivo)

## Endpoint de saldo DeepSeek (confirmado)

```
GET https://api.deepseek.com/user/balance
Authorization: Bearer sk-...
```

Respuesta verificada (09/09/2026):
```json
{
  "is_available": true,
  "balance_infos": [
    {
      "currency": "USD",
      "total_balance": "4.97",
      "granted_balance": "0.00",
      "topped_up_balance": "4.97"
    }
  ]
}
```

**Limitación conocida:** DeepSeek NO expone uso histórico por periodo vía API (`/user/usage` y `/key` devuelven vacío). El campo "Gasto hasta el momento" no es derivable — el card muestra los 3 campos que sí existen.

## Build y tests

- Backend: `go build -o hokma_test .` OK; `go test ./...` OK (48.086s)
- Frontend: `npm run typecheck` — solo errores pre-existentes (SelectionHandles.tsx); `npm run build` (PORT=5173 BASE_PATH=/ NODE_ENV=production) OK — 2125 modules, index-BMAe7BEX.js / index-DITMffOR.css
- Smoke test binario aislado (puerto 8090): GET /deepseek/credits → 200 con saldo real; sin token → 401; OPTIONS CORS → 204 con Allow-Origin: *

## Deploy

- `./deploy.sh` completado: build bcdad6dac74e9e0fe12f7123aefbf14a, tests OK, backup `hokma.bak_20260909_203743`, health check HTTP 200, hashes idénticos
- Frontend: copiado dist/public completo a `/var/www/hok-os` (backup `hok-os.bak_20260909_204016`); verificado que no faltan assets del index.html (JS/CSS con hash)
- Smoke test producción :8082: /deepseek/credits → 200 con saldo; nginx :3002 sirve el JS/CSS nuevos → 200

## Commits

- Backend: `c37d0ff` en `main` (hok-backend-atual) — "feat: rota GET /deepseek/credits para saldo da conta DeepSeek"
- Frontend: `a6a99ec` en `feature/preview-tab` (hok-frontend-atual) — "feat: card Créditos DeepSeek na tela Settings"
- Push: ambos repos actualizados en GitHub (devwluis)

## Pendientes / observaciones

- El card muestra "Saldo" del server (DEEPSEEK_API_KEY del .env), no de la key del cliente del browser — igual que OpenRouter
- Si DeepSeek en algún momento expone uso histórico por API, se puede agregar la línea "Uso en el mes/semana/día" al card sin cambiar la estructura
- Los archivos WIP de sesiones previas (agent_loop.go, self_heal.go, types.go, terminal_routes.go) quedaron sin commitear en el repo backend — no se mezclaron en este commit