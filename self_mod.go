package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func executeSelfMod(pa *PendingAction) string {
	tenantID := pa.TenantID
	if tenantID == "" {
		tenantID = "owner"
	}
	result := executeTool(pa.ToolName, pa.ArgsJSON)
	var modifiedFile string
	var args map[string]interface{}
	json.Unmarshal([]byte(pa.ArgsJSON), &args)
	cmd, _ := args["cmd"].(string)
	file, _ := args["file"].(string)
	if file != "" {
		modifiedFile = file
	} else {
		parts := strings.Fields(cmd)
		for i, p := range parts {
			if strings.HasPrefix(p, "/") && (strings.HasSuffix(p, ".go") || strings.HasSuffix(p, ".ts") || strings.HasSuffix(p, ".tsx") || strings.HasSuffix(p, ".js") || strings.HasSuffix(p, ".json")) {
				modifiedFile = p
				break
			}
			if i > 0 && (parts[i-1] == ">" || parts[i-1] == ">>" || parts[i-1] == "tee" || parts[i-1] == "mv" || parts[i-1] == "cp") {
				modifiedFile = p
				break
			}
		}
	}
	if modifiedFile == "" {
		recordSelfMod(tenantID, "", pa.Description, "", false, "applied")
		return result + "\n\n[AUTOMOD] Comando executado, mas arquivo nao identificado."
	}
	commitHash, err := selfModCommit(tenantID, modifiedFile, "usuario", pa.Description)
	if err != nil {
		recordSelfMod(tenantID, "", pa.Description, "", false, "commit_failed")
		return result + "\n\n[AUTOMOD] ERRO no commit: " + err.Error()
	}
	if !runSmokeTest() {
		selfModRevert(tenantID, commitHash)
		recordSelfMod(tenantID, commitHash, modifiedFile, pa.Description, false, "rolled_back")
		return result + "\n\n[AUTOMOD] SMOKE TEST FALHOU. Rollback executado."
	}
	recordSelfMod(tenantID, commitHash, modifiedFile, pa.Description, true, "applied")
	return result + "\n\n[AUTOMOD] Sucesso! Commit " + commitHash[:8] + " registrado."
}
func selfModCommit(tenantID, file, requestedBy, desc string) (string, error) {
	script := "/root/hokma/scripts/selfmod_commit.sh"
	if _, err := os.Stat(script); os.IsNotExist(err) {
		return "", fmt.Errorf("script nao encontrado: %s", script)
	}
	cmd := exec.Command("bash", script, tenantID, file, requestedBy, desc)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("commit falhou: %v - output: %s", err, string(out))
	}
	repo := fmt.Sprintf("/root/hokma/tenants/%s/.git-worktree", tenantID)
	hashCmd := exec.Command("git", "-C", repo, "log", "-1", "--format=%H")
	hashOut, _ := hashCmd.Output()
	return strings.TrimSpace(string(hashOut)), nil
}

func selfModRevert(tenantID, hash string) error {
	script := "/root/hokma/scripts/selfmod_revert.sh"
	cmd := exec.Command("bash", script, tenantID, hash)
	_, err := cmd.CombinedOutput()
	return err
}

func runSmokeTest() bool {
	script := "/root/hokma/scripts/smoke_test.sh"
	if _, err := os.Stat(script); os.IsNotExist(err) {
		return false
	}
	cmd := exec.Command("bash", script)
	return cmd.Run() == nil
}

func recordSelfMod(tenantID, hash, file, desc string, smokeOK bool, status string) {
	db, err := sql.Open("sqlite3", "/root/hokma/backend/hokma.db")
	if err != nil {
		return
	}
	defer db.Close()
	_, _ = db.Exec(
		"INSERT INTO self_modifications (tenant_id, commit_hash, file_path, ia_description, diff_summary, smoke_test_passed, status) VALUES (?, ?, ?, ?, ?, ?, ?)",
		tenantID, hash, file, desc, "", smokeOK, status,
	)
}
