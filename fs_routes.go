package main

import (
	"encoding/json"
	"fmt"
	"log"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ── Tipos ─────────────────────────────────────────────────────────────────────

type FileReadRequest struct {
	Path string `json:"path"`
}

type FileWriteRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Backup  bool   `json:"backup"` // true = salva .bak antes de escrever
}

type FileListRequest struct {
	Path string `json:"path"`
}

type ExecRequest struct {
	Command string `json:"command"` // comando shell a executar
	Timeout int    `json:"timeout"` // segundos (default 30)
}

type FSResponse struct {
	Status  string      `json:"status"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Elapsed string      `json:"elapsed,omitempty"`
}

// ── Segurança: paths permitidos ───────────────────────────────────────────────

var allowedRoots = []string{
	os.Getenv("HOME") + "/hokma",
	os.Getenv("HOME") + "/.keys",
	os.Getenv("HOME") + "/storage/downloads",
}

func isPathAllowed(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	for _, root := range allowedRoots {
		if abs == root || strings.HasPrefix(abs, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func fsJSON(w http.ResponseWriter, status int, resp FSResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// ── GET /fs/read ──────────────────────────────────────────────────────────────
// Body: {"path": "~/hokma/backend/agent_loop.go"}

func handleFileRead(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}

	if !requireHokAuth(w, r) {
		return
	}

	var req FileReadRequest
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		fsJSON(w, 400, FSResponse{Status: "error", Error: "JSON inválido"})
		return
	}

	// Expande ~
	path := expandHome(req.Path)

	if !isPathAllowed(path) {
		fsJSON(w, 403, FSResponse{Status: "error", Error: "path não permitido: " + path})
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fsJSON(w, 404, FSResponse{Status: "error", Error: err.Error()})
		return
	}

	info, _ := os.Stat(path)
	fsJSON(w, 200, FSResponse{
		Status: "ok",
		Data: map[string]interface{}{
			"path":     path,
			"content":  string(data),
			"size":     info.Size(),
			"modified": info.ModTime().Format(time.RFC3339),
		},
	})
}

// ── POST /fs/write ────────────────────────────────────────────────────────────
// Body: {"path": "...", "content": "...", "backup": true}

func handleFileWrite(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}

	if !requireHokAuth(w, r) {
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	var req FileWriteRequest
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		fsJSON(w, 400, FSResponse{Status: "error", Error: "JSON inválido"})
		return
	}

	path := expandHome(req.Path)

	if !isPathAllowed(path) {
		fsJSON(w, 403, FSResponse{Status: "error", Error: "path não permitido"})
		return
	}

	// Backup opcional
	if req.Backup {
		ts := time.Now().Format("20060102_150405")
		bakPath := path + ".bak_" + ts
		if orig, err := os.ReadFile(path); err == nil {
			os.WriteFile(bakPath, orig, 0644)
		}
	}

	// Cria diretório se não existir
	os.MkdirAll(filepath.Dir(path), 0755)

	if err := os.WriteFile(path, []byte(req.Content), 0644); err != nil {
		fsJSON(w, 500, FSResponse{Status: "error", Error: err.Error()})
		return
	}

	fsJSON(w, 200, FSResponse{
		Status: "ok",
		Data:   map[string]interface{}{"path": path, "bytes": len(req.Content)},
	})
}

// ── GET /fs/list ──────────────────────────────────────────────────────────────
// Body: {"path": "~/hokma/backend"}

func handleFileList(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}

	if !requireHokAuth(w, r) {
		return
	}

	var req FileListRequest
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		fsJSON(w, 400, FSResponse{Status: "error", Error: "JSON inválido"})
		return
	}

	path := expandHome(req.Path)

	if !isPathAllowed(path) {
		fsJSON(w, 403, FSResponse{Status: "error", Error: "path não permitido"})
		return
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		fsJSON(w, 404, FSResponse{Status: "error", Error: err.Error()})
		return
	}

	var files []map[string]interface{}
	for _, e := range entries {
		info, _ := e.Info()
		files = append(files, map[string]interface{}{
			"name":     e.Name(),
			"is_dir":   e.IsDir(),
			"size":     info.Size(),
			"modified": info.ModTime().Format(time.RFC3339),
		})
	}

	fsJSON(w, 200, FSResponse{Status: "ok", Data: files})
}

// ── POST /fs/exec ─────────────────────────────────────────────────────────────
// Body: {"command": "go build -o hokma .", "timeout": 60}
// ATENÇÃO: endpoint poderoso — só use internamente

func handleExec(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}

	if !requireHokAuth(w, r) {
		return
	}

	// Só aceita requests locais
	ip := strings.Split(r.RemoteAddr, ":")[0]
	if ip != "127.0.0.1" && ip != "::1" {
		fsJSON(w, 403, FSResponse{Status: "error", Error: "exec só permitido localmente"})
		return
	}

	var req ExecRequest
	body, _ := io.ReadAll(r.Body)
	if err := json.Unmarshal(body, &req); err != nil {
		fsJSON(w, 400, FSResponse{Status: "error", Error: "JSON inválido"})
		return
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = 30
	}

	convID := r.Header.Get("X-Conversation-Id")
        userID := userIdFromRequest(r)
	if convID == "" {
		convID = "default"
	}
	tenantID := tenantIdFromRequest(r)
	action, err := registerFsExecPendingAction(convID, tenantID, userID, req.Command, false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"pending_action": action,
		"message": "Comando registrado para aprovacao. Use /actions/approve para executar.",
	})
	return
	}

// ── POST /fs/rebuild ─────────────────────────────────────────────────────────
// Sinaliza watchdog para rebuild sem precisar matar o processo atual

func handleRebuild(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		return
	}

	if !requireHokAuth(w, r) {
		return
	}
	// Cria arquivo de sinal para o watchdog
	flagPath := os.Getenv("HOME") + "/hokma/rebuild_requested"
	if err := os.WriteFile(flagPath, []byte(time.Now().Format(time.RFC3339)), 0644); err != nil {
		fsJSON(w, 500, FSResponse{Status: "error", Error: err.Error()})
		return
	}

	fsJSON(w, 200, FSResponse{
		Status: "ok",
		Data:   map[string]string{"message": "rebuild sinalizado para o watchdog"},
	})
}

// ── Helper ────────────────────────────────────────────────────────────────────

func expandHome(path string) string {
	home := os.Getenv("HOME")
	if strings.HasPrefix(path, "~/") {
		return home + path[1:]
	}
	if path == "~" {
		return home
	}
	return path
}

// ── Registra rotas (chame isso no main ou routes) ─────────────────────────────

func registerFSRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/fs/read", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "OPTIONS" && !requireHokAuth(w, r) {
			return
		}
		handleFileRead(w, r)
	})
	mux.HandleFunc("/fs/write", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "OPTIONS" && !requireHokAuth(w, r) {
			return
		}
		handleFileWrite(w, r)
	})
	mux.HandleFunc("/fs/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "OPTIONS" && !requireHokAuth(w, r) {
			return
		}
		handleFileList(w, r)
	})
	mux.HandleFunc("/fs/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "OPTIONS" && !requireHokAuth(w, r) {
			return
		}
		handleExec(w, r)
	})
	mux.HandleFunc("/fs/rebuild", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "OPTIONS" && !requireHokAuth(w, r) {
			return
		}
		handleRebuild(w, r)
	})
}

// === FASE 2b: Registro de Pending Action para FS Exec ===
func registerFsExecPendingAction(convID string, tenantID string, userID string, command string, selfMod bool) (*PendingAction, error) {
    actionID := fmt.Sprintf("fs_exec_%s_%d", convID, time.Now().UnixNano())
    diffPreview := fmt.Sprintf("=== COMANDO BASH ===\n%s\n===================", command)
    actionType := "fs_exec"
    if selfMod {
        actionType = "self_mod"
    }
    action := &PendingAction{
        ID:          actionID,
        ToolName:    "fs_exec",
        Description: "Execucao de comando bash: " + command,
        CreatedAt:   time.Now(),
        ActionType:  actionType,
        DiffPreview: diffPreview,
    }
    key := tenantID + ":" + userID + ":" + convID
    pendingActionMu.Lock()
    pendingActionMap[key] = action
    pendingActionMu.Unlock()
    pendingExecMu.Lock()
    pendingExecCommands[actionID] = command
    pendingExecMu.Unlock()
    log.Printf("[AUDIT] PendingAction registrada fs_exec actionID=%s tenant=%s conv=%s", actionID, tenantID, convID)
    return action, nil
}
