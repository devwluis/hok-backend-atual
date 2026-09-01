package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// --- Fail-closed do OpenCode (mesmo padrao do bug critico de hoje) ---
// Se o prompt original sumiu (ArgsJSON perdido/corrompido), NUNCA executa nada.
func TestResolveOpenCodePendingActionFailClosed(t *testing.T) {
	resetPendingState()

	pa := &PendingAction{
		ID:          "oc_failclosed_test",
		ToolName:    "opencode",
		ArgsJSON:    `{"prompt":""}`, // prompt perdido
		Description: "Execucao de comando bash: rm -rf /tmp/x",
		CreatedAt:   time.Now(),
	}
	result := resolveOpenCodePendingAction(context.Background(), pa, "conv", "tenant", "user")
	if !strings.Contains(result, "nao esta mais disponivel") {
		t.Fatalf("esperava fail-closed, veio: %s", result)
	}
	if strings.Contains(result, "rm") || strings.Contains(result, "command not found") {
		t.Fatalf("fail-closed executou algo: %s", result)
	}

	// ArgsJSON invalido (nao-JSON) tambem deve ser fail-closed
	pa.ArgsJSON = "Ok perfeito agora uma duvida"
	result = resolveOpenCodePendingAction(context.Background(), pa, "conv", "tenant", "user")
	if !strings.Contains(result, "nao esta mais disponivel") {
		t.Fatalf("esperava fail-closed para args invalidos, veio: %s", result)
	}
}

// processOpenCodeJSONStream acumula eventos "text" e ignora o resto.
func TestProcessOpenCodeJSONStream(t *testing.T) {
	ndjson := "" +
		`{"type":"step_start","sessionID":"ses_x","timestamp":1}` + "\n" +
		`{"type":"text","sessionID":"ses_x","text":"Olá "}` + "\n" +
		`{"type":"text","sessionID":"ses_x","text":"mundo!"}` + "\n" +
		`{"type":"step_start","sessionID":"ses_x","timestamp":2}` + "\n"

	out, sessionID, executedDangerous, err := processOpenCodeJSONStream(strings.NewReader(ndjson))
	if err != nil {
		t.Fatalf("erro no stream: %v", err)
	}
	if out != "Olá mundo!" {
		t.Fatalf("esperava 'Olá mundo!', veio: %q", out)
	}
	if sessionID != "ses_x" {
		t.Errorf("esperava sessionID 'ses_x', veio: %q", sessionID)
	}
	if executedDangerous {
		t.Error("stream sem tool_use não deveria marcar executedDangerous")
	}
}

// TestProcessOpenCodeJSONStreamPlanDeny — TRAVA REAL do modo plan: tool negada
// pelo motor chega como "invalid" (nunca executou) → executedDangerous=false;
// tool de escrita REAL executada → executedDangerous=true (falha da trava).
func TestProcessOpenCodeJSONStreamPlanDeny(t *testing.T) {
	// Tool bash NEGADA pelo motor (OPENCODE_PERMISSION=deny): chega como
	// "invalid" — NÃO é marcada como execução perigosa.
	denied := "" +
		`{"type":"tool_use","sessionID":"ses_p","part":{"type":"tool","tool":"invalid","state":{"status":"completed","input":{"tool":"bash","error":"Model tried to call unavailable tool 'bash'"}}}}` + "\n" +
		`{"type":"text","sessionID":"ses_p","part":{"type":"text","text":"plano sem executar"}}` + "\n"
	out, _, executed, err := processOpenCodeJSONStream(strings.NewReader(denied))
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if executed {
		t.Fatalf("tool negada (invalid) NÃO deve marcar executedDangerous: %q", out)
	}

	// Tool bash REAL executada (trava falhou): tool_use com tool="bash".
	executedReal := "" +
		`{"type":"tool_use","sessionID":"ses_p","part":{"type":"tool","tool":"bash","callID":"c1","state":{"status":"completed","input":{"cmd":"touch x"}}}}` + "\n" +
		`{"type":"text","sessionID":"ses_p","part":{"type":"text","text":"criei"}}` + "\n"
	_, _, executed2, err := processOpenCodeJSONStream(strings.NewReader(executedReal))
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if !executed2 {
		t.Fatal("tool bash REAL deveria marcar executedDangerous=true")
	}

	// Tool de leitura (read/glob/grep) permitida no plan → não é perigosa.
	readOnly := "" +
		`{"type":"tool_use","sessionID":"ses_p","part":{"type":"tool","tool":"read","callID":"c1","state":{"status":"completed"}}}` + "\n"
	_, _, executed3, err := processOpenCodeJSONStream(strings.NewReader(readOnly))
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if executed3 {
		t.Fatal("tool read (somente leitura) NÃO deve marcar executedDangerous")
	}
}

// TestOpenCodePlanDenyJSON — a env OPENCODE_PERMISSION do modo plan precisa
// negar bash/edit/webfetch/etc (a trava é do motor, não do agente).
func TestOpenCodePlanDenyJSON(t *testing.T) {
	raw := opencodePlanDenyJSON()
	if !strings.Contains(raw, `"bash":"deny"`) || !strings.Contains(raw, `"edit":"deny"`) {
		t.Fatalf("deny JSON incompleto: %s", raw)
	}
	if strings.Contains(raw, `"allow"`) {
		t.Fatalf("deny JSON não pode conter allow: %s", raw)
	}
	// parseável
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("deny JSON inválido: %v", err)
	}
}

// TestOpenCodePlanNeverAuto — TRAVA REAL: plan + skipPermissions NUNCA
// combinam (fail-closed); os args do CLI em plan usam --agent plan sem --auto.
func TestOpenCodePlanNeverAuto(t *testing.T) {
	planArgs := opencodeCLIArgs("crie o arquivo x", false, "", "", true)
	joined := strings.Join(planArgs, " ")
	if !strings.Contains(joined, "--agent") || !strings.Contains(joined, "plan") {
		t.Fatalf("plan deveria conter --agent plan, veio: %v", planArgs)
	}
	if strings.Contains(joined, "--auto") {
		t.Fatalf("plan NUNCA pode ter --auto: %v", planArgs)
	}
	approved := strings.Join(opencodeCLIArgs("crie o arquivo x", true, "", "", false), " ")
	if !strings.Contains(approved, "--auto") {
		t.Fatalf("approved deveria ter --auto: %v", approved)
	}
}

// isOpenCodeTask detecta roteamento.
func TestIsOpenCodeTask(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"opencode: meu codigo", true},
		{"use opencode para revisar", true},
		{"Opencode por favor", true},
		{"revise o codigo", false},
		{"Hok?", false},
	}
	for _, c := range cases {
		if got := isOpenCodeTask(c.in); got != c.want {
			t.Errorf("isOpenCodeTask(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// describeOpenCodeAction inclui o modelo ativo na descricao.
func TestDescribeOpenCodeAction(t *testing.T) {
	activeModelMu.Lock()
	activeModel = ModelA
	activeModelMu.Unlock()
	desc := describeOpenCodeAction("analise o arquivo main.go")
	if !strings.Contains(desc, "OpenCode") {
		t.Fatalf("descricao deveria mencionar OpenCode: %s", desc)
	}
	if !strings.Contains(desc, "modelA") {
		t.Fatalf("descricao deveria incluir a tag do modelo: %s", desc)
	}
}

// classifyEngine inclui "opencode".
func TestClassifyEngineOpenCode(t *testing.T) {
	// ForceOpenCode=true sempre rotifica para opencode.
	req := ClientRequest{ForceOpenCode: true}
	if got := classifyEngine("qualquer coisa", req); got != "opencode" {
		t.Errorf("ForceOpenCode=true deveria retornar opencode, foi: %q", got)
	}
	// Mensagem contendo "opencode" tambem rotifica para opencode (mesmo sem Force).
	req = ClientRequest{}
	if got := classifyEngine("opencode me ajuda", req); got != "opencode" {
		t.Errorf("msg com 'opencode' deveria retornar opencode, foi: %q", got)
	}
	// Mensagens sem keyword vao para chat.
	if got := classifyEngine("Hok?", req); got != "chat" {
		t.Errorf("msg sem keyword deveria retornar chat, foi: %q", got)
	}
}

// activeModel / model constants.
func TestModelConstants(t *testing.T) {
	if ModelA != "deepseek/deepseek-chat-v3.1" {
		t.Fatalf("ModelA inesperado: %s", ModelA)
	}
	if ModelB != "minimax/minimax-m3:free" {
		t.Fatalf("ModelB inesperado: %s", ModelB)
	}
	// O modelo ativo eh definido via setActiveModel/getActiveModel (persistido em app_settings).
	// Antes era hardcoded como ModelA; agora e' configuravel pelo usuario via /models/select.
	activeModel := getDefaultChatModel()
	if activeModel != ModelA && activeModel != ModelB {
		t.Fatalf("getDefaultChatModel deveria ser ModelA ou ModelB, foi: %s", activeModel)
	}
	// modelA/modelB validados
	if !validatedModels[ModelA] || !validatedModels[ModelB] {
		t.Fatal("modelA e modelB devem estar validados")
	}
}

// isRecoverableOpenCodeError classifica erros de fallback.
func TestIsRecoverableOpenCodeError(t *testing.T) {
	if !isRecoverableOpenCodeError(errTimeout("mock timeout apos 120s")) {
		t.Error("timeout deve ser recuperavel")
	}
	if isRecoverableOpenCodeError(opencodeBlockedErr()) {
		t.Error("blocked nao deve ser recuperavel (seguranca)")
	}
}

type errTimeout string

func (e errTimeout) Error() string { return string(e) }
