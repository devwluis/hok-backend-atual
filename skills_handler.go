package main

import (
	"os"
	"path/filepath"
)

func skillsDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe) + "/skills"
}