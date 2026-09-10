# ADENDO — Deploy: recuperação do auto-clear do transcript do terminal

**Sessão:** 2026-09-10
**Status:** ✅ Commit + deploy + smoke test em produção concluídos.

---

## 1. Resumo Executivo

A feature de auto-clear do transcript do terminal (transcript > 1.5 MiB grava o marcador `.clear` automaticamente, equivalendo ao "Apagar") foi **recuperada da branch `hok-backend-atual` e reimplantada em produção**. Ela havia sido removida por engano na main pelo commit de checkpoint `e09f6eb` (10/09), que deixou a constante `termLogAutoClearBytes` órfã (definida, nunca usada).

## 2. Origem e Causa da Perda

| Item | Detalhe |
|---|---|
| Introduzida originalmente | `3dee9a2` (main, 03/09) / `48dafad` (branch, 01/09) |
| Removida por | `e09f6eb` — "checkpoint: pre-fix deepseek-native fallback" (10/09 07:49) |
| Evidência em produção | Auto-clear ativo nos binários de 03/09 02:21 a 09/09 09:35; ausente a partir de 09/09 19:06 |
| Recuperada de | branch `origin/hok-backend-atual` (bloco preservado em `48dafad`) |

## 3. Fix Aplicado

Commit `87ffe70` — `terminal_routes.go` (9 linhas, cirúrgico):

```go
if tr := readOpenCodeTranscript("", readTermLogClear(sess)); tr != "" {
    // FIX 01/09 (auto-limpeza): se o transcript passou do teto de
    // auto-clear, grava o marcador agora (equivale a "Apagar") para
    // os próximos retornos já saírem leves.
    if int64(len(tr)) > termLogAutoClearBytes {
        if err := os.WriteFile(termLogClearPath(sess), []byte(strconv.FormatInt(time.Now().UnixMilli(), 10)), 0o644); err != nil {
            log.Printf("[term-log] erro auto-clear: %v", err)
        }
        tr = readOpenCodeTranscript("", readTermLogClear(sess))
    }
    trLines := strings.Split(tr, "\n")
```

Bloco idêntico ao original da branch. **NÃO houve merge da branch** — apenas a recuperação pontual.

## 4. Validação

### 4.1 Teste isolado (pré-deploy)
- Cópia do source com paths redirecionados para `/tmp`, DB fake do opencode com transcript de 2.5MB
- **Positivo:** marker gravado (41ms), resposta 173 bytes
- **Negativo:** transcript 5KB → sem marker, conteúdo normal (5129 chars)

### 4.2 Smoke test em produção real (após deploy)
| Teste | Condição | Resultado |
|---|---|---|
| **Negativo** (sessão real, 495KB) | `< 1.5MB` | ✅ sem marker, resposta 524KB com conteúdo normal |
| **Positivo** (sessão temporária 2.5MB) | `> 1.5MB` | ✅ marker `smoke-autoclear.clear` gravado (58ms), resposta 165 bytes, `text len: 0` |

**Cleanup:** sessão temporária removida do opencode.db (0 linhas restantes), marker removido, sessão real mais recente restaurada (total de 80 sessões inalterado). Sanidade pós-cleanup: transcript real volta a responder normal (500KB).

## 5. Deploy

| Item | Valor |
|---|---|
| Hash antes | `8a29ed9a3e154a3cf03bed54900b335d` |
| Hash depois | `dc5f9c2ade25783e26cb0b86ce7d7543` |
| Backup | `hokma.bak_20260910_192215` |
| `go test ./...` | ✅ `ok hokma_backend 27.579s` |
| Health check | ✅ HTTP 200 (com retry) |
| Verificação de hash build vs rodando | ✅ idênticos |

## 6. Commit + Push

- **Commit:** `87ffe70` — "fix(terminal): restaura auto-clear do transcript removido por engano em e09f6eb"
- **Push:** `e369fe0..87ffe70 main -> main` (fast-forward)
- Branch `hok-backend-atual` divergida **não tocada**

## 7. Conclusão

| Item | Status |
|---|---|
| Feature recuperada (sem merge) | ✅ |
| Teste isolado (positivo+negativo) | ✅ |
| Smoke test em produção (positivo+negativo) | ✅ |
| Cleanup da sessão de teste | ✅ |
| Deploy + health check | ✅ |
| Commit + push | ✅ `87ffe70` |
| Rollback necessário | ❌ Não |
