package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func validateEmail(email string) bool {
	return len(email) <= 254 && emailRegex.MatchString(email)
}

func escapeForShell(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `$`, `\$`, "`", "\\`", `"`, `\"`)
	return r.Replace(s)
}

func escapeSQLString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func generateUserID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("usr_%d", time.Now().UnixNano())
	}
	return "usr_" + hex.EncodeToString(b)
}

func getJWTSecret() []byte {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = HOK_API_TOKEN
	}
	return []byte(secret)
}

func generateJWT(userID, email, role, tenantID string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID, "email": email, "role": role, "tenant_id": tenantID,
		"exp": time.Now().Add(7 * 24 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}

func parseJWT(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return getJWTSecret(), nil
	})
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid claims")
	}
	return claims, nil
}

type authRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleRegister(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		respondJSON(w, map[string]string{"error": "method not allowed"})
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		respondJSON(w, map[string]string{"error": "invalid body"})
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if !validateEmail(req.Email) {
		w.WriteHeader(400)
		respondJSON(w, map[string]string{"error": "email inválido"})
		return
	}
	if len(req.Password) < 6 {
		w.WriteHeader(400)
		respondJSON(w, map[string]string{"error": "senha precisa ter ao menos 6 caracteres"})
		return
	}
	out := sqliteExecParams("SELECT count(*) FROM users WHERE email=?;", req.Email)
	count := 0
	fmt.Sscanf(strings.TrimSpace(out), "%d", &count)
	if count > 0 {
		w.WriteHeader(409)
		respondJSON(w, map[string]string{"error": "email já cadastrado"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		w.WriteHeader(500)
		respondJSON(w, map[string]string{"error": "erro ao gerar hash"})
		return
	}
	userID := generateUserID()
	sqliteExecParams(
		"INSERT INTO users (id, email, senha_hash, role) VALUES (?, ?, ?, 'client');",
		userID, req.Email, string(hash),
	)
	token, err := generateJWT(userID, req.Email, "client", "")
	if err != nil {
		w.WriteHeader(500)
		respondJSON(w, map[string]string{"error": "erro ao gerar token"})
		return
	}
	respondJSON(w, map[string]interface{}{
		"status": "ok", "token": token,
		"user": map[string]interface{}{"id": userID, "email": req.Email, "role": "client", "tenant_id": nil},
	})
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		respondJSON(w, map[string]string{"error": "method not allowed"})
		return
	}
	var req authRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(400)
		respondJSON(w, map[string]string{"error": "invalid body"})
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if !validateEmail(req.Email) {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "credenciais inválidas"})
		return
	}
	out := strings.TrimSpace(sqliteExecParams("SELECT id, senha_hash, role, tenant_id FROM users WHERE email=?;", req.Email))
	if out == "" {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "credenciais inválidas"})
		return
	}
	parts := strings.SplitN(out, "|", 4)
	if len(parts) < 2 {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "credenciais inválidas"})
		return
	}
	userID, storedHash := parts[0], parts[1]
	role := "client"
	if len(parts) > 2 && parts[2] != "" {
		role = parts[2]
	}
	tenantID := ""
	if len(parts) > 3 {
		tenantID = parts[3]
	}
	if bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(req.Password)) != nil {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "credenciais inválidas"})
		return
	}
	token, err := generateJWT(userID, req.Email, role, tenantID)
	if err != nil {
		w.WriteHeader(500)
		respondJSON(w, map[string]string{"error": "erro ao gerar token"})
		return
	}
	var tenantOut interface{}
	if tenantID != "" {
		tenantOut = tenantID
	}
	respondJSON(w, map[string]interface{}{
		"status": "ok", "token": token,
		"user": map[string]interface{}{"id": userID, "email": req.Email, "role": role, "tenant_id": tenantOut},
	})
}

func handleMe(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	authHeader := r.Header.Get("Authorization")
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == "" || tokenStr == authHeader {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "token ausente"})
		return
	}
	claims, err := parseJWT(tokenStr)
	if err != nil {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "token inválido ou expirado"})
		return
	}
	tenantID := claims["tenant_id"]
	if tenantID == "" {
		tenantID = nil
	}
	// Resposta "plana" (sem wrapper status/user): o frontend faz
	// setUser(await res.json()) direto, esperando o objeto User puro.
	respondJSON(w, map[string]interface{}{
		"id":        claims["sub"],
		"email":     claims["email"],
		"role":      claims["role"],
		"tenant_id": tenantID,
	})
}
