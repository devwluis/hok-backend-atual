package main

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var validConvID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ── GET /conversations ────────────────────────────────────────
// Lista conversas do tenant da requisicao (via JWT), mantendo as
// conversas legadas sem tenant_id visiveis. Parametrizado, sem
// interpolacao de string. Sem auth valida o router ja responde 401.
func handleGetConversations(w http.ResponseWriter, r *http.Request) {
	tenantID := tenantIdFromRequest(r)
	out := sqliteExecQuoted(
		`SELECT id, title, project, model, created_at, updated_at FROM conversations
		 WHERE tenant_id = ? OR tenant_id IS NULL OR tenant_id = ''
		 ORDER BY updated_at DESC LIMIT 200;`, tenantID)
	var items []map[string]interface{}
	for _, fields := range parseQuotedRows(out, 6) {
		items = append(items, map[string]interface{}{
			"id":         fields[0],
			"title":      fields[1],
			"project":    fields[2],
			"model":      fields[3],
			"created_at": fields[4],
			"updated_at": fields[5],
		})
	}
	if items == nil {
		items = []map[string]interface{}{}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "conversations": items})
}

// ── GET /conversations/{id}/messages ─────────────────────────
func handleGetConvMessages(w http.ResponseWriter, r *http.Request, convID string) {
	if !validConvID.MatchString(convID) {
		http.Error(w, "conversation id invalido", 400)
		return
	}
	out := sqliteExecQuoted(`SELECT id, role, content, ts, attachments FROM conv_messages WHERE conv_id=? ORDER BY ts ASC;`, convID)
	var items []map[string]interface{}
	for _, fields := range parseQuotedRows(out, 5) {
		att := ""
		if fields[4] != "" {
			att = fields[4]
		}
		items = append(items, map[string]interface{}{
			"id":          fields[0],
			"role":        fields[1],
			"content":     fields[2],
			"ts":          fields[3],
			"attachments": att,
		})
	}
	if items == nil {
		items = []map[string]interface{}{}
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "messages": items})
}

// ── POST /conversations ───────────────────────────────────────
// Cria ou atualiza conversa + salva mensagens
func handleSaveConversation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID       string                   `json:"id"`
		Title    string                   `json:"title"`
		Project  string                   `json:"project"`
		Model    string                   `json:"model"`
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondJSON(w, map[string]string{"status": "error", "message": "bad request"})
		return
	}
	if body.ID == "" {
		respondJSON(w, map[string]string{"status": "error", "message": "id obrigatório"})
		return
	}
	if body.Title == "" {
		body.Title = "Nova conversa"
	}
	if body.Project == "" {
		body.Project = "default"
	}
	if body.Model == "" {
		body.Model = "default"
	}

	now := time.Now().Unix()
	// tenantIdFromRequest segue o mesmo padrao do GET: JWT -> tenant_id,
	// sem JWT valido -> "owner" (nunca null/vazio silenciosamente).
	tenantID := tenantIdFromRequest(r)

	// Upsert conversa
	sqliteExecParams(
		`INSERT OR REPLACE INTO conversations (id, title, project, model, created_at, updated_at, tenant_id)
		 VALUES (?, ?, ?, ?, COALESCE((SELECT created_at FROM conversations WHERE id=?), ?), ?, ?);`,
		body.ID, body.Title, body.Project, body.Model, body.ID, now, now, tenantID)

	// Salva mensagens (limpa e re-insere)
	if len(body.Messages) > 0 {
		sqliteExecParams(`DELETE FROM conv_messages WHERE conv_id=?;`, body.ID)
		for _, m := range body.Messages {
			msgID, _ := m["id"].(string)
			role, _ := m["role"].(string)
			content, _ := m["content"].(string)
			ts, _ := m["ts"].(float64)
			if msgID == "" || role == "" {
				continue
			}
			attJSON := ""
			if att, ok := m["attachments"]; ok && att != nil {
				if b, err := json.Marshal(att); err == nil {
					attJSON = string(b)
				}
			}
			tsInt := int64(ts)
			if tsInt == 0 {
				tsInt = now
			}
			sqliteExecParams(
				`INSERT OR IGNORE INTO conv_messages (id, conv_id, role, content, ts, attachments)
				 VALUES (?, ?, ?, ?, ?, ?);`,
				msgID, body.ID, role, content, tsInt, attJSON)
		}
	}

	respondJSON(w, map[string]string{"status": "ok", "id": body.ID})
}

// ── DELETE /conversations/{id} ────────────────────────────────
func handleDeleteConversation(w http.ResponseWriter, r *http.Request, convID string) {
	if !validConvID.MatchString(convID) {
		http.Error(w, "conversation id invalido", 400)
		return
	}
	sqliteExecParams(`DELETE FROM conversations WHERE id=?;`, convID)
	respondJSON(w, map[string]string{"status": "ok"})
}

// ── Router /conversations ─────────────────────────────────────
func handleConversations(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}

	if !requireOwnerToken(w, r) {
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/conversations")
	path = strings.TrimPrefix(path, "/")

	switch r.Method {
	case "GET":
		if path == "" {
			handleGetConversations(w, r)
		} else {
			parts := strings.SplitN(path, "/", 2)
			if len(parts) == 2 && parts[1] == "messages" {
				handleGetConvMessages(w, r, parts[0])
			} else {
				http.Error(w, "not found", 404)
			}
		}
	case "POST":
		handleSaveConversation(w, r)
	case "DELETE":
		if path != "" {
			handleDeleteConversation(w, r, path)
		} else {
			http.Error(w, "id obrigatório", 400)
		}
	default:
		http.Error(w, "method not allowed", 405)
	}
}
