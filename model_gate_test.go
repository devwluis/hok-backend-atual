package main

import (
	"errors"
	"strings"
	"testing"
)

// TestClassifyModelStatus — trava: 402 → pago; 404/410 → expirado;
// 400/invalid → indisponível; 429/rate-limit → rate_limited (FIX 02/09);
// 5xx/timeout → sem trava.
func TestClassifyModelStatus(t *testing.T) {
	cases := []struct {
		err    string
		status string
		want   string
	}{
		{"HTTP 402: Payment Required", modelStatusPaid, "Modelo agora é pago"},
		{"provider returned 402", modelStatusPaid, "Modelo agora é pago"},
		{"HTTP 404: model not found", modelStatusExpired, "Modelo expirou"},
		{"status 410 Gone", modelStatusExpired, "Modelo expirou"},
		{"HTTP 400: mimo-v2.5 is not a valid model ID", modelStatusUnavailable, "Modelo indisponível"},
		{"invalid model id", modelStatusUnavailable, "Modelo indisponível"},
		{"HTTP 429: rate limit", modelStatusRateLimited, "rate-limit"},
		{"rate-limit upstream", modelStatusRateLimited, "rate-limit"},
		{"Provider returned error: too many requests", modelStatusRateLimited, "rate-limit"},
		{"HTTP 500: internal", "", ""},   // transitório — sem trava
		{"timeout after 90s", "", ""},    // transitório — sem trava
	}
	for _, c := range cases {
		status, msg := classifyModelStatus(c.err)
		if status != c.status {
			t.Fatalf("erro %q: status esperado %q, veio %q", c.err, c.status, status)
		}
		if c.want != "" && !strings.Contains(msg, c.want) {
			t.Fatalf("erro %q: mensagem deveria conter %q, veio %q", c.err, c.want, msg)
		}
	}
}

// TestClassifyPermanentModelStatus — rate-limit é transitório: classificadores
// de bloqueio permanente devolvem "" para 429 (pool em cascata segue).
func TestClassifyPermanentModelStatus(t *testing.T) {
	if status, _ := classifyPermanentModelStatus("HTTP 429: rate limit"); status != "" {
		t.Fatalf("429 deveria ser transitório (sem trava), veio %q", status)
	}
	if status, _ := classifyPermanentModelStatus("HTTP 402: Payment Required"); status != modelStatusPaid {
		t.Fatalf("402 deveria travar como pago, veio %q", status)
	}
}

// TestModelForOpenRouter — só slugs OpenRouter válidos (com "/" e fora dos
// tiers do opencode) passam para os motores OpenRouter direto.
func TestModelForOpenRouter(t *testing.T) {
	ok := []string{
		"deepseek/deepseek-v4-flash-0731",
		"google/gemini-2.5-flash",
		"xiaomi/mimo-v2.5",
	}
	for _, m := range ok {
		if got := modelForOpenRouter(m); got != m {
			t.Fatalf("%q deveria ser aceito, veio %q", m, got)
		}
	}
	bad := []string{
		"mimo-v2.5-free",             // Zen sem prefixo
		"opencode/mimo-v2.5-free",    // tier Zen do opencode
		"opencode-go/deepseek-v4",    // tier Go do opencode
		"deepseek-v4-flash-free",     // sem "/"
		"",
	}
	for _, m := range bad {
		if got := modelForOpenRouter(m); got != "" {
			t.Fatalf("%q NÃO deveria ser aceito pelo OpenRouter, veio %q", m, got)
		}
	}
}

// TestModelBlockReply — mensagens da trava.
func TestModelBlockReply(t *testing.T) {
	msg, mode := modelBlockReply(modelStatusPaid)
	if !strings.Contains(msg, "Modelo agora é pago") || mode != "model_paid" {
		t.Fatalf("paid: msg=%q mode=%q", msg, mode)
	}
	msg, mode = modelBlockReply(modelStatusExpired)
	if !strings.Contains(msg, "Modelo expirou") || mode != "model_expired" {
		t.Fatalf("expired: msg=%q mode=%q", msg, mode)
	}
	msg, mode = modelBlockReply(modelStatusUnavailable)
	if !strings.Contains(msg, "Modelo indisponível") || mode != "model_unavailable" {
		t.Fatalf("unavailable: msg=%q mode=%q", msg, mode)
	}
	msg, mode = modelBlockReply(modelStatusRateLimited)
	if !strings.Contains(msg, "rate-limit") || mode != "model_rate_limited" {
		t.Fatalf("rate_limited: msg=%q mode=%q", msg, mode)
	}
}

// TestHermesModelResult — modelo não suportado (Zen/Go) → bloqueio com a
// mensagem; erro genérico → nil (cascata).
func TestHermesModelResult(t *testing.T) {
	r := hermesModelResult(errModelUnsupported)
	if r == nil || !strings.Contains(r.reply, "Modelo indisponível") {
		t.Fatalf("unsupported deveria bloquear: %+v", r)
	}
	r = hermesModelResult(errors.New("exit status 1: network timeout"))
	if r != nil {
		t.Fatalf("erro genérico não deveria bloquear: %+v", r)
	}
	r = hermesModelResult(errors.New("HTTP 402: Payment Required"))
	if r == nil || !strings.Contains(r.reply, "Modelo agora é pago") {
		t.Fatalf("402 deveria bloquear com pago: %+v", r)
	}
}

// TestModelBlockIfExpired — claude/opencode: erro classificado bloqueia
// (sem pending/fallback); genérico segue o fluxo.
func TestModelBlockIfExpired(t *testing.T) {
	r := modelBlockIfExpired(errors.New("HTTP 404: model not found"), "claude_code")
	if r == nil || r.mode != "model_expired" {
		t.Fatalf("404 deveria bloquear: %+v", r)
	}
	if r := modelBlockIfExpired(errors.New("boom"), "claude_code"); r != nil {
		t.Fatalf("erro genérico não deveria bloquear: %+v", r)
	}
}