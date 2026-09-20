package main

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

type totpSetupResponse struct {
	Status        string   `json:"status"`
	Secret        string   `json:"secret"`
	QRCodeURL     string   `json:"qr_code_url"`
	RecoveryCodes []string `json:"recovery_codes"`
}

type totpVerifyRequest struct {
	Token string `json:"token"`
}

type totpVerifyResponse struct {
	Status string `json:"status"`
	Token  string `json:"token"`
}

func generateRecoveryCodes() []string {
	codes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 5)
		rand.Read(b)
		codes[i] = strings.ToUpper(base32.HexEncoding.EncodeToString(b))[:10]
	}
	return codes
}

func totpHandleSetup(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, ok := sessionAuth(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		respondJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	email := claims["email"].(string)
	secret := base32.StdEncoding.EncodeToString(makeRandomBytes(20))
	issuer := "Hokma"
	qrURL := "otpauth://totp/" + issuer + ":" + email + "?secret=" + secret + "&issuer=" + issuer
	codes := generateRecoveryCodes()
	hashedCodes := make([]string, len(codes))
	for i, c := range codes {
		h, _ := bcrypt.GenerateFromPassword([]byte(c), bcrypt.DefaultCost)
		hashedCodes[i] = string(h)
	}
	codesJSON, _ := json.Marshal(hashedCodes)
	sqliteExecParams(
		`INSERT OR REPLACE INTO user_totp (email, secret, recovery_hashes, enabled) VALUES (?, ?, ?, 1);`,
		email, secret, string(codesJSON),
	)
	logLoginAttempt(email, "totp_setup", true)
	respondJSON(w, totpSetupResponse{
		Status:        "ok",
		Secret:        secret,
		QRCodeURL:     qrURL,
		RecoveryCodes: codes,
	})
}

func totpHandleVerify(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req totpVerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		w.WriteHeader(http.StatusBadRequest)
		respondJSON(w, map[string]string{"error": "token obrigatório"})
		return
	}
	claims, ok := sessionAuth(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		respondJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	email := claims["email"].(string)
	userID := claims["sub"].(string)
	role := claims["role"].(string)
	out := strings.TrimSpace(sqliteExecParams(
		"SELECT secret, recovery_hashes FROM user_totp WHERE email=?", email,
	))
	if out == "" {
		logLoginAttempt(email, "totp_verify", false)
		w.WriteHeader(http.StatusUnauthorized)
		respondJSON(w, map[string]string{"error": "TOTP não configurado"})
		return
	}
	parts := strings.SplitN(out, "|", 2)
	if len(parts) < 2 {
		logLoginAttempt(email, "totp_verify", false)
		w.WriteHeader(http.StatusUnauthorized)
		respondJSON(w, map[string]string{"error": "TOTP não configurado"})
		return
	}
	secret := parts[0]
	recoveryHashes := parts[1]
	valid := false
	if totp.Validate(req.Token, secret) {
		valid = true
	}
	if !valid {
		for _, line := range strings.Split(recoveryHashes, ",") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if err := bcrypt.CompareHashAndPassword([]byte(line), []byte(req.Token)); err == nil {
				valid = true
				break
			}
		}
	}
	logLoginAttempt(email, "totp_verify", valid)
	if !valid {
		w.WriteHeader(http.StatusUnauthorized)
		respondJSON(w, map[string]string{"error": "token inválido"})
		return
	}
	tok, err := generateSessionJWT(userID, email, role)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		respondJSON(w, map[string]string{"error": "erro ao gerar token"})
		return
	}
	respondJSON(w, totpVerifyResponse{Status: "ok", Token: tok})
}

func totpHandleSessionRefresh(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	claims, ok := sessionAuth(r)
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		respondJSON(w, map[string]string{"error": "unauthorized"})
		return
	}
	userID := claims["sub"].(string)
	email := claims["email"].(string)
	role := claims["role"].(string)
	tok, err := generateSessionJWT(userID, email, role)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		respondJSON(w, map[string]string{"error": "erro ao gerar token"})
		return
	}
	respondJSON(w, map[string]interface{}{"status": "ok", "token": tok})
}

func makeRandomBytes(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}
