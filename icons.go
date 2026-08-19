package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// iconsDriveIDs — nomes usados pelo frontend -> file ID no Google Drive.
// Arquivos na pasta "Icones-Hok" (Meu Drive, conta gestordeanunciosbr@gmail.com),
// mesma conta do Google Drive do n8n (credential "Google Drive account 2").
var iconsDriveIDs = map[string]string{
	"chat-hok.png":     "1bx_W3N-zy4CtXzudGRCNjzJRRffxBOXc",
	"terminal.png":     "11qHqBn0aerkuWnz0GS2yxBnHkTd32W1l",
	"n8n.png":          "1eqcmik_OGQPv61olufvhYbgANTNE2htW",
	"modelos.png":      "1WK55dxqr7Hoda1-omyWf071LeR40ECFJ",
	"configuracao.png": "17eCh_FHXyfplD3b-QoHyOIgMSwaT-SSg",
	"claude-code.png":  "1DYq1b7CGlc2tkN73vwFeB3bj1ls0ABU4",
	"hermes.png":       "1cprcVg_i34NiDBQTbzlOC-3FeXGt5Yar",
	"opencode.png":     "1W3ODkwZza0p8s6IwJBzwIW7gC5002NBn",
}

const iconsTTL = 24 * time.Hour

var (
	iconsCacheDir string
	iconsMu       sync.Mutex
	errIconUnknown    = errors.New("icon desconhecido")
	errDriveCredsMissing = errors.New("credenciais do Drive ausentes")
)

func iconsCacheDirInit() string {
	if iconsCacheDir != "" {
		return iconsCacheDir
	}
	iconsCacheDir = os.Getenv("ICONS_CACHE_DIR")
	if iconsCacheDir == "" {
		iconsCacheDir = "/root/hokma/backend/icons_cache"
	}
	os.MkdirAll(iconsCacheDir, 0o755)
	return iconsCacheDir
}

func iconCachePath(name string) string {
	return filepath.Join(iconsCacheDirInit(), name)
}

// driveAccessToken — busca um access token novo com o refresh token da conta
// do Google Drive usada pelo n8n (credenciais via env, extraídas do n8n).

func driveCreds() (clientID, clientSecret, refreshToken string) {
	clientID = os.Getenv("DRIVE_CLIENT_ID")
	clientSecret = os.Getenv("DRIVE_CLIENT_SECRET")
	refreshToken = os.Getenv("DRIVE_REFRESH_TOKEN")
	if clientID != "" && clientSecret != "" && refreshToken != "" {
		return
	}
	data, err := os.ReadFile("/root/hokma/backend/drive_creds.env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.TrimSpace(parts[0]) {
		case "DRIVE_CLIENT_ID":
			clientID = strings.TrimSpace(parts[1])
		case "DRIVE_CLIENT_SECRET":
			clientSecret = strings.TrimSpace(parts[1])
		case "DRIVE_REFRESH_TOKEN":
			refreshToken = strings.TrimSpace(parts[1])
		}
	}
	return
}

func driveAccessToken() (string, error) {
	clientID, clientSecret, refreshToken := driveCreds()
	if clientID == "" || clientSecret == "" || refreshToken == "" {
		return "", errDriveCredsMissing
	}
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	req, err := http.NewRequest("POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", &driveTokenError{tok.Error, string(body)}
	}
	return tok.AccessToken, nil
}

type driveTokenError struct{ err, body string }

func (e *driveTokenError) Error() string { return "drive token: " + e.err + ": " + e.body }

// downloadDriveFile — baixa um arquivo do Drive por file ID e salva em dest.
func downloadDriveFile(fileID, dest string) error {
	token, err := driveAccessToken()
	if err != nil {
		return err
	}
	u := "https://www.googleapis.com/drive/v3/files/" + fileID + "?alt=media"
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return &driveDownloadError{fileID, resp.StatusCode, string(body)}
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return nil
}

type driveDownloadError struct {
	fileID string
	status int
	body   string
}

func (e *driveDownloadError) Error() string {
	return "download " + e.fileID + ": HTTP " + http.StatusText(e.status) + ": " + e.body
}

// ensureIcon — garante ícone local (busca no Drive se ausente/expirado).
func ensureIcon(name string) (string, error) {
	iconsMu.Lock()
	defer iconsMu.Unlock()
	fileID, ok := iconsDriveIDs[name]
	if !ok {
		return "", errIconUnknown
	}
	path := iconCachePath(name)
	if fi, err := os.Stat(path); err == nil && time.Since(fi.ModTime()) < iconsTTL {
		return path, nil
	}
	if err := downloadDriveFile(fileID, path); err != nil {
		return "", err
	}
	log.Printf("[icons] cache atualizado: %s (fileID=%s)", name, fileID)
	return path, nil
}

// refreshAllIcons — re-baixa todos os ícones (revalidação sob demanda).
func refreshAllIcons() error {
	iconsMu.Lock()
	defer iconsMu.Unlock()
	var firstErr error
	for name, fileID := range iconsDriveIDs {
		path := iconCachePath(name)
		if err := downloadDriveFile(fileID, path); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		log.Printf("[icons] refresh: %s (fileID=%s)", name, fileID)
	}
	return firstErr
}

// handleIconsGet — GET /icons/{nome} (público, para o frontend).
func handleIconsGet(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/icons/")
	name = filepath.Base(name)
	if name == "." || name == "/" || name == "" {
		http.NotFound(w, r)
		return
	}
	path, err := ensureIcon(name)
	if err != nil {
		log.Printf("[icons] GET %s: %v", name, err)
		if err == errIconUnknown {
			http.NotFound(w, r)
			return
		}
		http.Error(w, `{"status":"error"}`, http.StatusBadGateway)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, `{"status":"error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	w.Write(data)
}

// handleIconsRefresh — POST /icons/refresh (autenticado) revalida o cache.
func handleIconsRefresh(w http.ResponseWriter, r *http.Request) {
	if !tokenMatches(strings.TrimSpace(r.Header.Get("X-Hok-Token"))) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status":"unauthorized"}`))
		return
	}
	if err := refreshAllIcons(); err != nil {
		log.Printf("[icons] refresh todos falhou: %v", err)
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"status":"error","detail":"` + err.Error() + `"}`))
		return
	}
	w.Write([]byte(`{"status":"ok","count":` + fmt.Sprintf("%d", len(iconsDriveIDs)) + `}`))
}
