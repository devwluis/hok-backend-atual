package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const caixaPretaFolderID = "16zPoX8HrHOHCZgKezWwNmOEQad1eOBjN"

type driveFile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimeType"`
	Parents  []string `json:"parents,omitempty"`
	Size     string `json:"size,omitempty"`
	ModifiedTime string `json:"modifiedTime,omitempty"`
}

type driveFileList struct {
	Files []driveFile `json:"files"`
	NextPageToken string `json:"nextPageToken,omitempty"`
}

type driveCreateRequest struct {
	Name        string `json:"name"`
	MimeType    string `json:"mimeType,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
}

type driveRenameRequest struct {
	FileID    string `json:"file_id"`
	NewName   string `json:"new_name"`
}

type driveMoveRequest struct {
	FileID    string `json:"file_id"`
	FolderID  string `json:"folder_id"`
	RemoveOld bool   `json:"remove_old,omitempty"`
}

type driveDeleteRequest struct {
	FileID    string `json:"file_id"`
	Trash     bool   `json:"trash,omitempty"`
}

type driveSearchRequest struct {
	Query     string `json:"query,omitempty"`
	FolderID  string `json:"folder_id,omitempty"`
	OrderBy   string `json:"order_by,omitempty"`
	Order     string `json:"order,omitempty"`
}

type driveUploadRequest struct {
	FileName  string `json:"file_name"`
	FolderID  string `json:"folder_id,omitempty"`
}

func driveDefaultFolder() string {
	folder := os.Getenv("DRIVE_DEFAULT_FOLDER")
	if folder != "" {
		return folder
	}
	return caixaPretaFolderID
}

func driveListFiles(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	folderID := r.URL.Query().Get("folder_id")
	if folderID == "" {
		folderID = driveDefaultFolder()
	}
	q := r.URL.Query().Get("q")
	onlyFolders := r.URL.Query().Get("onlyFolders") == "true"
	pageToken := r.URL.Query().Get("page_token")

	baseURL := "https://www.googleapis.com/drive/v3/files?"
	params := url.Values{}
	if q != "" {
		params.Set("q", fmt.Sprintf("%s and '%s' in parents and trashed = false", q, folderID))
	} else if onlyFolders {
		params.Set("q", fmt.Sprintf("mimeType = 'application/vnd.google-apps.folder' and '%s' in parents and trashed = false", folderID))
	} else {
		params.Set("q", fmt.Sprintf("'%s' in parents and trashed = false", folderID))
	}
	params.Set("fields", "files(id,name,mimeType,size,modifiedTime,parents),nextPageToken")
	params.Set("orderBy", "modifiedByMeTime desc")
	params.Set("pageSize", "200")
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	url := baseURL + params.Encode()

	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível", "detail": err.Error()})
		return
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Erro ao listar arquivos: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		respondJSON(w, map[string]interface{}{"error": "Erro na API do Drive: " + string(body), "status": resp.StatusCode})
		return
	}

	var result driveFileList
	if err := json.Unmarshal(body, &result); err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Erro ao parsear resposta: " + err.Error()})
		return
	}

	respondJSON(w, map[string]interface{}{
		"status":    "ok",
		"folder_id": folderID,
		"query":     q,
		"files":     result.Files,
		"next_page_token": result.NextPageToken,
		"count":     len(result.Files),
	})
}

func driveCreateFolder(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPost {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "método não permitido"})
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var req driveCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "JSON inválido: " + err.Error()})
		return
	}
	if req.Name == "" {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "nome obrigatório"})
		return
	}

	parentID := req.ParentID
	if parentID == "" {
		parentID = driveDefaultFolder()
	}

	body := map[string]interface{}{
		"name":     req.Name,
		"mimeType": "application/vnd.google-apps.folder",
		"parents":  []string{parentID},
	}
	if req.MimeType != "" {
		body["mimeType"] = req.MimeType
	}

	payload, _ := json.Marshal(body)
	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível"})
		return
	}

	reqHTTP, err := http.NewRequest("POST", "https://www.googleapis.com/drive/v3/files", bytes.NewReader(payload))
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+token)
	reqHTTP.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		respondJSON(w, map[string]interface{}{"status": "error", "http": resp.StatusCode, "detail": string(respBody)})
		return
	}

	var created driveFile
	json.Unmarshal(respBody, &created)

	log.Printf("[drive] pasta criada: %s (%s) em pai %s", created.Name, created.ID, parentID)
	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"file":   created,
		"message": fmt.Sprintf("Pasta '%s' criada com sucesso", req.Name),
	})
}

func driveUpload(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	if err := r.ParseMultipartForm(256 * 1024 * 1024); err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Erro ao parsear upload: " + err.Error()})
		return
	}

	folderID := r.FormValue("folder_id")
	if folderID == "" {
		folderID = driveDefaultFolder()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Nenhum arquivo enviado: " + err.Error()})
		return
	}
	defer file.Close()

	fileName := header.Filename
	if r.FormValue("file_name") != "" {
		fileName = r.FormValue("file_name")
	}

	mimeType := mime.TypeByExtension(filepath.Ext(fileName))
	if mimeType != "" {
		if idx := strings.IndexByte(mimeType, ';'); idx >= 0 {
			mimeType = strings.TrimSpace(mimeType[:idx])
		}
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	var metadata map[string]interface{}
	metadata = map[string]interface{}{
		"name": fileName,
		"parents": []string{folderID},
		"mimeType": mimeType,
	}

	metadataJSON, _ := json.Marshal(metadata)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	metadataHeaders := make(map[string][]string)
	metadataHeaders["Content-Type"] = []string{"application/json; charset=UTF-8"}
	part1, _ := writer.CreatePart(metadataHeaders)
	part1.Write(metadataJSON)

	fileHeader := map[string][]string{}
	fileHeader["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName)}
	fileHeader["Content-Type"] = []string{mimeType}
	part2, _ := writer.CreatePart(fileHeader)
	io.Copy(part2, file)
	writer.Close()

	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível"})
		return
	}

	uploadURL := "https://www.googleapis.com/upload/drive/v3/files?uploadType=multipart"
	reqHTTP, err := http.NewRequest("POST", uploadURL, body)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+token)
	contentType := writer.FormDataContentType()
	contentType = strings.Replace(contentType, "multipart/form-data", "multipart/related", 1)
	reqHTTP.Header.Set("Content-Type", contentType)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Erro no upload: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		respondJSON(w, map[string]interface{}{"status": "error", "http": resp.StatusCode, "detail": string(respBody)})
		return
	}

	var created driveFile
	json.Unmarshal(respBody, &created)

	log.Printf("[drive] upload concluído: %s (%s) em %s", created.Name, created.ID, folderID)
	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"file":   created,
		"message": fmt.Sprintf("Arquivo '%s' enviado com sucesso", fileName),
	})
}

func driveRename(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPost {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "método não permitido"})
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var req driveRenameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "JSON inválido: " + err.Error()})
		return
	}
	if req.FileID == "" || req.NewName == "" {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "file_id e new_name obrigatórios"})
		return
	}

	body := map[string]interface{}{"name": req.NewName}
	payload, _ := json.Marshal(body)

	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível"})
		return
	}

	reqHTTP, err := http.NewRequest("PATCH", fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s", req.FileID), bytes.NewReader(payload))
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+token)
	reqHTTP.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		respondJSON(w, map[string]interface{}{"status": "error", "http": resp.StatusCode, "detail": string(respBody)})
		return
	}

	var updated driveFile
	json.Unmarshal(respBody, &updated)

	log.Printf("[drive] renomeado: %s → %s", req.FileID, req.NewName)
	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"file":   updated,
		"message": fmt.Sprintf("Arquivo renomeado para '%s'", req.NewName),
	})
}

func driveMove(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if r.Method != http.MethodPost {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "método não permitido"})
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var req driveMoveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "JSON inválido: " + err.Error()})
		return
	}
	if req.FileID == "" || req.FolderID == "" {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "file_id e folder_id obrigatórios"})
		return
	}

	body := map[string]interface{}{
		"addParents": req.FolderID,
		"removeParents": "",
	}
	payload, _ := json.Marshal(body)

	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível"})
		return
	}

	reqHTTP, err := http.NewRequest("PATCH", fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s", req.FileID), bytes.NewReader(payload))
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+token)
	reqHTTP.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		respondJSON(w, map[string]interface{}{"status": "error", "http": resp.StatusCode, "detail": string(respBody)})
		return
	}

	var updated driveFile
	json.Unmarshal(respBody, &updated)

	log.Printf("[drive] movido: %s → %s", req.FileID, req.FolderID)
	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"file":   updated,
		"message": fmt.Sprintf("Arquivo movido para pasta %s", req.FolderID),
	})
}

func driveDelete(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	var req driveDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "JSON inválido: " + err.Error()})
		return
	}
	if req.FileID == "" {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "file_id obrigatório"})
		return
	}

	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível"})
		return
	}

	payload := map[string]interface{}{"trashed": true}
	payloadJSON, _ := json.Marshal(payload)

	reqHTTP, err := http.NewRequest("PATCH", fmt.Sprintf("https://www.googleapis.com/drive/v3/files/%s?supportsAllDrives=true", req.FileID), bytes.NewReader(payloadJSON))
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+token)
	reqHTTP.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		respondJSON(w, map[string]interface{}{"status": "error", "http": resp.StatusCode, "detail": string(respBody)})
		return
	}

	log.Printf("[drive] deletado (trash): %s", req.FileID)
	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"message": "Arquivo movido para a lixeira",
	})
}

func driveSearch(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "query (q) obrigatória"})
		return
	}

	folderID := r.URL.Query().Get("folder_id")
	if folderID == "" {
		folderID = driveDefaultFolder()
	}

	searchQuery := fmt.Sprintf("fullText contains '%s' and '%s' in parents and trashed = false", q, folderID)
	baseURL := "https://www.googleapis.com/drive/v3/files?"
	params := url.Values{}
	params.Set("q", searchQuery)
	params.Set("fields", "files(id,name,mimeType,size,modifiedTime,parents),nextPageToken")
	params.Set("pageSize", "100")
	url := baseURL + params.Encode()

	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível"})
		return
	}

	reqHTTP, err := http.NewRequest("GET", url, nil)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		respondJSON(w, map[string]interface{}{"error": "Erro na API do Drive: " + string(body), "status": resp.StatusCode})
		return
	}

	var result driveFileList
	json.Unmarshal(body, &result)

	respondJSON(w, map[string]interface{}{
		"status": "ok",
		"query":  q,
		"folder_id": folderID,
		"files":  result.Files,
		"count":  len(result.Files),
	})
}

func driveFolderInfo(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/drive/folder/")
	if path == "" || path == "/" {
		path = driveDefaultFolder()
	}

	folderID := path
	if strings.HasPrefix(path, "id:") {
		folderID = strings.TrimPrefix(path, "id:")
	}
	_ = filepath.Base(path)

	infoURL := "https://www.googleapis.com/drive/v3/files/" + folderID +
		"?fields=id,name,mimeType,parents,size,modifiedTime"

	token, err := driveAccessToken()
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": "Token do Drive indisponível"})
		return
	}

	reqHTTP, err := http.NewRequest("GET", infoURL, nil)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	reqHTTP.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(reqHTTP)
	if err != nil {
		respondJSON(w, map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		w.WriteHeader(resp.StatusCode)
		respondJSON(w, map[string]interface{}{"error": "Erro na API do Drive: " + string(body), "status": resp.StatusCode})
		return
	}

	var info driveFile
	json.Unmarshal(body, &info)

	childrenParams := url.Values{}
	childrenParams.Set("q", fmt.Sprintf("'%s' in parents and trashed = false", folderID))
	childrenParams.Set("fields", "files(id,name,mimeType,size,modifiedTime)")
	childrenParams.Set("pageSize", "200")
	childrenParams.Set("orderBy", "modifiedByMeTime desc")

	filesRespReq, _ := http.NewRequest("GET",
		"https://www.googleapis.com/drive/v3/files?"+childrenParams.Encode(),
		nil)
	filesRespReq.Header.Set("Authorization", "Bearer "+token)
	filesRespResp, _ := client.Do(filesRespReq)
	defer filesRespResp.Body.Close()

	var children driveFileList
	if filesRespResp.StatusCode == http.StatusOK {
		b, _ := io.ReadAll(filesRespResp.Body)
		json.Unmarshal(b, &children)
	}

	respondJSON(w, map[string]interface{}{
		"status":   "ok",
		"folder":   info,
		"children": children.Files,
		"child_count": len(children.Files),
	})
}
