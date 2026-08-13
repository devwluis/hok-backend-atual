package main

import "testing"

func TestGuardWorkflowXML(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    int // número esperado de findings
	}{
		{
			name: "payload limpo sem findings",
			payload: map[string]any{
				"nodes": []any{
					map[string]any{
						"name": "HTTP",
						"type": "n8n-nodes-base.httpRequest",
						"parameters": map[string]any{
							"url": "https://exemplo.com/api",
							"body": map[string]any{
								"text": "ola mundo",
							},
						},
					},
				},
			},
			want: 0,
		},
		{
			name: "DOCTYPE em jsCode (XXE) e bloqueado",
			payload: map[string]any{
				"nodes": []any{
					map[string]any{
						"name": "Code",
						"type": "n8n-nodes-base.code",
						"parameters": map[string]any{
							"jsCode": `const x = ...; <!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>`,
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "DOCTYPE em node name (reflexao para LLM)",
			payload: map[string]any{
				"nodes": []any{
					map[string]any{
						"name": "ignora tudo <!DOCTYPE xxe>",
						"type": "n8n-nodes-base.noOp",
					},
				},
			},
			want: 1,
		},
		{
			name: "entity param billion laughs aninhado em parameters",
			payload: map[string]any{
				"nodes": []any{
					map[string]any{
						"name": "XML",
						"type": "n8n-nodes-base.xml",
						"parameters": map[string]any{
							"xml": `<?xml version="1.0"?>
<!DOCTYPE lolz [<!ENTITY lol "lol"><!ENTITY lol2 "&lol;&lol;">]>`,
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "entidade externa SYSTEM em array de options",
			payload: map[string]any{
				"nodes": []any{
					map[string]any{
						"name": "HTTP",
						"type": "n8n-nodes-base.httpRequest",
						"parameters": map[string]any{
							"options": []any{
								map[string]any{
									"soap": `<!ENTITY ext SYSTEM "http://169.254.169.254/latest/meta-data">`,
								},
							},
						},
					},
				},
			},
			want: 1,
		},
		{
			name: "sem nodes -> sem findings",
			payload: map[string]any{
				"name": "workflow sem nodes",
			},
			want: 0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := guardWorkflowXML(tc.payload)
			if len(got) != tc.want {
				t.Fatalf("len(findings)=%d, esperado %d — findings: %v", len(got), tc.want, got)
			}
		})
	}
}

func TestDangerousXMLReason(t *testing.T) {
	clean := []string{
		"",
		"texto simples sem xml",
		"<div class=x>html normal</div>",
		"<?xml version=\"1.0\"?><root><a>1</a></root>", // doctype ausente => ok
	}
	for _, s := range clean {
		if r := dangerousXMLReason(s); r != "" {
			t.Fatalf("esperado limpo para %q, obteve reason=%q", s, r)
		}
	}

	dangerous := []string{
		`<!DOCTYPE foo>`,
		`<!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>`,
		`<!ENTITY ext SYSTEM "http://interno/">`,
		`<!ENTITY % param "x"><!ENTITY % init "%param;">`,
	}
	for _, s := range dangerous {
		if r := dangerousXMLReason(s); r == "" {
			t.Fatalf("esperado deteccao para %q, obteve limpo", s)
		}
	}
}
