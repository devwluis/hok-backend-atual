package main

import (
	"errors"
	"log"
	"strings"
)

// errModelUnsupported: o motor não suporta o modelo ativo (ex.: Hermes não
// roda modelos do tier Zen/Go do opencode — só OpenRouter).
var errModelUnsupported = errors.New("model unsupported by engine")

// ─── TRAVA DE SEGURANÇA DO MODELO ATIVO (29/08) ─────────────────────────────
// Se o modelo ativo/selecionado sumir da lista, virar pago, ou retornar erro
// de modelo inválido/expirado (402/404/410/400 do provider), o HOK NÃO troca
// automaticamente para outro modelo (nem fallback). Regras:
//   a) a última seleção do usuário permanece registrada (nunca sobrescreve);
//   b) o chat mostra a mensagem clara ("Modelo expirou" / "Modelo indisponível"
//      / "Modelo agora é pago", conforme o caso);
//   c) o envio fica bloqueado até o usuário escolher outro modelo manualmente.
// Erros transitórios (429/5xx/timeout) NÃO disparam a trava — a cascata de
// fallback continua valendo para esses (não são "modelo expirado").

// modelGateStatus representa o estado do modelo ativo.
const (
	modelStatusOK          = "ok"
	modelStatusExpired     = "expired"   // 404/410 ou sumiu do catálogo
	modelStatusPaid        = "paid"      // 402 — virou pago
	modelStatusUnavailable = "unavailable" // 400/invalid ou não suportado pelo motor
)

// classifyModelStatus classifica a string de erro do provider.
// Retorna ("", "") para erros transitórios (sem trava).
func classifyModelStatus(errStr string) (status, msg string) {
	e := strings.ToLower(errStr)
	switch {
	case strings.Contains(e, "402"):
		return modelStatusPaid, "Modelo agora é pago — escolha outro modelo na lista de IA para continuar."
	case strings.Contains(e, "404") || strings.Contains(e, "410"):
		return modelStatusExpired, "Modelo expirou — escolha outro modelo na lista de IA para continuar."
	case strings.Contains(e, "400") || strings.Contains(e, "not a valid model") || strings.Contains(e, "invalid model"):
		return modelStatusUnavailable, "Modelo indisponível — escolha outro modelo na lista de IA para continuar."
	}
	return "", ""
}

// modelForOpenRouter devolve o ID se ele é um slug válido para os motores
// que falam direto com o OpenRouter (hermes/claude/routeModel). Modelos do
// tier Zen/Go do opencode ("opencode/*", "opencode-go/*") e IDs sem prefixo
// NÃO são aceitos pelo OpenRouter — devolve "" (o motor não suporta; a trava
// mostra a mensagem — nunca fallback silencioso).
func modelForOpenRouter(m string) string {
	s := strings.TrimSpace(m)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "opencode/") || strings.HasPrefix(s, "opencode-go/") {
		return ""
	}
	if !strings.Contains(s, "/") {
		return ""
	}
	return s
}

// modelBlockReply monta a resposta padrão da trava para um motor.
func modelBlockReply(status string) (string, string) {
	switch status {
	case modelStatusPaid:
		return "Modelo agora é pago — escolha outro modelo na lista de IA para continuar.", "model_paid"
	case modelStatusExpired:
		return "Modelo expirou — escolha outro modelo na lista de IA para continuar.", "model_expired"
	default:
		return "Modelo indisponível — escolha outro modelo na lista de IA para continuar.", "model_unavailable"
	}
}

// modelUnsupportedReply para motores que não suportam o modelo ativo
// (ex.: Hermes não roda modelos do tier Zen/Go do opencode).
func modelUnsupportedReply(m string) (string, string) {
	return "Modelo indisponível — o modelo ativo (" + m + ") não é suportado por este motor. Escolha outro modelo na lista de IA.", "model_unavailable"
}

// auditModelBlock registra a trava (nunca troca o modelo — a seleção do
// usuário fica intacta em app_settings).
func auditModelBlock(engine, model, status string) {
	log.Printf("[AUDIT] model_gate engine=%s model=%s status=%s — seleção do usuário mantida, envio bloqueado até troca manual", engine, model, status)
}

// modelBlockIfExpired — trava genérica para os motores (claude/opencode):
// erro de modelo expirado/pago/inválido → resultado de bloqueio; senão nil
// (o fluxo normal continua — pending/cascata).
func modelBlockIfExpired(err error, engine string) *smartTextResult {
	if err == nil {
		return nil
	}
	if status, _ := classifyModelStatus(err.Error()); status != "" {
		msg, mode := modelBlockReply(status)
		auditModelBlock(engine, getActiveModel(), status)
		return &smartTextResult{reply: msg, mode: mode, engine: engine}
	}
	return nil
}

// hermesModelResult — trava específica do Hermes: inclui o caso do modelo
// não suportado pelo motor (errModelUnsupported — tiers Zen/Go do opencode)
// além dos erros classificados do provider.
func hermesModelResult(err error) *smartTextResult {
	if err == nil {
		return nil
	}
	if errors.Is(err, errModelUnsupported) {
		model := getActiveModel()
		msg, mode := modelUnsupportedReply(model)
		auditModelBlock("hermes", model, modelStatusUnavailable)
		return &smartTextResult{reply: msg, mode: mode, engine: "hermes"}
	}
	return modelBlockIfExpired(err, "hermes")
}