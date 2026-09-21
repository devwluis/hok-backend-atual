package main

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func init() {
	if jwtSecret == nil {
		jwtSecret = []byte("test-secret-12345")
	}
	if HOK_API_TOKEN == "" {
		HOK_API_TOKEN = "test-hok-token-12345"
	}
}

func generateJWT(sub, email, role, tenantID string) (string, error) {
	claims := jwt.MapClaims{
		"sub":   sub,
		"email": email,
		"role":  role,
		"exp":   time.Now().Add(30 * time.Minute).Unix(),
		"iat":   time.Now().Unix(),
		"jti":   generateUserID(),
	}
	if tenantID != "" {
		claims["tenant_id"] = tenantID
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}