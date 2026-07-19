package main

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

func searchDDG(query string) string {
	if len(strings.TrimSpace(query)) < 3 {
		return ""
	}
	client := &http.Client{Timeout: 12 * time.Second}
	escaped := url.QueryEscape(query)
	urlStr := "https://html.duckduckgo.com/html/?q=" + escaped + "&kl=br-pt"

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36")
	req.Header.Set("Accept-Language", "pt-BR,pt;q=0.9")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	html := string(body)

	re := regexp.MustCompile(`class="result__snippet"[^>]*>([\s\S]*?)</a>`)
	matches := re.FindAllStringSubmatch(html, 5)
	if len(matches) == 0 {
		return ""
	}

	tags := regexp.MustCompile(`<[^>]+>`)
	var snippets []string
	for _, m := range matches {
		s := strings.TrimSpace(tags.ReplaceAllString(m[1], ""))
		if len(s) > 20 {
			snippets = append(snippets, "• "+s)
		}
	}
	if len(snippets) == 0 {
		return ""
	}
	return "🔍 Resultados da busca web:\n" + strings.Join(snippets, "\n")
}
