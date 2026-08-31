package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// writeJSON — serializa v em JSON pretty e escreve em path
func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}

// readJSONInto — desserializa JSON de path em v (espera-se ponteiro)
func readJSONInto(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// readJSON — helper genérico (T any)
func readJSON[T any](path string) (*T, error) {
	var v T
	if err := readJSONInto(path, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

// sha256hex — hash hex de uma string (normalizada)
func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// log — wrapper de log com timestamp
func log(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[oca] "+format+"\n", args...)
}