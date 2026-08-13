# Adendo de Correção — Motor self_mod.go desconectado do fluxo real
**Complementa/corrige:** `ADENDO_2026-08-10_auditoria_selfmod.md` (seção 8, "ATUALIZAÇÃO")
**Data:** 2026-08-10, sessão de chat posterior
**Servidor:** hokma (Hostinger KVM2)

---

## 1. Contexto — o que o adendo anterior errou

O adendo anterior registrou como achado principal: *"`selfmod_revert.sh` não faz revert real — bug no rollback."* Essa investigação continuou nesta sessão e revelou algo mais profundo: **o bug do revert é irrelevante na prática**, porque o motor inteiro (`self_mod.go` / `executeSelfMod` / commit / smoke test / revert / tabela `self_modifications`) **nunca é chamado em produção**.

## 2. Achado real — dois campos, um usado errado

`PendingAction` tem dois campos que parecem sinônimos mas não são:

```go
ToolName    string  // decide QUAL FUNÇÃO EXECUTA (usado no switch de pending_action.go:371)
ActionType  string  // "n8n" | "self_mod" — hoje só rotula severidade/preview, não roteia execução
```

O switch que decide o que rodar após aprovação usa `pa.ToolName`:
```go
case "fs_exec":  → resolveFsExecPendingAction(pa)   // é o que roda de fato
case "self_mod": → executeSelfMod(pa)               // nunca é alcançado
```

O caminho real de produção (`smart_chat.go:isSelfModCommand` → `registerFsExecPendingAction` em `pending_action.go:557`) sempre cria a ação com `ToolName: "fs_exec"` e `ActionType: "self_mod"`. Como o switch olha `ToolName`, cai sempre no case `"fs_exec"` — o case `"self_mod"` e toda a função `executeSelfMod` são **código morto** hoje.

## 3. O que `resolveFsExecPendingAction` (o que roda de fato) realmente faz

```go
func resolveFsExecPendingAction(action *PendingAction) string {
    cmd, ok := pendingExecCommands[action.ID]
    output, err := ExecuteApprovedCommand(action.ID, cmd)
    // ...retorna output. Fim.
}
```

Nenhum commit git, nenhum smoke test, nenhum rollback automático, nenhuma gravação em `self_modifications`. Após a aprovação humana no chat, o bash roda cru — a única rede de segurança é o `diffPreview` textual mostrado **antes** da aprovação. Depois de aprovado, não há verificação de que o resultado foi seguro nem forma de desfazer automaticamente.

Isso explica de forma completa e definitiva os `0` registros em `self_modifications`: não é falha de gravação, é ausência total de chamada.

## 4. Reclassificação de severidade

| Achado do adendo anterior | Severidade original | Severidade real |
|---|---|---|
| `selfmod_revert.sh` só faz echo, não reverte | Média (bug de rollback) | **Baixa** — revert nunca é invocado no fluxo real, dead code |
| Motor completo (commit+test+revert+audit) desconectado do fluxo aprovado real | Não identificado | **Alta** — toda automodificação aprovada em produção roda sem commit, sem teste automático, sem auditoria em banco, sem rollback |

## 5. Pendência para próxima sessão (item 2, ainda não feito)

Decidir e implementar uma de duas abordagens:
1. Corrigir o roteamento (`ToolName` vs `ActionType`) para que ações de self-mod passem de fato por `executeSelfMod` (commit/smoke-test/revert/audit)
2. Ou aceitar `resolveFsExecPendingAction` como o caminho definitivo e adicionar commit+audit+smoke-test nele, arquivando `self_mod.go`/scripts como não usados

Ambas exigem também corrigir `selfmod_revert.sh` (bug do echo) — mas só depois de decidir qual caminho fica ativo, para não investir no motor errado.

---

*Este adendo corrige e substitui a conclusão do achado 8 (ATUALIZAÇÃO) do `ADENDO_2026-08-10_auditoria_selfmod.md`. Aquele documento permanece válido para o restante do conteúdo (scripts investigados, recuperação de skills, etc).*
