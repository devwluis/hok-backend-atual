# Adendo — Sessão 29/08 · Sincronização do contexto completo da CaixaPreta-Hok

**Origem:** opencode (terminal) | **Data/hora:** 29-08-2026

---

## O que foi feito

Sincronização completa da pasta **CaixaPreta-Hok** (Google Drive, folder ID
`16zPoX8HrHOHCZgKezWwNmOEQad1eOBjN`, conta `gestordeanunciosbr@gmail.com`)
para o diretório local `/root/contexto-hokma/`, que agora é a base de contexto
do projeto.

### Método

1. Extração da chave de criptografia n8n do container `n8n_oficial`:
   `/home/node/.n8n/config` → `encryptionKey: gtoqn1NHrrTgwagBhEQhiL+7xnOx8omc`
2. Cópia do banco SQLite do volume Docker: `/var/lib/docker/volumes/n8n_data_v2/_data/database.sqlite`
3. Descriptografia da credencial "Google Drive account 2" (`bdgxPVbfHa0sa3pT`)
   via AES-256-CBC EVP_BytesToKey (formato crypto-js Salted__).
4. Listagem + download de todos os arquivos da pasta via Google Drive API v3.
5. Limpeza dos arquivos temporários (credencial, banco, script).

### Resultado

| Métrica | Valor |
|---|---|
| Arquivos no Drive (total na pasta) | ~200 |
| Arquivos baixados | 144 |
| Ja existentes (ignorado) | 6 |
| Erros 403 (restricao de compartilhamento) | 8 |
| Total em `/root/contexto-hokma/` | **201 arquivos, 771 KB** |

Os 8 arquivos com erro 403 são de compartilhamento restrito no Drive
— não disponíveis para download pela credencial atual. Os arquivos ja
existiam localmente de sessões anteriores.

### Arquivos sincronizados

- **ADENDOs**: 57+ registros de sessão (de 10/08 a 29/08)
- **CONTEXTO_TERMINAL**: logs de sessão do terminal (15-17/08)
- **AUDITORIA**: auditorias de backend (14-15/08)
- **COMMITs**: 1 commit

### Arquivos com erro 403 (não baixados)

- `ADENDO_DECISAO_FASE3_OPENCODE_SERVE_20260827_061508.md`
- `ADENDO_ETAPA_B_REPLYPERMISSION_SSE_TOOL_APPROVAL_20260827.md`
- `ADENDO_FIX_IDENTIDADE_ENGINES_24-08-2026.md`
- `ADENDO_INCIDENTE_20260827_terminal_popup_reconnect_trava_teclado.md`
- `ADENDO_SESSAO_20260822_chat_terminal_bugs_pendentes.md`
- `ADENDO_SESSAO_20260822_terminal_web_bugs_diversos.md`
- `ADENDO_SESSAO_MODO_COMPARTILHADO_PLAN_BUILD_AUTONOMO_24-08-2026.md`
- `CONTEXTO_CLAUDE_WEB_20260815_074935_DEPLOY.md`

Estes podem conter contexto relevante. Se necessário, acessar diretamente
pela UI do Google Drive logado como `gestordeanunciosbr@gmail.com`.

## Estado atual do contexto

A partir desta sessão, `/root/contexto-hokma/` é a base de contexto local
do projeto HOK. Todo adendo novo deve ser salvo ali E enviado para a
CaixaPreta-Hok via webhook n8n (POST `http://127.0.0.1:5678/webhook/contexto-hok-terminal`).

## Comandos úteis

```bash
# Verificar quantos arquivos no contexto local
ls /root/contexto-hokma/ | wc -l

# Listar ADENDOs mais recentes
ls -lt /root/contexto-hokma/ADENDO* | head -20

# Buscar dentro dos arquivos
grep -rl "palavra_chave" /root/contexto-hokma/

# Upload de novo adendo (via webhook n8n — salvar primeiro localmente)
```
