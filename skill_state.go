package main

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
)

func skillStateDir() string {
	dir := filepath.Join(ROOT_PATH, "backend", "skill_state")
	os.MkdirAll(dir, 0755)
	return dir
}

func skillStateFile(skillName string) string {
	h := sha1.Sum([]byte(skillName))
	return filepath.Join(skillStateDir(), hex.EncodeToString(h[:])+".txt")
}

func getSkillLastOutput(skillName string) string {
	data, err := os.ReadFile(skillStateFile(skillName))
	if err != nil {
		return ""
	}
	return string(data)
}

func saveSkillOutput(skillName, output string) {
	os.WriteFile(skillStateFile(skillName), []byte(output), 0644)
}
