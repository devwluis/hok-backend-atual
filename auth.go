package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
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

func generateUserID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("usr_%d", time.Now().UnixNano())
	}
	return "usr_" + hex.EncodeToString(b)
}

var jwtSecret []byte

func initJWTSecret() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		log.Fatal("JWT_SECRET nao definida no .env")
	}
	jwtSecret = []byte(secret)
}

func generateSessionJWT(userID, email, role string) (string, error) {
	claims := jwt.MapClaims{
		"sub": userID, "email": email, "role": role,
		"exp": time.Now().Add(30 * time.Minute).Unix(),
		"iat": time.Now().Unix(),
		"jti": generateUserID(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

func parseSessionJWT(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return jwtSecret, nil
	})
	if err == nil && token.Valid {
		claims, ok := token.Claims.(jwt.MapClaims)
		if ok {
			return claims, nil
		}
	}
	token, err = jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return HOK_API_TOKEN, nil
	})
	if err == nil && token.Valid {
		claims, ok := token.Claims.(jwt.MapClaims)
		if ok {
			return claims, nil
		}
	}
	return nil, fmt.Errorf("invalid token")
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
	if !checkRateLimit(getClientIP(r), 10) {
		w.WriteHeader(429)
		respondJSON(w, map[string]string{"error": "too many requests"})
		return
	}
	// Registration requires X-Hok-Token AND no users yet
	hokTok := r.Header.Get("X-Hok-Token")
	if hokTok == "" || subtle.ConstantTimeCompare([]byte(hokTok), []byte(HOK_API_TOKEN)) != 1 {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	userCount := 0
	out := sqliteExecParams("SELECT count(*) FROM users;")
	fmt.Sscanf(strings.TrimSpace(out), "%d", &userCount)
	if userCount > 0 {
		w.WriteHeader(403)
		respondJSON(w, map[string]string{"error": "primeiro usuario ja cadastrado"})
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
	out = sqliteExecParams("SELECT count(*) FROM users WHERE email=?;", req.Email)
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
	token, err := generateSessionJWT(userID, req.Email, "client")
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
	if !checkRateLimit(getClientIP(r), 10) {
		w.WriteHeader(429)
		respondJSON(w, map[string]string{"error": "too many requests"})
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
		logLoginAttempt(req.Email, "login_fail", false)
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "credenciais inválidas"})
		return
	}
	outActive := strings.TrimSpace(sqliteExecParams("SELECT active FROM users WHERE email=?", req.Email))
	if outActive != "1" {
		logLoginAttempt(req.Email, "login_disabled", true)
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "credenciais inválidas"})
		return
	}
	if isTOTPEnabled(req.Email) {
		logLoginAttempt(req.Email, "login_totp_required", true)
		w.WriteHeader(403)
		respondJSON(w, map[string]string{"error": "totp_required"})
		return
	}
	token, err := generateSessionJWT(userID, req.Email, role)
	if err != nil {
		w.WriteHeader(500)
		respondJSON(w, map[string]string{"error": "erro ao gerar token"})
		return
	}
	logLoginAttempt(req.Email, "login_success", true)
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
	claims, ok := sessionAuth(r)
	if !ok {
		w.WriteHeader(401)
		respondJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	tenantID := claims["tenant_id"]
	if tenantID == "" {
		tenantID = nil
	}
	respondJSON(w, map[string]interface{}{
		"id":        claims["sub"],
		"email":     claims["email"],
		"role":      claims["role"],
		"tenant_id": tenantID,
	})
}

// handleOwnerCheck — valida a senha do dono/administrador no servidor e
// devolve um JWT curto (role owner). Usado pelo OwnerGate do frontend para
// não embutir hash de senha no bundle público. Rate-limited contra brute force.
func handleOwnerCheck(w http.ResponseWriter, r *http.Request) {
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
	if !checkRateLimit(getClientIP(r), 10) {
		w.WriteHeader(429)
		respondJSON(w, map[string]string{"error": "too many requests"})
		return
	}
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		w.WriteHeader(400)
		respondJSON(w, map[string]string{"error": "invalid body"})
		return
	}
	out := strings.TrimSpace(sqliteExecParams(
		"SELECT id, email, senha_hash, role, tenant_id FROM users WHERE role IN ('owner', 'admin');",
	))
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 5)
		if len(parts) < 3 {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(parts[2]), []byte(body.Password)) != nil {
			continue
		}
		userID, email, role := parts[0], parts[1], parts[2]
		tenantID := ""
		if len(parts) > 4 {
			tenantID = parts[4]
		}
		if isTOTPEnabled(email) {
			logLoginAttempt(email, "owner_totp_required", true)
			w.WriteHeader(403)
			respondJSON(w, map[string]string{"error": "totp_required"})
			return
		}
		token, err := generateSessionJWT(userID, email, role)
		if err != nil {
			w.WriteHeader(500)
			respondJSON(w, map[string]string{"error": "erro ao gerar token"})
			return
		}
		logLoginAttempt(email, "owner_login_success", true)
		respondJSON(w, map[string]interface{}{
			"status": "ok",
			"token":  token,
			"user": map[string]interface{}{
				"id": userID, "email": email, "role": role, "tenant_id": tenantID,
			},
		})
		return
	}
	logLoginAttempt("", "owner_login_fail", false)
	w.WriteHeader(401)
	respondJSON(w, map[string]string{"error": "credenciais inválidas"})
}

func isTOTPEnabled(email string) bool {
	out := strings.TrimSpace(sqliteExecParams(
		"SELECT enabled FROM user_totp WHERE email=?;", email,
	))
	return out == "1"
}

func logLoginAttempt(email, action string, success bool) {
	val := 0
	if success {
		val = 1
	}
	sqliteExecParams(
		"INSERT INTO login_attempts (email, action, success, ts) VALUES (?, ?, ?, unixepoch());",
		email, action, val,
	)
}

func escapeSQL(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func sessionAuth(r *http.Request) (jwt.MapClaims, bool) {
	authHeader := r.Header.Get("Authorization")
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr != "" && tokenStr != authHeader {
		claims, err := parseSessionJWT(tokenStr)
		if err == nil {
			email, ok := claims["email"].(string)
			if ok && email != "" {
			out := strings.TrimSpace(sqliteExecParams("SELECT active FROM users WHERE email=?", email))
			if out != "1" {
				return jwt.MapClaims{}, false
			}
			}
			return claims, true
		}
	}
	hokToken := r.Header.Get("X-Hok-Token")
	if hokToken != "" && subtle.ConstantTimeCompare([]byte(hokToken), []byte(HOK_API_TOKEN)) == 1 {
		return jwt.MapClaims{"sub": "service", "email": "service", "role": "owner"}, true
	}
	return nil, false
}

func handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" { w.WriteHeader(204); return }
	if r.Method != "POST" { w.WriteHeader(405); return }
	_, ok := sessionAuth(r)
	if !ok { w.WriteHeader(401); respondJSON(w, map[string]string{"error": "unauthorized"}); return }
	totpHandleSetup(w, r)
}

func handleTOTPVerify(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" { w.WriteHeader(204); return }
	if r.Method != "POST" { w.WriteHeader(405); return }
	totpHandleVerify(w, r)
}

func handleSessionRefresh(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" { w.WriteHeader(204); return }
	if r.Method != "POST" { w.WriteHeader(405); return }
	_, ok := sessionAuth(r)
	if !ok { w.WriteHeader(401); respondJSON(w, map[string]string{"error": "unauthorized"}); return }
	totpHandleSessionRefresh(w, r)
}
