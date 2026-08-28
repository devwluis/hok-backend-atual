# Adendo — Fix do opencode.json (config inválida) — 2026-08-20

Correção do erro que impedia o OpenCode de abrir no terminal do HOK OS:
"Configuration is invalid at /root/hokma/backend/opencode.json", antes
mesmo de inicializar.

## 1. Sintoma

Rodar `opencode` no terminal (cwd `/root/hokma/backend`) falhava com:

```
Error: Configuration is invalid at /root/hokma/backend/opencode.json
↳ Expected string | undefined, got null provider.anthropic.options.apiKey
↳ (idem google, meta, mistral, qwen, deepseek, stablelm, mosaic)
↳ Expected { type: "local", ... } | { type: "remote", ... } | object, got true mcp.enabled
↳ … got ["npx","-y","@playwright/mcp"] mcp.command / got "local" mcp.type
```

## 2. Causa raiz confirmada

- **Arquivo de projeto desatualizado** (`/root/hokma/backend/opencode.json`):
  - `provider.<nome>.options.apiKey: null` para 8 provedores — o schema do
    OpenCode exige `string | undefined`, nunca `null` explícito;
  - bloco `mcp` em formato ANTIGO do schema (`enabled: true` +
    `command`/`type` soltos no topo) — o formato atual exige um objeto
    **nomeado por servidor** (`{ "type": "local", "command": [...] }`).
- **O gerador Go NÃO era a origem**: `propagateToOpenCodeConfig`
  (model_propagate.go) só escreve `~/.opencode/opencode.json`, que estava
  limpo e válido (`{ "$schema", "model" }`) — ele nunca grava `apiKey` nem o
  bloco `mcp`. O `backend/opencode.json` é config de projeto (cwd do
  terminal) criada à parte.

## 3. Correções

1. **`opencode.json` (fix do arquivo)**:
   - Removido o bloco `provider` inteiro — sem chave de API configurada, o
     campo deve ser **omitido** (undefined), não null;
   - `mcp` reescrito para o formato atual:
     ```json
     "mcp": {
       "playwright": {
         "type": "local",
         "command": ["npx", "-y", "@playwright/mcp"]
       }
     }
     ```
2. **`model_propagate.go` — hardening na origem (anti-regressão)**:
   - Nova `sanitizeOpenCodeConfig()`, chamada no `propagateToOpenCodeConfig`
     antes de reescrever o arquivo:
     - remove `provider.<id>.options.apiKey` quando `null` (e o bloco
       `options` vazio), preservando demais chaves;
     - converte bloco `mcp` legado (`enabled/command/type` soltos) para o
       formato nomeado (`"playwright": { type, command }`).
   - Assim, qualquer propagação futura de uma config poluída não regenera a
     invalidação.

## 4. Validação

- **Real (não só linter)**: `opencode run` no cwd `/root/hokma/backend`
  inicializa **sem** "Configuration is invalid" (mostra o motor/modelo e
  entra no fluxo; o "auto-rejecting" de `bash` é o comportamento normal de
  `permission.bash: ask` em modo não-interativo).
- **Saneador real testado** (teste temporário removido após rodar): PASS —
  `apiKey:null` removido, chave `extra` preservada, `mcp` → servidor nomeado.
- Backend: `go build` ✓, `go vet` ✓, `go test ./...` → exit 0.

## 5. Deploy e commit

- Binário: backup `hokma.bak_oc_*` → stop → swap → start (PID 2754303),
  health OK, hash binário = hash testado (`4a56d3eb…`).
- Commit `eecf363` "fix(opencode): config de projeto invalida (apiKey null +
  mcp schema antigo) + saneador no gerador" → push
  `devwluis/hok-backend-atual` (model_propagate.go + opencode.json).
- O fix do `opencode.json` vale imediatamente no terminal (config de projeto
  lida do disco); o binário com o saneador foi deployado.

## 6. Nota

- Backups: `opencode.json.bak_oc_20260820_202523`,
  `model_propagate.go.bak_oc_20260820_202523`.
- Ainda em aberto (separado, sem deploy de fix): diagnóstico da queda de
  conexão residual do terminal — logging de close code/reattach/procAlive no
  ar (commits c413802/b988d67) aguardando o próximo log real para concluir
  se é comportamento normal ou regressão.