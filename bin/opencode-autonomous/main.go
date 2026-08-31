package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const version = "0.1.0"

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	var root = &cobra.Command{
		Use:     "oca",
		Short:   "OpenCode Autonomous — wrapper CLI com snapshot + budget + circuit breaker",
		Version: version,
	}
	root.AddCommand(startCmd())
	root.AddCommand(rollbackCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(listCmd())
	root.AddCommand(configCmd())
	// FIX item-6: comandos de recovery (resume, abort-stale, checkpoint)
	root.AddCommand(resumeCmd())
	root.AddCommand(abortStaleCmd())
	root.AddCommand(checkpointCmd())
	return root
}

// === oca start ===

func startCmd() *cobra.Command {
	var (
		path         string
		message      string
		budget       int
		autoRollback bool
		cbMaxRepeat  int
		cbWindowMins int
		notes        string
		tui          bool
		pollMs       int
		force        bool
	)
	c := &cobra.Command{
		Use:   "start [path]",
		Short: "Inicia sessão autônoma do opencode com snapshot + budget + circuit breaker",
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
			if force {
				log("--force: limpando state/current (mesmo se órfão)")
				_ = clearState()
			}
			return runStart(abs, message, budget, autoRollback, cbMaxRepeat, cbWindowMins, notes, tui, pollMs)
		},
	}
	c.Flags().StringVarP(&message, "message", "m", "explore the repository", "mensagem inicial pro opencode (só modo run)")
	c.Flags().IntVarP(&budget, "budget", "b", 50, "número máximo de ações autônomas antes de bloquear")
	c.Flags().BoolVar(&autoRollback, "auto-rollback", false, "rollback automático quando budget/CB disparar (opt-in)")
	c.Flags().IntVar(&cbMaxRepeat, "cb-max", 3, "circuit breaker: max repetições idênticas")
	c.Flags().IntVar(&cbWindowMins, "cb-window", 10, "circuit breaker: janela em minutos")
	c.Flags().StringVar(&notes, "notes", "", "nota descritiva da sessão (vai pro META.json)")
	c.Flags().BoolVar(&tui, "tui", false, "modo TUI puro (opencode interativo completo, polling SQLite ~200ms)")
	c.Flags().IntVar(&pollMs, "poll-interval", 250, "intervalo de polling do SQLite (ms) — só modo --tui")
	c.Flags().BoolVar(&force, "force", false, "ignora state/current (mesmo se órfão) e cria snapshot novo")
	return c
}

func runStart(repoPath, message string, budget int, autoRollback bool, cbMax, cbWin int, notes string, tui bool, pollMs int) error {
	// 1. Verifica se já tem sessão ativa OU órfã (FIX item-6)
	cur, _ := readCurrentState()
	if cur != nil {
		stale := isStale(cur)
		if stale {
			fmt.Printf("⚠ sessão anterior órfã detectada: %s\n", cur.ID)
			fmt.Printf("  ações usadas: %d / %d\n", cur.ActionsUsed, cur.Budget)
			fmt.Printf("  CB events: %d\n", len(cur.CBEvents))
			fmt.Printf("  last heartbeat: %s\n", cur.LastHeartbeat)
			fmt.Printf("  para retomar:    oca resume\n")
			fmt.Printf("  para limpar:     oca abort-stale --yes\n")
			fmt.Printf("  para ignorar:    oca start --force (cria snapshot novo, descarta state)\n")
			return fmt.Errorf("sessão órfã: '%s' (use --force ou oca resume)", cur.ID)
		}
		return fmt.Errorf("já existe sessão ativa: %s (pid=%d). encerre com kill %d ou delete state/current",
			cur.ID, cur.PID, cur.PID)
	}

	// 2. Cria snapshot
	log("criando snapshot em %s...", repoPath)
	meta, err := CreateSnapshot(repoPath, notes)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	log("✓ snapshot criado: %s (tag=%s, commit=%s)",
		meta.ID, meta.Tag, truncate(meta.CommitSHA, 8))

	// 3. Gera config restritivo
	userCfg := filepath.Join(os.Getenv("HOME"), ".config", "opencode", "opencode.json")
	rc, err := GenerateRestrictiveConfig(userCfg)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	cfgDir := filepath.Join(StateBaseDir, "tmp", meta.ID)
	cfgPath, err := WriteRestrictiveConfig(cfgDir, rc)
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	log("✓ config restritivo: %s (%d bash deny + %d edit deny)",
		cfgPath, len(DefaultDangerousPatterns), len(SensitivePaths))

	// 4. State inicial
	mode := "run"
	if tui {
		mode = "tui"
	}
	state := &SessionState{
		ID:            meta.ID,
		Mode:          mode,
		RepoPath:      repoPath,
		ConfigPath:    cfgPath,
		ConfigDir:     cfgDir,
		Budget:        budget,
		ActionsUsed:   0,
		StartedAt:     meta.CreatedAt.Format(time.RFC3339),
		AutoRollback:  autoRollback,
		CBWindowMins:  cbWin,
		CBMaxRepeat:   cbMax,
		Notes:         notes,
	}
	if err := writeState(state); err != nil {
		return fmt.Errorf("write state: %w", err)
	}

	// 5. Setup CB + Budget
	bt := NewBudgetTracker(budget)
	cb := NewCircuitBreaker(cbMax, cbWin)

// onToolUse — callback chamado a cada tool_use interceptado.
	// FIX item-6: persiste Actions[] + CBEvents[] + UpdatedAt em state/current
	// a cada tool_use (atomic rename). Permite resume após crash.
	var terminated bool // FIX bug-log-duplicado: callback pode ser chamado 2x antes do KillOpenCode propagar
	onToolUse := func(tool, hash, summary string) {
		if terminated {
			return
		}
		now := time.Now().UTC()
		state.ActionsUsed++
		remaining, blocked, warned := bt.Consume()
		repeat, triggered := cb.Record(hash, summary)

		// Append action record (item 6: auditoria completa)
		state.Actions = append(state.Actions, ActionRecord{
			N:       state.ActionsUsed,
			Tool:    tool,
			Hash:    hash,
			Summary: summary,
			Ts:      now,
		})
		// Snapshot CB events (item 6: persistência do sliding window)
		state.CBEvents = cb.Snapshot()
		state.LastHeartbeat = now.Format(time.RFC3339)

		if warned {
			log("⚠ AVISO: budget em %d/%d — %d ações restantes", remaining, budget, remaining)
		}

		if triggered {
			terminated = true
			state.BlockedReason = "circuit_breaker: " + summary + " (repetiu " + itoa(repeat) + "x em " +
				itoa(cbWin) + "min)"
			log("⛔ CIRCUIT BREAKER: %s repetiu %dx em %dmin — encerrando opencode", summary, repeat, cbWin)
			_ = writeState(state)
			if err := KillOpenCode(state); err != nil {
				log("kill err: %v", err)
			}
			return
		}

		if blocked {
			terminated = true
			state.BlockedReason = "budget_exhausted"
			log("⛔ BUDGET ESGOTADO (%d/%d) — encerrando opencode", budget, budget)
			_ = writeState(state)
			if err := KillOpenCode(state); err != nil {
				log("kill err: %v", err)
			}
			return
		}

		log("  ação #%d: %s (restam %d)", state.ActionsUsed, summary, remaining)
		_ = writeState(state)
	}

	log("iniciando opencode modo=%s (budget=%d)...", mode, budget)

	var result *InterceptResult
	if tui {
		// MODO TUI — spawn opencode puro + polling SQLite.
		// FIX bug-session-missing: opencode leva ~500ms pra criar a session
		// no DB; se o wrapper procura antes, falha. Solução: spawnar
		// opencode PRIMEIRO, depois loop de retry no findOpencodeSession.
		log("(TUI puro: opencode roda interativo, polling SQLite a cada %dms)", pollMs)
		tuiCmd := newOpencodeTUICmd(repoPath, cfgDir)
		if err := tuiCmd.Start(); err != nil {
			return fmt.Errorf("start opencode tui: %w", err)
		}
		state.PID = tuiCmd.Process.Pid
		log("opencode TUI iniciado: pid=%d", state.PID)
		// Espera até 10s pela session aparecer no DB
		sessionID, err := waitForOpencodeSession(repoPath, 10*time.Second)
		if err != nil {
			_ = tuiCmd.Process.Kill()
			return fmt.Errorf("acha session: %w", err)
		}
		state.OpencodeSessionID = sessionID
		log("session opencode detectada: %s", sessionID)
		result, err = runTUIWithKnownSession(tuiCmd, state, time.Duration(pollMs)*time.Millisecond, onToolUse)
	} else {
		// MODO RUN — JSON stream (existente)
		log("(modo run: opencode executa com mensagem inicial)")
		if message == "" {
			message = "explore the repository"
		}
		result, err = RunOpenCodeWithIntercept(repoPath, message, rc.Model, cfgDir, state, onToolUse)
	}
	if err != nil {
		return fmt.Errorf("run: %w", err)
	}

	// 7. Finaliza
	if state.BlockedReason == "" {
		state.BlockedReason = "completed"
	}
	if result != nil && result.Reason == "opencode_exit" && state.ActionsUsed == 0 {
		state.BlockedReason = "no_actions"
	}

	// 8. Auto-rollback se configurado
	if state.AutoRollback && state.BlockedReason != "completed" && state.BlockedReason != "no_actions" {
		log("auto-rollback ATIVO — restaurando snapshot %s", state.ID)
		if err := RestoreSnapshot(state.ID, repoPath, false); err != nil {
			log("auto-rollback falhou: %v", err)
		} else {
			log("✓ auto-rollback completo")
		}
	} else if state.BlockedReason != "completed" && state.BlockedReason != "no_actions" {
		log("para reverter manualmente: oca rollback %s", state.ID)
	}

	// 9. Salva no histórico + limpa state/current
	if err := snapshotStateToHistory(state); err != nil {
		log("aviso: salvou history: %v", err)
	}
	_ = clearState()

	log("✓ sessão encerrada: id=%s, modo=%s, ações=%d, status=%s",
		state.ID, state.Mode, state.ActionsUsed, state.BlockedReason)
	return nil
}

// === oca rollback ===

func rollbackCmd() *cobra.Command {
	var (
		dryRun bool
		yes    bool
	)
	c := &cobra.Command{
		Use:   "rollback <checkpoint_id>",
		Short: "Restaura um snapshot (git reset + restore dos tars)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if !yes {
				fmt.Printf("isso vai SOBRESCREVER código em git e restaurar ~/.config/opencode + ~/.local/share/opencode.\n")
				fmt.Printf("tem certeza? digite 'sim' pra continuar: ")
				var resp string
				fmt.Scanln(&resp)
				if !strings.EqualFold(strings.TrimSpace(resp), "sim") {
					fmt.Println("abortado")
					return nil
				}
			}
			return RestoreSnapshot(id, "", dryRun)
		},
	}
	c.Flags().BoolVar(&dryRun, "dry-run", false, "mostra o que seria feito sem modificar nada")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "pula a confirmação interativa")
	return c
}

// === oca status ===

func statusCmd() *cobra.Command {
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
			fmt.Printf("id:        %s\n", cur.ID)
			fmt.Printf("repo:      %s\n", cur.RepoPath)
			fmt.Printf("pid:       %d\n", cur.PID)
			fmt.Printf("budget:    %d/%d\n", cur.ActionsUsed, cur.Budget)
			fmt.Printf("started:   %s\n", cur.StartedAt)
			fmt.Printf("config:    %s\n", cur.ConfigPath)
			if cur.AutoRollback {
				fmt.Printf("auto-rollback: ON\n")
			} else {
				fmt.Printf("auto-rollback: OFF\n")
			}
			return nil
		},
	}
}

// === oca list ===

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Lista sessões passadas (histórico)",
		RunE: func(cmd *cobra.Command, args []string) error {
			hist, err := listHistory()
			if err != nil {
				return err
			}
			if len(hist) == 0 {
				fmt.Println("(nenhuma sessão no histórico)")
				return nil
			}
			fmt.Printf("%-25s %-25s %-10s %-20s\n", "ID", "REPO", "AÇÕES", "STATUS")
			for _, h := range hist {
				repo := truncate(h.RepoPath, 25)
				fmt.Printf("%-25s %-25s %-10s %-20s\n",
					h.ID, repo,
					fmt.Sprintf("%d/%d", h.ActionsUsed, h.Budget),
					h.BlockedReason)
			}
			return nil
		},
	}
}

// === oca config ===

func configCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Mostra config restritivo que seria gerado (debug da blocklist)",
		RunE: func(cmd *cobra.Command, args []string) error {
			userCfg := filepath.Join(os.Getenv("HOME"), ".config", "opencode", "opencode.json")
			rc, err := GenerateRestrictiveConfig(userCfg)
			if err != nil {
				return err
			}
			out, err := jsonMarshalIndent(rc, "", "  ")
			if err != nil {
				return err
			}
			fmt.Println(string(out))
			fmt.Printf("\n[bash deny patterns: %d]\n", len(DefaultDangerousPatterns))
			fmt.Printf("[edit deny paths: %d]\n", len(SensitivePaths))
			return nil
		},
	}
}

// === Helpers ===

func jsonMarshalIndent(v interface{}, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(v, prefix, indent)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}