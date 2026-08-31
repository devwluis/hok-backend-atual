package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SnapshotMeta — metadata de um snapshot salvo em META.json
type SnapshotMeta struct {
	ID         string    `json:"id"`
	Tag        string    `json:"tag"`
	CreatedAt  time.Time `json:"created_at"`
	RepoPath   string    `json:"repo_path"`
	CommitSHA  string    `json:"commit_sha"`
	ConfigHash string    `json:"config_hash"`  // sha256 do ~/.config/opencode snapshot
	DataHash   string    `json:"data_hash"`    // sha256 do ~/.local/share/opencode snapshot
	ConfigSize int64     `json:"config_size"`  // bytes
	DataSize   int64     `json:"data_size"`
	Notes      string    `json:"notes,omitempty"`
}

// SnapshotPaths — paths relevantes a incluir no snapshot do opencode
var SnapshotPaths = []string{
	"/root/.config/opencode",
	"/root/.local/share/opencode",
}

// CreateSnapshot — faz git tag + tar dos paths relevantes + META.json.
// Retorna o ID do snapshot (= timestamp).
func CreateSnapshot(repoPath string, notes string) (*SnapshotMeta, error) {
	id := fmt.Sprintf("auto_%s", time.Now().UTC().Format("20060102_150405"))
	stateDir := filepath.Join(StateBaseDir, "snapshots", id)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir state: %w", err)
	}

	meta := &SnapshotMeta{
		ID:        id,
		Tag:       "snapshot/" + id,
		CreatedAt: time.Now().UTC(),
		RepoPath:  repoPath,
		Notes:     notes,
	}

	// 1. Git commit + tag (se repoPath for um git repo)
	if isGitRepo(repoPath) {
		commitSHA, err := gitCommitAllowEmpty(repoPath, "oca: snapshot "+id)
		if err != nil {
			return nil, fmt.Errorf("git commit: %w", err)
		}
		meta.CommitSHA = commitSHA

		if err := gitCreateTag(repoPath, meta.Tag, "oca auto-snapshot "+id); err != nil {
			return nil, fmt.Errorf("git tag: %w", err)
		}
	} else {
		meta.Notes += " [no git repo at " + repoPath + "]"
	}

	// 2. Tar dos paths do opencode
	for _, src := range SnapshotPaths {
		if _, err := os.Stat(src); os.IsNotExist(err) {
			continue // pula silenciosamente
		}
		baseName := filepath.Base(src) // "opencode" (config) ou "opencode" (data) — mesmo nome!
		// distinguir via parent dir
		tarName := baseName
		if strings.Contains(src, ".config") {
			tarName = "config_opencode.tgz"
		} else {
			tarName = "data_opencode.tgz"
		}
		tarPath := filepath.Join(stateDir, tarName)

		// FIX bug-checkpoint-fail: o opencode pode estar escrevendo no DB
		// enquanto o tar lê ("file changed as we read it"). Se falhar,
		// logamos aviso e pulamos — o config_opencode.tgz é o mais crítico.
		size, hash, err := tarDir(src, tarPath)
		if err != nil {
			log("aviso: tar %s falhou (%v) — pulando", src, err)
			if strings.Contains(src, ".config") {
				meta.Notes += " [config TAR FALHOU — rollback de config indisponível]"
			} else {
				meta.Notes += " [data TAR FALHOU — checkpoint parcial]"
			}
			continue
		}
		if strings.Contains(src, ".config") {
			meta.ConfigHash = hash
			meta.ConfigSize = size
		} else {
			meta.DataHash = hash
			meta.DataSize = size
		}
	}

	// 3. META.json
	metaPath := filepath.Join(stateDir, "META.json")
	if err := writeJSON(metaPath, meta); err != nil {
		return nil, fmt.Errorf("write META: %w", err)
	}

	return meta, nil
}

// RestoreSnapshot — restaura git tag + tar dos paths.
// Funciona com o opencode parado (camada fora do agente, igual recovery.sh).
func RestoreSnapshot(id string, repoPath string, dryRun bool) error {
	stateDir := filepath.Join(StateBaseDir, "snapshots", id)
	metaPath := filepath.Join(stateDir, "META.json")
	meta, err := readJSON[SnapshotMeta](metaPath)
	if err != nil {
		return fmt.Errorf("ler META.json: %w (snapshot %s existe?)", err, id)
	}

	log("=== restore snapshot %s ===", id)
	log("repo: %s", meta.RepoPath)
	log("tag:  %s", meta.Tag)
	log("config: %d bytes (%s)", meta.ConfigSize, meta.ConfigHash[:12])
	log("data:   %d bytes (%s)", meta.DataSize, meta.DataHash[:12])
	log("dry-run: %v", dryRun)

	if dryRun {
		log("DRY-RUN: nada foi modificado")
		return nil
	}

	// 1. Git reset --hard para a tag (se for git repo)
	if isGitRepo(meta.RepoPath) && meta.Tag != "" {
		if err := gitResetHard(meta.RepoPath, meta.Tag); err != nil {
			return fmt.Errorf("git reset --hard %s: %w", meta.Tag, err)
		}
		log("✓ código restaurado: git reset --hard %s", meta.Tag)
	}

	// 2. Restaura os tars
	for _, src := range SnapshotPaths {
		var tarName string
		if strings.Contains(src, ".config") {
			tarName = "config_opencode.tgz"
		} else {
			tarName = "data_opencode.tgz"
		}
		tarPath := filepath.Join(stateDir, tarName)
		if _, err := os.Stat(tarPath); os.IsNotExist(err) {
			continue
		}
		// rm -rf do path original + extract
		if err := os.RemoveAll(src); err != nil {
			return fmt.Errorf("rm %s: %w", src, err)
		}
		if err := extractTar(tarPath, filepath.Dir(src)); err != nil {
			return fmt.Errorf("extract %s: %w", tarPath, err)
		}
		log("✓ %s restaurado", src)
	}

	log("=== restore completo ===")
	log("verifique o estado. se algo estiver errado, o git tag ainda existe:")
	log("  git tag -l snapshot/")
	return nil
}

// === Helpers ===

func isGitRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

func gitCommitAllowEmpty(path, msg string) (string, error) {
	cmd := exec.Command("git", "-C", path, "-c", "user.email=oca@hokma", "-c", "user.name=oca",
		"commit", "--allow-empty", "-m", msg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("commit: %v\n%s", err, out)
	}
	shaCmd := exec.Command("git", "-C", path, "rev-parse", "HEAD")
	shaOut, err := shaCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %v\n%s", err, shaOut)
	}
	return strings.TrimSpace(string(shaOut)), nil
}

func gitCreateTag(path, tag, msg string) error {
	cmd := exec.Command("git", "-C", path, "tag", "-a", tag, "-m", msg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("tag: %v\n%s", err, out)
	}
	return nil
}

func gitResetHard(path, tag string) error {
	cmd := exec.Command("git", "-C", path, "reset", "--hard", tag)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("reset: %v\n%s", err, out)
	}
	cleanCmd := exec.Command("git", "-C", path, "clean", "-fd")
	cleanOut, cleanErr := cleanCmd.CombinedOutput()
	if cleanErr != nil {
		log("aviso: git clean falhou: %v\n%s", cleanErr, cleanOut)
	}
	return nil
}

func tarDir(src, dst string) (int64, string, error) {
	// tar -czf dst -C <parent> <base>
	parent := filepath.Dir(src)
	base := filepath.Base(src)
	cmd := exec.Command("tar", "-czf", dst, "-C", parent, base)
	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, "", fmt.Errorf("tar: %v\n%s", err, out)
	}

	info, err := os.Stat(dst)
	if err != nil {
		return 0, "", err
	}

	// hash
	f, err := os.Open(dst)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return 0, "", err
	}
	return info.Size(), hex.EncodeToString(h.Sum(nil)), nil
}

func extractTar(src, dst string) error {
	cmd := exec.Command("tar", "-xzf", src, "-C", dst)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract: %v\n%s", err, out)
	}
	return nil
}