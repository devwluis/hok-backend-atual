package main

// Testes do parser extractAnalysis (debug_n8n.go v3)
// Roda com: go test -v -run TestExtract
// Cobertura: code fences, aspas curly, numeros com lixo, trailing comma, JSON flat/nested

import (
	"encoding/json"
	"testing"
)

var testCases = []struct {
	name              string
	input             string
	wantBrokenNode    string
	wantConfidenceGE  float64
	wantJSON          string
}{
	{
		name:             "caso 1 - JSON perfeito nested",
		input:            `{"broken_node":"n1","root_cause":"timeout","fix":{"description":"aumente","diff":{"parameters.timeout":10000}},"confidence":0.95}`,
		wantBrokenNode:   "n1",
		wantConfidenceGE: 0.9,
		wantJSON:         `{"broken_node":"n1","confidence":0.95,"fix":{"description":"aumente","diff":{"parameters.timeout":10000}},"root_cause":"timeout"}`,
	},
	{
		name:             "caso 2 - JSON em code fence",
		input:            "```json\n{\"broken_node\":\"Webhook\",\"root_cause\":\"timeout\",\"fix\":{\"description\":\"aumentar\",\"diff\":{\"timeout\":15000}},\"confidence\":0.88}\n```",
		wantBrokenNode:   "Webhook",
		wantConfidenceGE: 0.8,
		wantJSON:         `{"broken_node":"Webhook","confidence":0.88,"fix":{"description":"aumentar","diff":{"timeout":15000}},"root_cause":"timeout"}`,
	},
	{
		name:             "caso 3 - numero com ponto final (BUG PRINCIPAL)",
		input:            "{\n  \"broken_node\":\"n1\",\n  \"root_cause\":\"O no excedeu tempo.\",\n  \"fix\":{\n    \"description\":\"ajustar\",\n    \"diff\":{\n      \"parameters.timeout\": 10000.\n    }\n  },\n  \"confidence\": 0.95\n}",
		wantBrokenNode:   "n1",
		wantConfidenceGE: 0.9,
		wantJSON:         `{"broken_node":"n1","confidence":0.95,"fix":{"description":"ajustar","diff":{"parameters.timeout":10000}},"root_cause":"O no excedeu tempo."}`,
	},
	{
		name:             "caso 4 - aspas curly",
		input:            "{\n\u201cbroken_node\u201c: \u201cn1\u201c,\n\u201croot_cause\u201c: \u201ctimeout\u201c,\n\u201cfix\u201c: {\u201cdescription\u201c: \u201cconsertar\u201c, \u201cdiff\u201c: {\u201ccampo\u201c: 100}},\n\u201cconfidence\u201c: 0.85\n}",
		wantBrokenNode:   "n1",
		wantConfidenceGE: 0.8,
		wantJSON:         `{"broken_node":"n1","confidence":0.85,"fix":{"description":"consertar","diff":{"campo":100}},"root_cause":"timeout"}`,
	},
	{
		name:             "caso 5 - JSON flat (sem nesting de fix)",
		input:            `{"broken_node":"HTTP Request","root_cause":"API down","fix.description":"trocar url","fix.diff":{"url":"https://api.exemplo.com"},"confidence":0.75}`,
		wantBrokenNode:   "HTTP Request",
		wantConfidenceGE: 0.7,
		wantJSON:         `{"broken_node":"HTTP Request","confidence":0.75,"fix":{"description":"trocar url","diff":{"url":"https://api.exemplo.com"}},"root_cause":"API down"}`,
	},
	{
		name:             "caso 6 - tudo errado junto (markdown + trailing comma + curly)",
		input:            "```json\n{\n  \"broken_node\": \"Schedule Trigger\",\n  \"root_cause\": \"trigger nao configurado\",\n  \"fix\": {\n    \"description\": \"ajustar cron\",\n    \"diff\": {\n      \"rule.interval\": \"5m\",\n    },\n  },\n  \"confidence\": 0.82,\n}\n```",
		wantBrokenNode:   "Schedule Trigger",
		wantConfidenceGE: 0.8,
		wantJSON:         `{"broken_node":"Schedule Trigger","confidence":0.82,"fix":{"description":"ajustar cron","diff":{"rule.interval":"5m"}},"root_cause":"trigger nao configurado"}`,
	},
}

func cmpJSON(t *testing.T, label string, got, want string) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Errorf("[%s] parse got: %v\n  input: %s", label, err, got)
		return
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Errorf("[%s] parse want: %v\n  input: %s", label, err, want)
		return
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Errorf("[%s]\n  got:  %s\n  want: %s", label, gb, wb)
	}
}

func TestExtractAnalysis(t *testing.T) {
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := extractAnalysis(tc.input)
			if err != nil {
				t.Fatalf("parse falhou: %v\ninput: %s", err, tc.input)
			}
			if a.BrokenNode != tc.wantBrokenNode {
				t.Errorf("BrokenNode got %q want %q", a.BrokenNode, tc.wantBrokenNode)
			}
			if a.Confidence < tc.wantConfidenceGE {
				t.Errorf("Confidence got %v want >= %v", a.Confidence, tc.wantConfidenceGE)
			}
			gotJSON, _ := json.Marshal(a)
			cmpJSON(t, "structure", string(gotJSON), tc.wantJSON)
		})
	}
}

func TestStripCodeFences(t *testing.T) {
	tests := map[string]string{
		"```json\n{}\n```":          "{}",
		"```\n{}\n```":              "{}",
		"```JSON\n{\"a\":1}\n```":    "{\"a\":1}",
		"  \n```json\n{}\n```\n  ": "{}",
		"{no fence}":                "{no fence}",
		"":                          "",
	}
	for in, want := range tests {
		got := stripCodeFences(in)
		if got != want {
			t.Errorf("stripCodeFences(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeQuotes(t *testing.T) {
	in := "\u201chello\u201d \u2018world\u2019"
	want := "\"hello\" 'world'"
	got := normalizeQuotes(in)
	if got != want {
		t.Errorf("normalizeQuotes = %q, want %q", got, want)
	}
}

func TestSanitizeJSON(t *testing.T) {
	tests := map[string]string{
		`{"x":10000.}`:    `{"x":10000}`,
		`{"x":10000,}`:    `{"x":10000}`,
		`{"a":{"b":1,},}`: `{"a":{"b":1}}`,
		`{"x":1.5.}`:      `{"x":1.5}`,
	}
	for in, want := range tests {
		got := sanitizeJSON(in)
		if got != want {
			t.Errorf("sanitizeJSON(%q) = %q, want %q", in, got, want)
		}
	}
}
