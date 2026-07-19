package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)


func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]interface{}{"error": msg, "status": "error"})
}

func n8nBase() string {
	if v := os.Getenv("N8N_BASE_URL"); v != "" { return v }
	return "http://localhost:5678"
}
func n8nKey() string { return os.Getenv("N8N_API_KEY") }
func uid() string    { return fmt.Sprintf("%x", time.Now().UnixNano()) }

type N8NReq struct {
	Name      string `json:"name"`
	Type      string `json:"type"`       // manual | webhook | cron
	Cron      string `json:"cron"`
	ActionURL string `json:"action_url"`
	Method    string `json:"method"`
}

func buildWF(req N8NReq) map[string]interface{} {
	if req.Name == "" { req.Name = "HOK-" + time.Now().Format("0102-1504") }
	url := req.ActionURL
	if url == "" { url = "http://172.17.0.1:8082/health" }
	meth := req.Method
	if meth == "" { meth = "GET" }

	var trig map[string]interface{}
	tName := "Manual Trigger"

	switch req.Type {
	case "webhook":
		tName = "Webhook"
		trig = map[string]interface{}{
			"parameters": map[string]interface{}{
				"path": fmt.Sprintf("hok-%x", time.Now().UnixNano()%99999),
				"httpMethod": "POST", "responseMode": "onReceived",
			},
			"id": uid(), "name": tName,
			"type": "n8n-nodes-base.webhook", "typeVersion": 2.0,
			"position": []int{240, 300},
		}
	case "cron":
		tName = "Schedule Trigger"
		cron := req.Cron
		if cron == "" { cron = "*/5 * * * *" }
		trig = map[string]interface{}{
			"parameters": map[string]interface{}{
				"rule": map[string]interface{}{
					"interval": []interface{}{
						map[string]interface{}{"field": "cronExpression", "expression": cron},
					},
				},
			},
			"id": uid(), "name": tName,
			"type": "n8n-nodes-base.scheduleTrigger", "typeVersion": 1.1,
			"position": []int{240, 300},
		}
	default:
		trig = map[string]interface{}{
			"parameters": map[string]interface{}{},
			"id": uid(), "name": tName,
			"type": "n8n-nodes-base.manualTrigger", "typeVersion": 1.0,
			"position": []int{240, 300},
		}
	}

	httpNode := map[string]interface{}{
		"parameters": map[string]interface{}{
			"method": meth, "url": url,
			"options": map[string]interface{}{},
		},
		"id": uid(), "name": "HTTP Request",
		"type": "n8n-nodes-base.httpRequest", "typeVersion": 4.1,
		"position": []int{480, 300},
	}

	return map[string]interface{}{
		"name":  req.Name,
		"nodes": []interface{}{trig, httpNode},
		"connections": map[string]interface{}{
			tName: map[string]interface{}{
				"main": []interface{}{
					[]interface{}{
						map[string]interface{}{"node": "HTTP Request", "type": "main", "index": 0},
					},
				},
			},
		},
		"settings":   map[string]interface{}{"executionOrder": "v1"},
		"staticData": nil,
	}
}

func n8nCreateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.Error(w, "POST only", 405); return }
	if n8nKey() == "" { jsonError(w, "N8N_API_KEY nao definida", 500); return }
	var req N8NReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "body invalido: "+err.Error(), 400); return
	}
	wf := buildWF(req)
	body, _ := json.Marshal(wf)
	hReq, _ := http.NewRequest("POST", n8nBase()+"/api/v1/workflows", bytes.NewReader(body))
	hReq.Header.Set("Content-Type", "application/json")
	hReq.Header.Set("X-N8N-API-KEY", n8nKey())
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(hReq)
	if err != nil { jsonError(w, "N8N inacessivel: "+err.Error(), 500); return }
	defer resp.Body.Close()
	rb, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		jsonError(w, fmt.Sprintf("N8N %d: %s", resp.StatusCode, string(rb)), 500); return
	}
	var res map[string]interface{}
	json.Unmarshal(rb, &res)
	id := fmt.Sprintf("%v", res["id"])
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     true,
		"workflow_id": id,
		"name":        req.Name,
		"edit_url":    "https://hok.imoveischaves.com/workflow/" + id,
		"message":     "Workflow criado! ID: " + id,
	})
}

func n8nListHandler(w http.ResponseWriter, r *http.Request) {
	if n8nKey() == "" { jsonError(w, "N8N_API_KEY nao definida", 500); return }
	hReq, _ := http.NewRequest("GET", n8nBase()+"/api/v1/workflows", nil)
	hReq.Header.Set("X-N8N-API-KEY", n8nKey())
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(hReq)
	if err != nil { jsonError(w, err.Error(), 500); return }
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}
