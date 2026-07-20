package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

type DeviceCommand struct {
	ID        string `json:"id"`
	Skill     string `json:"skill"`
	Action    string `json:"action"`
	CreatedAt int64  `json:"created_at"`
}

type DeviceResult struct {
	ID     string `json:"id"`
	Output string `json:"output"`
	Error  string `json:"error"`
	DoneAt int64  `json:"done_at"`
}

var (
	deviceQueue   []DeviceCommand
	deviceResults = map[string]DeviceResult{}
	deviceMu      sync.Mutex
	resultReady   = map[string]chan struct{}{}
)

func handleDeviceQueue(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	deviceMu.Lock()
	if len(deviceQueue) == 0 {
		deviceMu.Unlock()
		respondJSON(w, map[string]interface{}{"command": nil})
		return
	}
	cmd := deviceQueue[0]
	deviceQueue = deviceQueue[1:]
	deviceMu.Unlock()

	respondJSON(w, map[string]interface{}{"command": cmd})
}

func handleDeviceResult(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var result DeviceResult
	if err := json.NewDecoder(r.Body).Decode(&result); err != nil {
		http.Error(w, "invalid json", 400)
		return
	}
	result.DoneAt = time.Now().Unix()

	deviceMu.Lock()
	deviceResults[result.ID] = result
	if ch, ok := resultReady[result.ID]; ok {
		close(ch)
		delete(resultReady, result.ID)
	}
	deviceMu.Unlock()

	respondJSON(w, map[string]string{"status": "ok"})
}

func enqueueDeviceCommand(skill, action string) (string, chan struct{}) {
	id := time.Now().Format("20060102150405.000")
	cmd := DeviceCommand{
		ID:        id,
		Skill:     skill,
		Action:    action,
		CreatedAt: time.Now().Unix(),
	}
	ch := make(chan struct{})
	deviceMu.Lock()
	deviceQueue = append(deviceQueue, cmd)
	resultReady[id] = ch
	deviceMu.Unlock()
	return id, ch
}

func getDeviceResult(id string) (DeviceResult, bool) {
	deviceMu.Lock()
	defer deviceMu.Unlock()
	r, ok := deviceResults[id]
	return r, ok
}
