package main

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDebugLeakCapture(t *testing.T) {
	prompt := "liste os arquivos do projeto"
	if p := os.Getenv("DEBUG_PROMPT"); p != "" {
		prompt = p
	}
	skip := os.Getenv("DEBUG_SKIP") == "1"
	t.Logf("PROMPT: %q (skipPermissions=%v)", prompt, skip)
	args := claudeCLIArgs(prompt, skip)
	var cmd *exec.Cmd
	if skip {
		cmd = exec.Command("runuser", append([]string{"-u", "hokma-agent", "--", "claude"}, args...)...)
	} else {
		cmd = exec.Command("claude", args...)
	}
	cmd.Env = append(os.Environ(), "ANTHROPIC_MODEL="+normalizeModelSlugForAPI(getActiveModel()))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out strings.Builder
	chunkIdx := 0
	leakedAt := -1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.Contains(line, `"type":"assistant"`) {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Type != "assistant" {
			continue
		}
		for _, c := range ev.Message.Content {
			if c.Type == "text" && c.Text != "" {
				chunkIdx++
				out.WriteString(c.Text)
				if detectSystemPromptLeak(out.String()) && leakedAt < 0 {
					leakedAt = chunkIdx
					t.Logf(">>> LEAK DETECTADO apos chunk #%d", chunkIdx)
				}
			}
		}
	}
	_ = cmd.Wait()
	final := out.String()
	t.Logf("stderr: %s", strings.TrimSpace(stderr.String()))
	t.Logf("=== TEXTO ACUMULADO FINAL (%d chars) ===", len(final))
	t.Logf("%s", final)
	lower := strings.ToLower(final)
	report := func(name string, list []string) {
		var hits []string
		for _, s := range list {
			if strings.Contains(lower, s) {
				hits = append(hits, s)
			}
		}
		if len(hits) > 0 {
			t.Logf("--- %s (%d hits): %v", name, len(hits), hits)
		}
	}
	report("STRONG", systemPromptLeakStrong)
	report("STRONG2", systemPromptLeakStrong2)
	report("SIGNALS", systemPromptLeakSignals)
	report("NARRATION", internalNarrationSignals)
	words := 0
	var wordHits []string
	for _, w := range agentNarrationWords {
		if strings.Contains(lower, w) {
			words++
			wordHits = append(wordHits, w)
		}
	}
	t.Logf("--- agentNarrationWords: %d hits: %v", words, wordHits)
	skillLines := 0
	for _, line := range strings.Split(lower, "\n") {
		line = strings.TrimSpace(line)
		if len(line) > 4 && strings.HasPrefix(line, "- ") {
			rest := line[2:]
			idx := strings.Index(rest, ":")
			if idx > 0 && idx < 40 && !strings.Contains(rest[:idx], "*") {
				hasSlash := strings.Contains(rest[:idx], "/")
				if hasSlash {
					t.Logf("--- skillLine com '/' : %q", line)
				}
				if !strings.Contains(rest[:idx], " ") {
					skillLines++
					t.Logf("--- skillLine sem espaco: %q", line)
				}
			}
		}
	}
	t.Logf("--- skillLines: %d", skillLines)
	t.Logf("=== RESULTADO detectSystemPromptLeak: %v (leakedAt=%d) ===", detectSystemPromptLeak(final), leakedAt)
}