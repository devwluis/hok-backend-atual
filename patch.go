package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
)

type PatchOp struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Content string `json:"content"`
}

type PatchRequest struct {
	Path    string    `json:"path"`
	Patches []PatchOp `json:"patches"`
}

type PatchResult struct {
	Status       string `json:"status"`
	Path         string `json:"path"`
	BackupPath   string `json:"backup_path"`
	PatchesApply int    `json:"patches_applied"`
	LinesOld     int    `json:"lines_before"`
	LinesNew     int    `json:"lines_after"`
}

func handleFsPatch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		http.Error(w, `{"status":"error","message":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	if !requireHokAuth(w, r) {
		return
	}
	var req PatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"status":"error","message":"invalid json"}`, http.StatusBadRequest)
		return
	}

	if req.Path == "" {
		http.Error(w, `{"status":"error","message":"path required"}`, http.StatusBadRequest)
		return
	}

	if len(req.Patches) == 0 {
		http.Error(w, `{"status":"error","message":"patches array empty"}`, http.StatusBadRequest)
		return
	}

	if strings.Contains(req.Path, "..") {
		http.Error(w, `{"status":"error","message":"invalid path"}`, http.StatusBadRequest)
		return
	}

	original, err := os.ReadFile(req.Path)
	if err != nil {
		errMsg := fmt.Sprintf(`{"status":"error","message":"cannot read file: %s"}`, err.Error())
		http.Error(w, errMsg, http.StatusNotFound)
		return
	}

	backupPath, err := createBackup(req.Path)
	if err != nil {
		errMsg := fmt.Sprintf(`{"status":"error","message":"backup failed: %s"}`, err.Error())
		http.Error(w, errMsg, http.StatusInternalServerError)
		return
	}

	lines := strings.Split(string(original), "\n")
	linesOld := len(lines)

	sort.Slice(req.Patches, func(i, j int) bool {
		return req.Patches[i].Start > req.Patches[j].Start
	})

	for _, p := range req.Patches {
		s := p.Start - 1
		e := p.End

		if s < 0 { s = 0 }
		if e > len(lines) { e = len(lines) }
		if s > len(lines) { s = len(lines) }

		var newLines []string
		if p.Content != "" {
			newLines = strings.Split(p.Content, "\n")
		}
		lines = append(lines[:s], append(newLines, lines[e:]...)...)
	}

	result := strings.Join(lines, "\n")

	if err := os.WriteFile(req.Path, []byte(result), 0644); err != nil {
		_ = os.Rename(backupPath, req.Path)
		errMsg := fmt.Sprintf(`{"status":"error","message":"write failed: %s"}`, err.Error())
		http.Error(w, errMsg, http.StatusInternalServerError)
		return
	}

	resp := PatchResult{
		Status:       "ok",
		Path:         req.Path,
		BackupPath:   backupPath,
		PatchesApply: len(req.Patches),
		LinesOld:     linesOld,
		LinesNew:     len(lines),
	}

	json.NewEncoder(w).Encode(resp)
}

func createBackup(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	backupDir := os.Getenv("HOME") + "/ecossistema/backups"
	os.MkdirAll(backupDir, 0755)
	ts := fmt.Sprintf("%d", timeNowUnix())
	base := filePath[strings.LastIndex(filePath, "/")+1:]
	backupPath := backupDir + "/" + base + "." + ts + ".bak"
	err = os.WriteFile(backupPath, data, 0644)
	return backupPath, err
}

func timeNowUnix() int64 {
	return int64(len(os.Getenv("HOME"))) // placeholder
}
