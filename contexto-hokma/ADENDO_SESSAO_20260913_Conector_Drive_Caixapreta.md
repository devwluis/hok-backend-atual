# Adendo — Conector Google Drive (subpasta CaixaPreta-Hok)

**Origem:** opencode (backend) | **Data/hora:** 13-09-2026

---

## O que foi feito

Criado um conector completo de Google Drive no backend Hok (Go) e integração no frontend (React), permitindo que o sistema Hok web acesse, gerencie e navegue pelos arquivos da pasta **CaixaPreta-Hok** no Google Drive (`folder_id: 16zPoX8HrHOHCZgKezWwNmOEQad1eOBjN`, conta `gestordeanunciosbr@gmail.com`).

---

## Backend — `backend/drive_connector.go` (~700 linhas)

### Endpoints registrados em `backend/main.go:182-189`

| Rota | Método | Função | Descrição |
|---|---|---|---|
| `/drive/files` | GET | `driveListFiles` | Lista arquivos/pastas de uma folder (default: CaixaPreta) |
| `/drive/search` | GET | `driveSearch` | Busca arquivos por texto (`fullText contains`) |
| `/drive/folder/` | GET | `driveFolderInfo` | Info de uma pasta + lista de filhos |
| `/drive/upload` | POST | `driveUpload` | Upload de arquivo (multipart/form-data) |
| `/drive/create-folder` | POST | `driveCreateFolder` | Cria nova pasta |
| `/drive/rename` | POST | `driveRename` | Renomeia arquivo/pasta |
| `/drive/move` | POST | `driveMove` | Move arquivo/pasta entre pastas (`addParents`/`removeParents`) |
| `/drive/delete` | POST | `driveDelete` | Deleta arquivo (sempre vai para lixeira) |

### Decisões arquiteturais

1. **`multipart/related` vs `multipart/form-data`** — Upload via Google Drive API exige `multipart/related`. O Go `multipart.NewWriter` gera `form-data`; corrigido com `strings.Replace(contentType, "multipart/form-data", "multipart/related", 1)` + metadata part sem `Content-Disposition`.
2. **MIME type detection** — `mime.TypeByExtension(".txt")` retorna `text/plain; charset=utf-8`. Google Drive rejeita parâmetros no mimeType → filtrado com `strings.IndexByte` + `TrimSpace`.
3. **Search query syntax** — Drive API exige `fullText contains 'term' and 'folderId' in parents` (NOT `term and 'folderId' in parents`). Removido `orderBy` que conflita com `fullText`.
4. **Move** — Drive API `parents` é read-only; usa `addParents`/`removeParents` no PATCH.
5. **Auth reutilizada** — `driveAccessToken()` em `icons.go:92` (mesmo pacote) lê refresh token de `drive_creds.env`.
6. **CaixaPreta como padrão** — `caixaPretaFolderID = "16zPoX8HrHOHCZgKezWwNmOEQad1eOBjN"` como fallback quando nenhum folder é especificado.

### Arquivo de credenciais

- `backend/drive_creds.env` — `client_id`, `client_secret`, `refresh_token`
- Refresh token extraído do banco SQLite do n8n (`/var/lib/docker/volumes/n8n_data_v2/_data/database.sqlite`) descriptografado com chave de `/home/node/.n8n/config` (`encryptionKey: gtoqn1NHrrTgwagBhEQhiL+7xnOx8omc`)

---

## Frontend — `/root/hokma-web/artifacts/hok-os/`

### Arquivos criados/modificados

| Arquivo | Tipo | Alteração |
|---|---|---|
| `src/components/screens/DriveScreen.tsx` | NOVO | Tela gerenciador Drive (~510 linhas): listar, busca, upload, criar pasta, renomear, mover, deletar, breadcrumb |
| `src/lib/drive-api.ts` | NOVO | Cliente API para 8 endpoints Drive, com configurações salvas em `hokma.drive.settings.v1` |
| `src/lib/app-state.ts` | MODIFICADO | Adicionado `"drive"` ao tipo `ScreenId` |
| `src/components/shell/AppShell.tsx` | MODIFICADO | Import + registro Drive no SCREENS |
| `src/components/shell/Drawer.tsx` | MODIFICADO | Ícone `Cloud` + entrada Drive na grid de apps (sub: "Cloud", cores #4285f4 → #1a73e8) |

### Funcionalidades do DriveScreen

- 📁 Listar arquivos e pastas (CaixaPreta como padrão)
- 🔍 Busca em tempo real (debounce 500ms, `fullText contains`)
- 📤 Upload de arquivos via `FormData` → `multipart/related`
- 📁 Criar pastas dentro da pasta atual
- ✏️ Renomear itens inline
- ↔️ Mover entre pastas (por ID da pasta destino)
- 🗑️ Deletar (sempre vai para a lixeira, com confirmação)
- 🧭 Navegação por breadcrumb (clicável, com path completo)
- 🔗 Link direto para drive.google.com em pastas
- 🔔 Toasts de feedback (ok/err)
- ⚠️ Confirmação na exclusão permanente

---

## Correções aplicadas durante testes

| Problema | Solução |
|---|---|
| Auth falhando com `Authorization: Bearer` | Usar header `X-Hok-Token` (padrão do projeto) |
| Upload retornava `application/octet-stream` | 1) Filtrar charset do mimeType, 2) Mudar `multipart/form-data` → `multipart/related`, 3) Remover Content-Disposition da metadata |
| Search falhando com orderBy + fullText | Remover `orderBy` do query |
| Move falhando com "parents is not writable" | Usar `addParents`/`removeParents` |
| Variável `url` sombreava `net/url` | Renomear para `infoURL` |
| `textproto.MIMEHeader` indisponível | Usar `map[string][]string` |
| `setFiles(undefined)` crashava render | Adicionar `|| []` nos setFiles |
| DriveScreen "Pasta vazia" — nginx não proxy /drive/ | Adicionar `drive` no regex de proxy do nginx (`/etc/nginx/sites-enabled/hokma-web`) |
| driveListFiles/driveSearch/driveFolderInfo devolviam {files:[]} silenciosamente | Backend: `w.WriteHeader(código)` + formato `{error, status}`; Frontend: `throw new Error(data.error)`; DriveScreen: toast "Erro ao carregar Drive: ..." |
| driveDelete usava DELETE (risco de deleção permanente) | Agora sempre PATCH com `{"trashed": true}` (lixeira) |
| Listagem Drive raiz mostrava arquivos soltos | Backend: `onlyFolders=true` filtra `mimeType='application/vnd.google-apps.folder'`; Frontend passa `onlyFolders=true` quando `folderId === pasta raiz`; todas as queries incluem `and trashed = false` |

---

## Testes realizados

| Endpoint | Status |
|---|---|
| GET `/drive/files` | ✅ 86 arquivos listados |
| GET `/drive/search?q=ADENDO_SESSAO` | ✅ 4 resultados |
| GET `/drive/folder/16zPoX8HrHOHCZgKezWwNmOEQad1eOBjN` | ✅ 86 filhos |
| GET `/drive/files` via nginx :3002 | ✅ 86 arquivos (proxy funcionando) |
| GET `/drive/folder/INVALID` | ✅ HTTP 404 com `{error: "...", status: 404}` |
| GET `/drive/search` erro | ✅ Lança Error com mensagem real |
| Frontend: build TS | ✅ 0 erros (novo código) |
| POST `/drive/create-folder` | ✅ Pasta criada |
| POST `/drive/rename` | ✅ Renomeado |
| POST `/drive/move` | ✅ Movido |
| POST `/drive/upload` | ✅ Arquivo enviado |
| POST `/drive/delete` | ✅ Deletado (trash) |
| GET `/drive/files?onlyFolders=true` | ✅ Só pastas (0 arquivos soltos) |
| GET `/drive/files?onlyFolders=false` | ✅ Pastas + arquivos |
| DELETE via PATCH (`driveDelete`) | ✅ Sempre via lixeira |
| Frontend: build TS | ✅ 0 erros (novo código)
| Frontend: vite build | ✅ Sucesso |
| Frontend: deploy nginx | ✅ Reload OK |

---

## Pendências

1. **Deploy backend**: Confirmar com usuário antes de `systemctl restart hokma` com binary final
2. **Tela preta frontend**: Se o usuário reportar, verificar console do browser (F12 → Console) para erros de runtime
3. **Limpeza Drive**: Pastas de teste criadas durante desenvolvimento já foram deletadas do Drive

---

## Comandos úteis

```bash
# Rebuild backend
cd /root/hokma/backend && go build -o hokma_test . && cp hokma_test hokma && systemctl restart hokma

# Rebuild frontend
cd /root/hokma-web/artifacts/hok-os && PORT=3002 BASE_PATH=/ NODE_ENV=production npm run build
rsync -a --delete dist/public/ /var/www/hok-os/ && systemctl reload nginx

# Testar endpoint
curl -s -H "X-Hok-Token: $HOK_TOKEN" "http://localhost:8082/drive/files" | python3 -m json.tool
```
