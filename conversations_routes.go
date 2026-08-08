package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var validConvID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ── GET /conversations ────────────────────────────────────────
// Lista todas as conversas ordenadas por updated_at DESC
func handleGetConversations(w http.ResponseWriter, r *http.Request) {
	out := sqliteExec(fmt.Sprintf(
		`SELECT id, title, project, model, created_at, updated_at FROM conversations ORDER BY updated_at DESC LIMIT 200;`,
	))
	var items []map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 6)
		if len(parts) == 6 {
			items = append(items, map[string]interface{}{
				"id":         parts[0],
				"title":      parts[1],
				"project":    parts[2],
				"model":      parts[3],
				"created_at": parts[4],
				"updated_at": parts[5],
			})
		}
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
	out := sqliteExec(fmt.Sprintf(
		`SELECT id, role, content, ts, attachments FROM conv_messages WHERE conv_id='%s' ORDER BY ts ASC;`,
		strings.ReplaceAll(convID, "'", "''")))
	var items []map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, "|", 5)
		if len(parts) >= 4 {
			att := ""
			if len(parts) == 5 {
				att = parts[4]
			}
			items = append(items, map[string]interface{}{
				"id":          parts[0],
				"role":        parts[1],
				"content":     parts[2],
				"ts":          parts[3],
				"attachments": att,
			})
		}
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
	id := strings.ReplaceAll(body.ID, "'", "''")
	title := strings.ReplaceAll(body.Title, "'", "''")
	project := strings.ReplaceAll(body.Project, "'", "''")
	model := strings.ReplaceAll(body.Model, "'", "''")

	// Upsert conversa
	sqliteExec(fmt.Sprintf(
		`INSERT OR REPLACE INTO conversations (id, title, project, model, created_at, updated_at)
		 VALUES ('%s', '%s', '%s', '%s',
		   COALESCE((SELECT created_at FROM conversations WHERE id='%s'), %d), %d);`,
		id, title, project, model, id, now, now))

	// Salva mensagens (limpa e re-insere)
	if len(body.Messages) > 0 {
		sqliteExec(fmt.Sprintf(`DELETE FROM conv_messages WHERE conv_id='%s';`, id))
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
			mID := strings.ReplaceAll(msgID, "'", "''")
			mRole := strings.ReplaceAll(role, "'", "''")
			mContent := strings.ReplaceAll(content, "'", "''")
			mAtt := strings.ReplaceAll(attJSON, "'", "''")
			sqliteExec(fmt.Sprintf(
				`INSERT OR IGNORE INTO conv_messages (id, conv_id, role, content, ts, attachments)
				 VALUES ('%s', '%s', '%s', '%s', %d, '%s');`,
				mID, id, mRole, mContent, tsInt, mAtt))
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
	id := strings.ReplaceAll(convID, "'", "''")
	sqliteExec(fmt.Sprintf(`DELETE FROM conversations WHERE id='%s';`, id))
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
