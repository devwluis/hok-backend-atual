package main

import "testing"

func TestNormalizeModelSlugForAPI(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"deepseek-v4-flash-free", "deepseek-v4-flash"},   // Zen free → id real aceito pelo OpenRouter
		{"deepseek-v4-flash", "deepseek-v4-flash"},        // Zen puro: intacto
		{"DEEPSEEK-V4-FLASH-FREE", "DEEPSEEK-V4-FLASH"},   // case-insensitive no sufixo
		{"deepseek/deepseek-chat-v3.1", "deepseek/deepseek-chat-v3.1"},   // OR com "/": intacto
		{"google/gemini-2.5-flash", "google/gemini-2.5-flash"},           // OR com "/": intacto
		{"meta-llama/llama-3.3-70b-instruct:free", "meta-llama/llama-3.3-70b-instruct:free"}, // ":free" OR real: intacto
		{"claude-opus-5", "claude-opus-5"},               // Zen sem sufixo: intacto
		{"", ""},                                          // vazio
		{"   ", ""},                                       // so espacos
	}
	for _, c := range cases {
		got := normalizeModelSlugForAPI(c.in)
		if got != c.want {
			t.Errorf("normalizeModelSlugForAPI(%q) = %q; want %q", c.in, got, c.want)
		}
	}
}