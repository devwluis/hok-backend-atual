package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var backupDir = os.Getenv("HOME") + "/ecossistema/backups"

func ensureBackupDir() error {
	return os.MkdirAll(backupDir, 0755)
}

func saveBackup(filePath string) (string, error) {
	if err := ensureBackupDir(); err != nil {
		return "", fmt.Errorf("criar diretório de backup: %w", err)
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return "", nil
	}
	timestamp := time.Now().Format("20060102_150405")
	safePath := strings.ReplaceAll(strings.TrimPrefix(filePath, "/"), "/", "_")
	backupName := fmt.Sprintf("%s__%s", timestamp, safePath)
	backupPath := filepath.Join(backupDir, backupName)
	if err := copyFile(filePath, backupPath); err != nil {
		return "", fmt.Errorf("copiar backup: %w", err)
	}
	pruneOldBackups(safePath, 10)
	return backupPath, nil
}

func restoreBackup(backupPath, originalPath string) error {
	if backupPath == "" {
		return fmt.Errorf("nenhum backup disponível")
	}
	return copyFile(backupPath, originalPath)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func pruneOldBackups(safePath string, keep int) {
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return
	}
	var matching []string
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), safePath) {
			matching = append(matching, e.Name())
		}
	}
	sort.Strings(matching)
	for len(matching) > keep {
		os.Remove(filepath.Join(backupDir, matching[0]))
		matching = matching[1:]
	}
}

func latestBackupFor(filePath string) string {
	safePath := strings.ReplaceAll(strings.TrimPrefix(filePath, "/"), "/", "_")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return ""
	}
	var matching []string
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), safePath) {
			matching = append(matching, e.Name())
		}
	}
	if len(matching) == 0 {
		return ""
	}
	sort.Strings(matching)
	return filepath.Join(backupDir, matching[len(matching)-1])
}

func handleFsRollback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		http.Error(w, `{"error":"campo 'path' obrigatório"}`, http.StatusBadRequest)
		return
	}
	backupPath := latestBackupFor(req.Path)
	if backupPath == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": "nenhum backup encontrado para " + req.Path})
		return
	}
	if err := restoreBackup(backupPath, req.Path); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "restored": req.Path, "from": filepath.Base(backupPath)})
}

func handleFsBackupList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "método não permitido", http.StatusMethodNotAllowed)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	filePath := r.URL.Query().Get("path")
	if filePath == "" {
		http.Error(w, `{"error":"parâmetro 'path' obrigatório"}`, http.StatusBadRequest)
		return
	}
	safePath := strings.ReplaceAll(strings.TrimPrefix(filePath, "/"), "/", "_")
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "backups": []string{}})
		return
	}
	var backups []map[string]string
	for _, e := range entries {
		if !e.IsDir() && strings.Contains(e.Name(), safePath) {
			info, _ := e.Info()
			backups = append(backups, map[string]string{"name": e.Name(), "modified": info.ModTime().Format(time.RFC3339)})
		}
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i]["name"] > backups[j]["name"] })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"status": "ok", "file": filePath, "backups": backups})
}
