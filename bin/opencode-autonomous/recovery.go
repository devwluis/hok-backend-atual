package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// === oca resume ===

func resumeCmd() *cobra.Command {
	var (
		path         string
		budget       int
		yes          bool
	)
	c := &cobra.Command{
		Use:   "resume [path]",
		Short: "Detecta sessão órfã e retoma de onde parou (item 6)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				path = args[0]
			}
			if path == "" {
				path = "."
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			return runResume(abs, budget, yes)
		},
	}
	c.Flags().IntVarP(&budget, "budget", "b", 0, "budget adicional (0 = usa o restante do state)")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "pula confirmação")
	return c
}

// runResume — carrega state/current, valida que está stale, e retoma sessão
// reaproveitando snapshot + CB events + actions já feitas.
func runResume(repoPath string, extraBudget int, yes bool) error {
	stale, err := detectStaleSession(repoPath)
	if err != nil {
		return fmt.Errorf("detectar órfão: %w", err)
	}
	if stale == nil {
		return fmt.Errorf("nenhuma sessão órfã detectada. verifique 'oca status' ou 'oca list'")
	}

	log("sessão órfã detectada:")
	log("  id: %s", stale.ID)
	log("  repo: %s", stale.RepoPath)
	log("  ações usadas: %d / %d", stale.ActionsUsed, stale.Budget)
	log("  CB events: %d (janela %dmin)", len(stale.CBEvents), stale.CBWindowMins)
	log("  started: %s", stale.StartedAt)
	log("  last heartbeat: %s", stale.LastHeartbeat)
	if len(stale.Actions) > 0 {
		log("  última ação: #%d %s (%s)", stale.Actions[len(stale.Actions)-1].N,
			stale.Actions[len(stale.Actions)-1].Summary,
			stale.Actions[len(stale.Actions)-1].Ts.Format("15:04:05"))
	}
	log("  opencode session id: %s", emptyAsDash(stale.OpencodeSessionID))

	if !yes {
		fmt.Printf("\nretomar esta sessão? (rollback continua disponível) [sim/não]: ")
		var resp string
		fmt.Scanln(&resp)
		if !strings.EqualFold(strings.TrimSpace(resp), "sim") {
			fmt.Println("abortado")
			return nil
		}
	}

	// === Retoma: restaura CB events e configura budget restante ===
	resumeBudget := extraBudget
	if resumeBudget == 0 {
		// sem novo budget — mantém o restante (Budget - ActionsUsed)
		resumeBudget = stale.Budget
		if stale.ActionsUsed >= stale.Budget {
			log("AVISO: budget já estava esgotado (%d/%d) — usando --budget adicional de 10",
				stale.ActionsUsed, stale.Budget)
			resumeBudget = 10
		}
	}

	stale.ResumeCount++
	stale.PID = 0 // será atualizado quando spawnar opencode
	stale.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	stale.LastHeartbeat = stale.UpdatedAt
	if err := writeState(stale); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	log("✓ sessão retomada (resume #%d, budget=%d)", stale.ResumeCount, resumeBudget)
	log("use 'oca start' (com mesmo path) — ele detecta state existente e continua")
	return nil
}

// === oca abort-stale ===

func abortStaleCmd() *cobra.Command {
	var yes bool
	c := &cobra.Command{
		Use:   "abort-stale",
		Short: "Limpa state/current de uma sessão órfã (NÃO deleta snapshot)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAbortStale(yes)
		},
	}
	c.Flags().BoolVarP(&yes, "yes", "y", false, "pula confirmação")
	return c
}

func runAbortStale(yes bool) error {
	cur, err := readCurrentState()
	if err != nil {
		return err
	}
	if cur == nil {
		fmt.Println("nenhuma sessão órfã — state/current não existe")
		return nil
	}
	if !yes {
		fmt.Printf("isso vai DELETAR state/current (snapshot fica preservado em snapshots/%s/)\n", cur.ID)
		fmt.Printf("confirma? [sim/não]: ")
		var resp string
		fmt.Scanln(&resp)
		if !strings.EqualFold(strings.TrimSpace(resp), "sim") {
			fmt.Println("abortado")
			return nil
		}
	}
	if err := clearState(); err != nil {
		return err
	}
	fmt.Printf("✓ state/current limpo. snapshot %s ainda disponível via 'oca rollback %s'\n",
		cur.ID, cur.ID)
	return nil
}

// === oca checkpoint ===

func checkpointCmd() *cobra.Command {
	var (
		path  string
		notes string
		yes   bool
	)
	c := &cobra.Command{
		Use:   "checkpoint [path]",
		Short: "Cria snapshot manual (sem iniciar sessão) — útil antes de mudanças arriscadas",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				path = args[0]
			}
			if path == "" {
				path = "."
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			// Recusa se já tem sessão ativa
			cur, _ := readCurrentState()
			if cur != nil && !yes {
				fmt.Printf("já existe sessão ativa: %s. aborta primeiro com 'oca abort-stale --yes'\n", cur.ID)
				return nil
			}
			meta, err := CreateSnapshot(abs, notes)
			if err != nil {
				return err
			}
			log("✓ checkpoint criado: %s", meta.ID)
			log("  tag:  %s", meta.Tag)
			log("  size: config=%d data=%d bytes", meta.ConfigSize, meta.DataSize)
			log("  para reverter: oca rollback %s", meta.ID)
			return nil
		},
	}
	c.Flags().StringVar(&notes, "notes", "manual checkpoint", "nota descritiva")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "ignora checagem de sessão ativa")
	return c
}

// === oca status (atualizado p/ mostrar item 6) ===

func statusUpdatedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Mostra estado da sessão ativa (se houver)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cur, err := readCurrentState()
			if err != nil {
				return err
			}
			if cur == nil {
				fmt.Println("nenhuma sessão ativa")
				return nil
			}
			// Detecta se está stale (processo morto)
			stale := isStale(cur)
			fmt.Printf("id:          %s%s\n", cur.ID, ifThen(stale, " (STALE — resume disponível)", ""))
			fmt.Printf("repo:        %s\n", cur.RepoPath)
			fmt.Printf("pid:         %d (live: %v)\n", cur.PID, !stale)
			fmt.Printf("budget:      %d/%d\n", cur.ActionsUsed, cur.Budget)
			fmt.Printf("started:     %s\n", cur.StartedAt)
			fmt.Printf("updated:     %s\n", cur.UpdatedAt)
			fmt.Printf("heartbeat:   %s\n", cur.LastHeartbeat)
			fmt.Printf("config:      %s\n", cur.ConfigPath)
			fmt.Printf("CB events:   %d (janela %dmin)\n", len(cur.CBEvents), cur.CBWindowMins)
			fmt.Printf("resumes:     %d\n", cur.ResumeCount)
			if cur.AutoRollback {
				fmt.Printf("auto-rollback: ON\n")
			} else {
				fmt.Printf("auto-rollback: OFF\n")
			}
			if cur.BlockedReason != "" {
				fmt.Printf("status:      %s\n", cur.BlockedReason)
			}
			return nil
		},
	}
}

// === Helpers ===

// detectStaleSession — verifica state/current e retorna a sessão se:
// - state existe
// - state.RepoPath == repoPath (ou repoPath vazio)
// - processo PID morto (kill -0 falha) E heartbeat antigo (> StateStaleThreshold)
// OU: processo vivo MAS opencode_session_id diferente do estado (nunca resume antes)
func detectStaleSession(repoPath string) (*SessionState, error) {
	cur, err := readCurrentState()
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, nil
	}
	if repoPath != "" && cur.RepoPath != repoPath {
		log("state/current é de outro repo (%s vs %s) — não considera stale",
			cur.RepoPath, repoPath)
		return nil, nil
	}
	if !isStale(cur) {
		log("state/current existe mas processo está vivo (pid=%d) — sessão ativa, não é stale", cur.PID)
		return nil, nil
	}
	return cur, nil
}

// isStale — true se processo PID morto OU heartbeat antigo
func isStale(s *SessionState) bool {
	if s == nil {
		return false
	}
	// Se PID ainda existe E heartbeat recente → ativo
	if s.PID > 0 {
		if err := syscall.Kill(s.PID, 0); err == nil {
			// processo vivo
			if s.LastHeartbeat != "" {
				if hb, err := time.Parse(time.RFC3339, s.LastHeartbeat); err == nil {
					if time.Since(hb) < StateStaleThreshold {
						return false // vivo + hb recente
					}
				}
			}
		}
	}
	// PID morto OU hb antigo
	if s.LastHeartbeat != "" {
		if hb, err := time.Parse(time.RFC3339, s.LastHeartbeat); err == nil {
			return time.Since(hb) > StateStaleThreshold
		}
	}
	// sem heartbeat (sessão muito antiga) — considera stale
	return true
}

// emptyAsDash — utilidade de formatação
func emptyAsDash(s string) string {
	if s == "" {
		return "(vazio)"
	}
	return s
}

func ifThen(b bool, yes, no string) string {
	if b {
		return yes
	}
	return no
}

// checkPIDLive — wrapper pra testes
func checkPIDLive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// unused — silencia warning do exec.Command caso remova imports
var _ = exec.Command
var _ = strconv.Itoa