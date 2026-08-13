package main

import "testing"

func TestParseMCPWorkflowVerdict(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantValid bool
		wantOK    bool
	}{
		{
			name: "veredito invalido com errors (structuredContent)",
			raw: `{"content":[{"type":"text","text":"{\"valid\": false, \"summary\": {\"errorCount\": 2}}"}],
				"structuredContent":{"valid":false,"summary":{"errorCount":2,"warningCount":1},
				"errors":[{"node":"HTTP","message":"missing url"}],
				"warnings":[{"node":"Code","message":"uses $env"}]}}`,
			wantValid: false,
			wantOK:    true,
		},
		{
			name:      "veredito valido (structuredContent)",
			raw:       `{"content":[],"structuredContent":{"valid":true,"summary":{"errorCount":0,"warningCount":0},"errors":[],"warnings":[]}}`,
			wantValid: true,
			wantOK:    true,
		},
		{
			name: "veredito so no content[0].text",
			raw:  `{"content":[{"type":"text","text":"{\"valid\": true, \"summary\": {\"errorCount\": 0, \"warningCount\": 1}, \"warnings\": [{\"message\":\"no error handling\"}]}"}]}`,
			wantValid: true,
			wantOK:    true,
		},
		{
			name:      "resposta sem veredito",
			raw:       `{"content":[{"type":"text","text":"servidor ocupado"}]}`,
			wantValid: false,
			wantOK:    false,
		},
		{
			name:      "json invalido",
			raw:       `nao-e-json`,
			wantValid: false,
			wantOK:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid, report, ok := parseMCPWorkflowVerdict(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v, esperado %v (report=%s)", ok, tc.wantOK, report)
			}
			if ok && valid != tc.wantValid {
				t.Fatalf("valid=%v, esperado %v (report=%s)", valid, tc.wantValid, report)
			}
			if tc.wantOK && report == "" {
				t.Fatalf("report vazio com ok=true")
			}
			t.Logf("report: %s", report)
		})
	}
}
