# ADENDO_SESSAO_20260829 — Eliminação do frontend antigo + modelo free (minimax-m3)

**Origem:** opencode (terminal) | **Data/hora:** 29-08-2026 13:59

---

## Resumo

Após o recovery do frontend (deploy acidental do build errado — template "Hokmá" do
Replit em vez do "HOK OS"), Washington pediu para eliminar o frontend ultrapassado
para não confundir, e depois commit/push + backup.

## O que foi feito

1. **Identificação dos projetos frontend no servidor:**

   | Diretório | Título | Status |
   |---|---|--|
   | `/root/hokma-web/artifacts/hok-os` | HOK OS | **ATUAL** — deployado (nginx :3002) |
   | `/root/hok_atual2` | Hokmá (template Replit) | **ANTIGO** — foi deployado por engano |
   | `/root/hokclaw-frontend` | tanstack (jun/26) | **ANTIGO** — protótipo abandonado |

2. **Causa da confusão:** o build do `/root/hok_atual2` (template Replit "Hokmá —
   built on Replit", `index-DNmBa2BK.js`) foi rsync-ado por engano para
   `/var/www/hok-os/` nas mudanças de 12:17, substituindo o frontend correto
   (HOK OS, `index-CsMPFJFf.js`). Recovery: re-deploy do build do
   `/root/hokma-web/artifacts/hok-os` (11:49).

3. **Eliminação:** `/root/hok_atual2` removido após backup
   `/root/backups_frontend_antigo/hok_atual2_20260829.tar.gz` (160 MB).
   `/root/hokclaw-frontend` mantido por ora (protótipo abandonado, sem deploy).

4. **Modelo free testado (substituir DeepSeek V4 Flash free, limite semanal
   esgotado):**
   - `minimax/minimax-m3:free` → **OK** (rápido, código correto, contexto 1M)
   - `google/gemma-4-31b-it:free` → OK (mais lento)
   - `nvidia/nemotron-3-super-120b-a12b:free` → OK (verboso)
   - `z-ai/glm-5.2:free` → provider error
   - `opencode-go/ox-alpha-free` / `opencode/x-preview-f-free` (Ox Alpha) → erro
     "Unexpected server error" no CLI (tier Zen/Go)

   **Recomendado: `minimax/minimax-m3:free`** (custo 0/0, contexto 1.048.576,
   reasoning + tool_call). Teste em andamento por Washington.

## Estado do deploy (29/08 13:59)

| Componente | Status |
|---|---|
| Backend Go | Produção, porta 8082 |
| Frontend | Produção (nginx :3002) — HOK OS `index-CsMPFJFf.js` |
| n8n | Docker, porta 5678 |
| hok_atual2 (antigo) | **Eliminado** (backup em /root/backups_frontend_antigo/) |

## Pendências

- Configurar `minimax/minimax-m3:free` como modelo default no HOK (após teste)
- `/root/hokclaw-frontend` — decidir se remove também (protótipo de jun/26)