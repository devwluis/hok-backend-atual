package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	mcp "github.com/ktr0731/go-mcp"
	"github.com/ktr0731/go-mcp/protocol"
	"golang.org/x/exp/jsonrpc2"
)

var mcpTools = []protocol.Tool{
	{
		Name:        "hok_health",
		Description: "Verifica saúde do backend Hokma (/health) + OpenCode serve (/opencode/serve/status)",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	},
	{
		Name:        "hok_project_state",
		Description: "Retorna o conteúdo completo de HOK_STATE.md via /fs/read",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	},
	{
		Name:        "hok_recent_adendos",
		Description: "Lista os adendos mais recentes em backend/contexto-hokma/ via /fs/list",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"limit":{"type":"number","description":"Máximo de adendos a listar (default 10)"}},"additionalProperties":false}`),
	},
	{
		Name:        "hok_search_context",
		Description: "Busca texto em HOK_STATE.md e adendos recentes em contexto-hokma/",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Texto a buscar"}},"required":["query"],"additionalProperties":false}`),
	},
	{
		Name:        "hok_drive_files",
		Description: "Lista arquivos do Google Drive vinculados ao Hokma via /drive/files",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	},
	{
		Name:        "hok_session_info",
		Description: "Retorna informações da sessão OpenCode serve via /opencode/serve/status",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
	},
}

func hokHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func hokGET(path string) ([]byte, error) {
	token := os.Getenv("MCP_TOKEN")
	req, err := http.NewRequest("GET", "http://localhost:8082"+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Hok-Token", token)
	resp, err := hokHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro na requisição %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return body, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

func hokPOST(path string, body any) ([]byte, error) {
	token := os.Getenv("MCP_TOKEN")
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "http://localhost:8082"+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Hok-Token", token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := hokHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("erro na requisição %s: %w", path, err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return bodyBytes, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return bodyBytes, nil
}

func toolHokHealth(ctx context.Context, params protocol.CallToolRequestParams) (any, error) {
	healthBody, err := hokGET("/health")
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("backend /health erro: %v", err)}}}, nil
	}
	serveBody, err := hokGET("/opencode/serve/status")
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("opencode /opencode/serve/status erro: %v", err)}}}, nil
	}
	var health, serve map[string]any
	_ = json.Unmarshal(healthBody, &health)
	_ = json.Unmarshal(serveBody, &serve)
	result := map[string]any{
		"backend":       health,
		"opencode_serve": serve,
	}
	text, _ := json.MarshalIndent(result, "", "  ")
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}, nil
}

func toolHokProjectState(ctx context.Context, params protocol.CallToolRequestParams) (any, error) {
	body, err := hokPOST("/fs/read", map[string]string{"path": "/root/hokma/HOK_STATE.md"})
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("erro ao ler HOK_STATE.md: %v", err)}}}, nil
	}
	var fsResp FSResponse
	_ = json.Unmarshal(body, &fsResp)
	if fsResp.Status == "error" {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("FS error: %s", fsResp.Error)}}}, nil
	}
	data, _ := fsResp.Data.(map[string]any)
	content, _ := data["content"].(string)
	size, _ := data["size"].(int64)
	modified, _ := data["modified"].(string)
	result := map[string]any{
		"path":     data["path"],
		"size":     size,
		"modified": modified,
		"content":  content,
	}
	text, _ := json.MarshalIndent(result, "", "  ")
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}, nil
}

func parseArgs(params protocol.CallToolRequestParams) map[string]any {
	args := map[string]any{}
	if len(params.Arguments) > 0 {
		_ = json.Unmarshal(params.Arguments, &args)
	}
	return args
}

func toolHokRecentAdendos(ctx context.Context, params protocol.CallToolRequestParams) (any, error) {
	limit := 10
	args := parseArgs(params)
	if l, ok := args["limit"]; ok {
		switch v := l.(type) {
		case float64:
			limit = int(v)
		case json.Number:
			fmt.Sscanf(v.String(), "%d", &limit)
		}
	}
	body, err := hokPOST("/fs/list", map[string]string{"path": "/root/hokma/backend/contexto-hokma"})
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("erro ao listar adendos: %v", err)}}}, nil
	}
	var fsResp FSResponse
	_ = json.Unmarshal(body, &fsResp)
	if fsResp.Status == "error" {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("FS error: %s", fsResp.Error)}}}, nil
	}
	files, _ := fsResp.Data.([]any)
	sort.SliceStable(files, func(i, j int) bool {
		im, _ := files[i].(map[string]any)["modified"].(string)
		jm, _ := files[j].(map[string]any)["modified"].(string)
		return im > jm
	})
	if limit > len(files) {
		limit = len(files)
	}
	if limit < 0 {
		limit = 0
	}
	result := map[string]any{
		"total":      len(files),
		"showing":    limit,
		"directory":  "/root/hokma/backend/contexto-hokma",
		"adendos":    files[:limit],
	}
	text, _ := json.MarshalIndent(result, "", "  ")
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}, nil
}

func toolHokSearchContext(ctx context.Context, params protocol.CallToolRequestParams) (any, error) {
	query := ""
	args := parseArgs(params)
	if q, ok := args["query"]; ok {
		query, _ = q.(string)
	}
	if query == "" {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": "erro: parametro 'query' é obrigatório"}}}, nil
	}
	queryLower := strings.ToLower(query)

	stateBody, err := hokPOST("/fs/read", map[string]string{"path": "/root/hokma/HOK_STATE.md"})
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("erro ao ler HOK_STATE.md: %v", err)}}}, nil
	}
	var stateResp FSResponse
	_ = json.Unmarshal(stateBody, &stateResp)

	listBody, err := hokPOST("/fs/list", map[string]string{"path": "/root/hokma/backend/contexto-hokma"})
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("erro ao listar contexto-hokma/: %v", err)}}}, nil
	}
	var listResp FSResponse
	_ = json.Unmarshal(listBody, &listResp)

	files, _ := listResp.Data.([]any)
		sort.SliceStable(files, func(i, j int) bool {
		im, _ := files[i].(map[string]any)["modified"].(string)
		jm, _ := files[j].(map[string]any)["modified"].(string)
		return im > jm
	})

	results := map[string]any{
		"query": query,
		"hok_state_hits": searchInContent("HOK_STATE.md", stateResp, queryLower),
		"adendo_hits": []any{},
	}

	maxRead := 5
	if len(files) < maxRead {
		maxRead = len(files)
	}
	for i := 0; i < maxRead; i++ {
		f := files[i].(map[string]any)
		fname, _ := f["name"].(string)
		if !strings.HasSuffix(fname, ".md") || fname == "CONTEXTO_TERMINAL" || strings.Contains(fname, "CONTEXTO_") {
			continue
		}
		fbody, err := hokPOST("/fs/read", map[string]string{"path": "/root/hokma/backend/contexto-hokma/" + fname})
		if err != nil {
			continue
		}
		var fresp FSResponse
		_ = json.Unmarshal(fbody, &fresp)
		hits := searchInContent(fname, fresp, queryLower)
		if len(hits) > 0 {
			results["adendo_hits"] = append(results["adendo_hits"].([]any), hits...)
		}
	}

	totalHits := len(results["hok_state_hits"].([]any)) + len(results["adendo_hits"].([]any))
	result := map[string]any{
		"query":            query,
		"total_hits":       totalHits,
		"hok_state_hits":   results["hok_state_hits"],
		"adendo_hits":      results["adendo_hits"],
	}
	text, _ := json.MarshalIndent(result, "", "  ")
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(text)}}}, nil
}

func searchInContent(fileName string, resp FSResponse, queryLower string) []any {
	var hits []any
	if resp.Status != "ok" {
		return hits
	}
	data, _ := resp.Data.(map[string]any)
	content, _ := data["content"].(string)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), queryLower) {
			hits = append(hits, map[string]any{
				"file":    fileName,
				"line":    i + 1,
				"content": strings.TrimSpace(line),
			})
		}
	}
	return hits
}

func toolHokDriveFiles(ctx context.Context, params protocol.CallToolRequestParams) (any, error) {
	body, err := hokGET("/drive/files")
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("erro ao listar arquivos Drive: %v", err)}}}, nil
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(body)}}}, nil
}

func toolHokSessionInfo(ctx context.Context, params protocol.CallToolRequestParams) (any, error) {
	body, err := hokGET("/opencode/serve/status")
	if err != nil {
		return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("erro ao obter sessão: %v", err)}}}, nil
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(body)}}}, nil
}

func startMCP() {
		tools := make([]protocol.Tool, 0, len(mcpTools))
		for _, t := range mcpTools {
			tools = append(tools, t)
		}

		handler := &mcp.Handler{
			Tools: tools,
			ToolHandler: protocol.ServerHandlerFunc[protocol.CallToolRequestParams](func(ctx context.Context, method string, req protocol.CallToolRequestParams) (any, error) {
				switch req.Name {
				case "hok_health":
					return toolHokHealth(ctx, req)
				case "hok_project_state":
					return toolHokProjectState(ctx, req)
				case "hok_recent_adendos":
					return toolHokRecentAdendos(ctx, req)
				case "hok_search_context":
					return toolHokSearchContext(ctx, req)
				case "hok_drive_files":
					return toolHokDriveFiles(ctx, req)
				case "hok_session_info":
					return toolHokSessionInfo(ctx, req)
				default:
					return map[string]any{"content": []map[string]any{{"type": "text", "text": fmt.Sprintf("Tool '%s' não encontrada", req.Name)}}}, nil
				}
			}),
			Capabilities: protocol.ServerCapabilities{
				Tools: &protocol.ToolCapability{ListChanged: false},
			},
			Implementation: protocol.Implementation{
				Name:    "hokma-mcp",
				Version: "v1.0.0",
			},
		}

		log.Println("MCP server starting (stdio)...")
		ctx, listener, binder := mcp.NewStdioTransport(context.Background(), handler, nil)
		srv, err := jsonrpc2.Serve(ctx, listener, binder)
		if err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	log.Println("MCP server running on stdio (6 tools registered)")
	srv.Wait()
}
