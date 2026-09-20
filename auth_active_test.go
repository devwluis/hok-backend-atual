package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func TestActiveUserBlocking(t *testing.T) {
	dbLocal, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer dbLocal.Close()
	dbLocal.SetMaxOpenConns(1)

	origDB := db
	db = dbLocal
	defer func() { db = origDB }()

	origDBPath := DB_PATH
	DB_PATH = "file::memory:?cache=shared"
	defer func() { DB_PATH = origDBPath }()

	schema := []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, senha_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'client', tenant_id TEXT, criado_em INTEGER NOT NULL DEFAULT (unixepoch()), active INTEGER DEFAULT 1);`,
		`CREATE TABLE user_totp (email TEXT PRIMARY KEY, secret TEXT NOT NULL, recovery_hashes TEXT NOT NULL, enabled INTEGER DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()));`,
		`CREATE TABLE login_attempts (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT, action TEXT, success INTEGER DEFAULT 0, ts INTEGER DEFAULT (unixepoch()));`,
		`CREATE TABLE sessions (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, email TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'client', token_hash TEXT NOT NULL, exp INTEGER NOT NULL, revoked INTEGER DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()), last_used INTEGER DEFAULT (unixepoch()));`,
	}
	for _, tbl := range schema {
		if _, err := db.Exec(tbl); err != nil {
			t.Fatal(err)
		}
	}

	os.Setenv("JWT_SECRET", "test-secret-12345")
	defer os.Unsetenv("JWT_SECRET")
	jwtSecret = []byte("test-secret-12345")

	os.Setenv("HOK_TOKEN", "test-hok-token-12345")
	defer os.Unsetenv("HOK_TOKEN")
	HOK_API_TOKEN = "test-hok-token-12345"

	hash, err := bcrypt.GenerateFromPassword([]byte("testpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec("INSERT INTO users (id, email, senha_hash, role, active) VALUES (?, ?, ?, ?, 0)",
		"usr_disabled", "disabled@test.com", string(hash), "client"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec("INSERT INTO users (id, email, senha_hash, role, active) VALUES (?, ?, ?, ?, 1)",
		"usr_active", "active@test.com", string(hash), "client"); err != nil {
		t.Fatal(err)
	}

	t.Run("login_active0_returns_401_no_token", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"email": "disabled@test.com", "password": "testpass123"})
		req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handleLogin(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("esperado 401, obteve %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if _, ok := resp["token"]; ok {
			t.Error("token nao deveria existir para usuario ativo=0")
		}
	})

	t.Run("login_active1_returns_200_with_token", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"email": "active@test.com", "password": "testpass123"})
		req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handleLogin(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("esperado 200, obteve %d", w.Code)
		}
		var resp map[string]interface{}
		json.NewDecoder(w.Body).Decode(&resp)
		if _, ok := resp["token"]; !ok {
			t.Error("token deveria existir para usuario ativo=1")
		}
	})

	t.Run("sessionauth_valid_jwt_active0_rejected", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":   "usr_disabled",
			"email": "disabled@test.com",
			"role":  "client",
			"exp":   time.Now().Add(30 * time.Minute).Unix(),
			"iat":   time.Now().Unix(),
		})
		tokenStr, err := token.SignedString(jwtSecret)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		_, ok := sessionAuth(req)
		if ok {
			t.Error("sessionAuth deveria rejeitar JWT de usuario ativo=0")
		}
	})

	t.Run("sessionauth_valid_jwt_active1_accepted", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":   "usr_active",
			"email": "active@test.com",
			"role":  "client",
			"exp":   time.Now().Add(30 * time.Minute).Unix(),
			"iat":   time.Now().Unix(),
		})
		tokenStr, err := token.SignedString(jwtSecret)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		_, ok := sessionAuth(req)
		if !ok {
			t.Error("sessionAuth deveria aceitar JWT de usuario ativo=1")
		}
	})

}

func TestActiveUserBlockingErrorCases(t *testing.T) {
	dbLocal, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer dbLocal.Close()
	dbLocal.SetMaxOpenConns(1)

	origDB := db
	db = dbLocal
	defer func() { db = origDB }()

	// Schema SEM coluna active (simula DB antiga / erro de consulta)
	schemaNoActive := []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, senha_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'client', tenant_id TEXT, criado_em INTEGER NOT NULL DEFAULT (unixepoch()));`,
		`CREATE TABLE user_totp (email TEXT PRIMARY KEY, secret TEXT NOT NULL, recovery_hashes TEXT NOT NULL, enabled INTEGER DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()));`,
		`CREATE TABLE login_attempts (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT, action TEXT, success INTEGER DEFAULT 0, ts INTEGER DEFAULT (unixepoch()));`,
		`CREATE TABLE sessions (jti TEXT PRIMARY KEY, user_id TEXT NOT NULL, email TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'client', token_hash TEXT NOT NULL, exp INTEGER NOT NULL, revoked INTEGER DEFAULT 0, created_at INTEGER DEFAULT (unixepoch()), last_used INTEGER DEFAULT (unixepoch()));`,
	}
	for _, tbl := range schemaNoActive {
		if _, err := db.Exec(tbl); err != nil {
			t.Fatal(err)
		}
	}

	os.Setenv("JWT_SECRET", "test-secret-12345")
	defer os.Unsetenv("JWT_SECRET")
	jwtSecret = []byte("test-secret-12345")

	hash, _ := bcrypt.GenerateFromPassword([]byte("testpass123"), bcrypt.DefaultCost)
	if _, err := db.Exec("INSERT INTO users (id, email, senha_hash, role) VALUES (?, ?, ?, ?)",
		"usr_err", "err@test.com", string(hash), "client"); err != nil {
		t.Fatal(err)
	}

	t.Run("handleLogin_query_error_returns_401", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"email": "err@test.com", "password": "testpass123"})
		req := httptest.NewRequest("POST", "/auth/login", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handleLogin(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("esperado 401 para erro de consulta, obteve %d", w.Code)
		}
	})

	t.Run("sessionauth_nonexistent_user_rejected", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":   "usr_nonexistent",
			"email": "nonexistent@test.com",
			"role":  "client",
			"exp":   time.Now().Add(30 * time.Minute).Unix(),
			"iat":   time.Now().Unix(),
		})
		tokenStr, err := token.SignedString(jwtSecret)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		_, ok := sessionAuth(req)
		if ok {
			t.Error("sessionAuth deveria rejeitar usuario inexistente")
		}
	})

	t.Run("sessionauth_query_error_rejected", func(t *testing.T) {
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"sub":   "usr_err",
			"email": "err@test.com",
			"role":  "client",
			"exp":   time.Now().Add(30 * time.Minute).Unix(),
			"iat":   time.Now().Unix(),
		})
		tokenStr, err := token.SignedString(jwtSecret)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("GET", "/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+tokenStr)
		_, ok := sessionAuth(req)
		if ok {
			t.Error("sessionAuth deveria rejeitar quando consulta falha (coluna active inexistente)")
		}
	})
}
