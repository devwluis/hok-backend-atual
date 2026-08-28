package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ─── Recovery: snapshot + rollback do MODO AUTÔNOMO TOTAL (28/08) ──────────
// Decisões fechadas por Washington:
//  1. mode:"autonomous_total" como flag explícita (nunca default);
//  2. budget alto (50) — rede de segurança mesmo se o CB falhar;
//  3. rollback manual por padrão (CB para + avisa + comando pronto), flag
//     auto_rollback opcional; script standalone recovery.sh como camada
//     fora do agente;
//  4. snapshot inclui o volume do hermes (/opt/data + /root/.hermes); n8n
//     fica de fora;
//  5. branch task/<nome> para código de tarefas grandes (orientação de uso).

const (
	snapshotsRoot  = "/root/hokma/snapshots"
	recoveryScript = "/root/hokma/backend/recovery.sh"
	totalBudget    = 50
)

var checkpointIDPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func checkpointDir(id string) string { return filepath.Join(snapshotsRoot, id) }

func checkpointExists(id string) bool {
	st, err := os.Stat(checkpointDir(id))
	return err == nil && st.IsDir()
}

// ─── Snapshot do código (git) — testável com gitDir parametrizado ──────────

// gitSnapshot commita o working tree (checkpoint) e cria a tag
// snapshot/<id>. "Nothing to commit" é tolerado (tag no HEAD atual).
func gitSnapshot(gitDir, snapDir, id string) error {
	add := exec.Command("git", "-C", gitDir, "add", "-A")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	commit := exec.Command("git", "-C", gitDir, "commit", "-m", fmt.Sprintf("checkpoint %s (autonomous_total pre-tarefa)", id))
	if out, err := commit.CombinedOutput(); err != nil {
		// tolera "nothing to commit": tag no HEAD atual
		if !strings.Contains(string(out), "nothing to commit") {
			return fmt.Errorf("git commit: %v: %s", err, out)
		}
	}
	tag := exec.Command("git", "-C", gitDir, "tag", "snapshot/"+id)
	if out, err := tag.CombinedOutput(); err != nil {
		return fmt.Errorf("git tag: %v: %s", err, out)
	}
	return nil
}

// ─── Snapshot completo (orquestração) ───────────────────────────────────────

// snapshotCreate monta o checkpoint <snapshotsRoot>/<id> com: código (git
// commit+tag), memory.db (sqlite3 .backup), volume do hermes (/opt/data),
// /root/.hermes, .env, e META.json. Grava o checkpoint_id na session_mode.
func snapshotCreate(convID, tenantID, userID string) (string, error) {
	id := "auto_" + time.Now().Format("20060102_150405")
	dir := checkpointDir(id)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return "", err
	}
	backendDir := "/root/hokma/backend"

	if err := gitSnapshot(backendDir, dir, id); err != nil {
		return "", err
	}

	// Banco: cópia consistente via sqlite3 .backup (captura WAL/shm).
	execCmd := exec.Command("sqlite3", filepath.Join(backendDir, "memory.db"), ".backup '"+filepath.Join(dir, "memory.db")+"'")
	if out, err := execCmd.CombinedOutput(); err != nil {
		log.Printf("[AUDIT] snapshot %s: backup do banco falhou: %v %s", id, err, out)
	}

	// Volume do hermes (/opt/data) — decisão 4: mutável dentro do escopo.
	if vol, vErr := hermesDataVolume(); vErr == nil && vol != "" {
		hostData := "/var/lib/docker/volumes/" + vol + "/_data"
		if st, err := os.Stat(hostData); err == nil && st.IsDir() {
			tarCmd := exec.Command("tar", "czf", filepath.Join(dir, "hermes_optdata.tgz"), "-C", hostData, ".")
			if out, err := tarCmd.CombinedOutput(); err != nil {
				log.Printf("[AUDIT] snapshot %s: tar do volume falhou: %v %s", id, err, out)
			}
		}
	}

	// /root/.hermes (bind do hermes-gateway).
	if st, err := os.Stat("/root/.hermes"); err == nil && st.IsDir() {
		tarCmd := exec.Command("tar", "czf", filepath.Join(dir, "hermes_root_home.tgz"), "-C", "/root", ".hermes")
		if out, err := tarCmd.CombinedOutput(); err != nil {
			log.Printf("[AUDIT] snapshot %s: tar /root/.hermes falhou: %v %s", id, err, out)
		}
	}

	// .env (cópia simples — nunca vai pro git).
	if b, err := os.ReadFile(filepath.Join(backendDir, ".env")); err == nil {
		os.WriteFile(filepath.Join(dir, ".env"), b, 0600)
	}

	meta := map[string]string{
		"id":      id,
		"ts":      time.Now().Format(time.RFC3339),
		"conv_id": convID,
		"tenant":  tenantID,
		"user":    userID,
	}
	if mb, err := json.Marshal(meta); err == nil {
		os.WriteFile(filepath.Join(dir, "META.json"), mb, 0600)
	}

	log.Printf("[AUDIT] snapshot criado id=%s conv=%s (git tag snapshot/%s)", id, convID, id)
	return id, nil
}

// ─── Disparo do rollback (script standalone recovery.sh) ───────────────────

// triggerRecovery roda o recovery.sh fora do cgroup do hokma (systemd-run)
// — o script para o serviço, restaura e reinicia; funciona mesmo se o
// próprio hokma estiver caído (camada de segurança fora do agente).
func triggerRecovery(checkpointID string) error {
	if !checkpointIDPattern.MatchString(checkpointID) || !checkpointExists(checkpointID) {
		return fmt.Errorf("checkpoint inválido: %s", checkpointID)
	}
	if os.Getenv("HOK_RECOVERY_DRY_RUN") == "1" {
		log.Printf("[AUDIT] recovery DRY-RUN: dispararia recovery.sh %s", checkpointID)
		return nil
	}
	cmd := exec.Command("systemd-run", "--unit=recovery-"+checkpointID, "--collect", recoveryScript, checkpointID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemd-run recovery falhou: %v %s", err, out)
	}
	log.Printf("[AUDIT] recovery disparado para o checkpoint %s (serviço será reiniciado)", checkpointID)
	return nil
}

// ─── Endpoint POST /recovery/rollback ───────────────────────────────────────

func handleRecoveryRollback(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var body struct {
		CheckpointID string `json:"checkpoint_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	id := strings.TrimSpace(body.CheckpointID)
	if id == "" {
		// fallback: checkpoint da conversa (session_mode)
		convID := convIdFromRequest(r)
		if cid := sessionModeCheckpoint(convID, tenantIdFromRequest(r), userIdFromRequest(r)); cid != "" {
			id = cid
		}
	}
	if err := triggerRecovery(id); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "recovery_started", "checkpoint_id": id,
		"message": "rollback iniciado — o hokma será reiniciado (recovery.sh standalone)",
	})
}