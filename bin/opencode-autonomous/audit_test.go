package main

import (
	"encoding/json"
	"testing"
)

// TestPartDataParse — verifica que parse do JSON de part.data funciona
func TestPartDataParse(t *testing.T) {
	raw := `{
		"type": "tool",
		"tool": "bash",
		"callID": "call_01a0543465e971b2b2768417",
		"state": {
			"status": "completed",
			"input": {"command": "echo CB_PROBE"},
			"output": "CB_PROBE\n",
			"metadata": {"output": "CB_PROBE\n", "exit": 0, "truncated": false},
			"time": {"start": 1788119116087, "end": 1788119116093}
		}
	}`
	var p PartData
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Type != "tool" {
		t.Errorf("type = %q, want tool", p.Type)
	}
	if p.Tool != "bash" {
		t.Errorf("tool = %q, want bash", p.Tool)
	}
	if p.State.Status != "completed" {
		t.Errorf("status = %q, want completed", p.State.Status)
	}
	if p.State.Input["command"] != "echo CB_PROBE" {
		t.Errorf("input.command = %v, want echo CB_PROBE", p.State.Input["command"])
	}
	if p.State.Time["start"] != 1788119116087 {
		t.Errorf("start = %d, want 1788119116087", p.State.Time["start"])
	}
}

// TestPartDataParseError — verifica que erro é tratado gracefully
func TestPartDataParseError(t *testing.T) {
	var p PartData
	err := json.Unmarshal([]byte("{invalid json"), &p)
	if err == nil {
		t.Fatal("esperava erro, veio nil")
	}
}

// TestHashFromPartInput — verifica que hash determinístico do input
// funciona (essencial pro CB).
func TestHashFromPartInput(t *testing.T) {
	p1 := PartData{State: PartState{Input: map[string]interface{}{"command": "ls -la"}}}
	p2 := PartData{State: PartState{Input: map[string]interface{}{"command": "ls -la"}}}
	p3 := PartData{State: PartState{Input: map[string]interface{}{"command": "ls"}}}
	j1, _ := json.Marshal(p1.State.Input)
	j2, _ := json.Marshal(p2.State.Input)
	j3, _ := json.Marshal(p3.State.Input)
	h1 := sha256hex(string(j1))
	h2 := sha256hex(string(j2))
	h3 := sha256hex(string(j3))
	if h1 != h2 {
		t.Errorf("mesmo input deve ter mesmo hash: %s != %s", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("inputs diferentes devem ter hash diferentes")
	}
}