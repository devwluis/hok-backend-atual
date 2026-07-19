package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
)

type PipelineCheckpoint struct {
	Task      string               `json:"task"`
	Steps     []PipelineStepResult `json:"steps"`
	NextInput string               `json:"next_input"`
}

func checkpointDir() string {
	dir := filepath.Join(ROOT_PATH, "backend", "pipeline_checkpoints")
	os.MkdirAll(dir, 0755)
	return dir
}

func checkpointFile(pipelineID string) string {
	h := sha1.Sum([]byte(pipelineID))
	return filepath.Join(checkpointDir(), hex.EncodeToString(h[:])+".json")
}

func loadCheckpoint(pipelineID string) (*PipelineCheckpoint, bool) {
	if pipelineID == "" {
		return nil, false
	}
	data, err := os.ReadFile(checkpointFile(pipelineID))
	if err != nil {
		return nil, false
	}
	var cp PipelineCheckpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, false
	}
	return &cp, true
}

func saveCheckpoint(pipelineID string, cp PipelineCheckpoint) {
	if pipelineID == "" {
		return
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return
	}
	os.WriteFile(checkpointFile(pipelineID), data, 0644)
}

func deleteCheckpoint(pipelineID string) {
	if pipelineID == "" {
		return
	}
	os.Remove(checkpointFile(pipelineID))
}
