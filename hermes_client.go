package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

func isComplexTask(msg string) bool {
	keywords := []string{
		"crie uma skill", "criar skill", "nova skill",
		"crie um script", "cria um script", "faca um script",
		"pesquise na web", "busque na internet",
		"procure na web", "ultimas noticias",
		"automatize", "automatizar", "cron", "agende",
		"monitore", "monitorar", "instale", "deploy",
		"rebuild", "redeploy", "analise o log",
		"diagnostique", "por que esta falhando",
	}
	lower := strings.ToLower(msg)
	for _, k := range keywords {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// HermesModelA/B vindos de env (default: ModelA/DeepSeek, ModelB/Gemini)
var (
	hermesModelA = os.Getenv("HERMES_MODEL_A")
	hermesModelB = os.Getenv("HERMES_MODEL_B")
)

func init() {
	if hermesModelA == "" {
		hermesModelA = ModelA
	}
	if hermesModelB == "" {
		hermesModelB = ModelB
	}
}

func callHermes(prompt string) (string, error) {
	// modelo ativo global (selecionado via /models/select no frontend);
	// fallback automatico para hermesModelB quando o ativo falha.
	// O slug do catalogo (ex: deepseek-v4-flash-free) e' normalizado antes de
	// virar argumento -m: o sufixo -free e' metadado do catalogo, e o provider
	// openrouter nao aceita esse id (espera "deepseek-v4-flash").
	model := getActiveModel()
	out, err := callHermesWith(normalizeModelSlugForAPI(model), prompt)
	if err == nil {
		return out, nil
	}
	logModelIncompatibility("hermes", model, err)
	log.Printf("⚠️ Hermes modelA falhou (%v) — reexecutando com modelB", err)
	// fallback automatico para modelB
	return callHermesWith(hermesModelB, prompt)
}

// callHermesWith roda a imagem do hermes-gateway dockerizada com o modelo dado.
func callHermesWith(model string, prompt string) (string, error) {
	return callHermesWithMode(model, prompt, false)
}

// callHermesWithMode — GATE PLAN (28/08): em planMode NÃO passa --yolo (o
// hermes não auto-aprova ferramentas); os args são montados em função
// separada para teste.
func callHermesArgs(model string, prompt string, yolo bool) []string {
	args := []string{"exec", "hermes-gateway", "hermes", "-z", prompt, "-m", model, "--provider", "openrouter"}
	if yolo {
		args = append(args, "--yolo")
	}
	return args
}

func callHermesWithMode(model string, prompt string, planMode bool) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	var args []string
	if planMode {
		// GATE PLAN REFORÇADO (28/08): isolamento FÍSICO via docker run
		// efêmero — rootfs read-only, volume /opt/data montado read-only
		// (config/auth para leitura), e /opt/data de trabalho em tmpfs
		// DESCARTÁVEL. Mesmo que o hermes consiga "escrever", nada persiste
		// no host nem em volume algum (--rm remove o container e o tmpfs).
		vol, vErr := hermesDataVolume()
		if vErr != nil {
			return "", vErr
		}
		args = hermesIsolatedArgs(model, prompt, vol)
	} else {
		args = append([]string{}, callHermesArgs(model, prompt, !planMode)...)
	}
	cmd := exec.CommandContext(ctx, "/usr/bin/docker", args...)
	out, err := cmd.Output()
	if err != nil {
		sqliteExec("INSERT INTO logs (event, level, source) VALUES ('hermes_invoke:minimax-m3 fail', 'WARN', 'hermes_client');")
		return "", err
	}
	sqliteExec("INSERT INTO logs (event, level, source) VALUES ('hermes_invoke:minimax-m3 ok', 'INFO', 'hermes_client');")
	return strings.TrimSpace(string(out)), nil
}

func buildHermesPrompt(msg string, req ClientRequest) string {
	var sb strings.Builder
	sb.WriteString("Você é Hermes, agente de automação do HOK OS, parceiro técnico do Hokmá (Washington Luis).\n")
	sb.WriteString("Responda sempre em português brasileiro.\n\n")
	if len(req.History) > 0 {
		sb.WriteString("Histórico recente:\n")
		limit := len(req.History)
		if limit > 6 {
			limit = 6
		}
		for _, h := range req.History[len(req.History)-limit:] {
			sb.WriteString(h.Role + ": " + h.Content + "\n")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Usuário: " + msg)
	return sb.String()
}

// hermesVerifyOutput — verificação pós-execução (28/08): o hermes pode
// afirmar que criou/alterou arquivos sem ter feito nada (alucinação). Extrai
// caminhos citados no output (entre crases ou após "criado/criar/escrito/
// modificado") e confere no disco; quando a alegação de criação não bate,
// anexa um aviso ao reply. Heurística — não substitui a checagem real do
// agente, mas pega os casos óbvios de alucinação.
func hermesVerifyOutput(out string) string {
	if strings.TrimSpace(out) == "" {
		return out
	}
	claimsCreation := strings.Contains(strings.ToLower(out), "criad") ||
		strings.Contains(strings.ToLower(out), "escrito") ||
		strings.Contains(strings.ToLower(out), "modificad")
	if !claimsCreation {
		return out
	}
	missing := []string{}
	// caminhos citados entre crases
	for _, m := range backtickPathRe.FindAllString(out, -1) {
		p := strings.Trim(m, "`")
		if !strings.HasPrefix(p, "/") {
			continue
		}
		if strings.HasSuffix(p, "/") {
			if fi, err := os.Stat(p); err != nil || !fi.IsDir() {
				missing = append(missing, p)
			}
		} else if _, err := os.Stat(p); err != nil {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		return out + "\n\n⚠️ [verificação pós-execução] o Hermes afirmou ter criado/alterado, mas estes caminhos NÃO existem no disco: " +
			strings.Join(missing, ", ") + " — possível alucinação de confirmação."
	}
	return out
}

var backtickPathRe = regexp.MustCompile("`[^`]+`")

// hermesDataVolume — resolve o volume do /opt/data do hermes-gateway (onde
// ficam config/auth do hermes). Cacheado após a primeira resolução.
func hermesDataVolume() (string, error) {
	hermesVolMu.Lock()
	defer hermesVolMu.Unlock()
	if hermesVolCache != "" {
		return hermesVolCache, nil
	}
	out, err := exec.Command("/usr/bin/docker", "inspect", "hermes-gateway", "--format", "{{json .Mounts}}").Output()
	if err != nil {
		return "", fmt.Errorf("hermes: docker inspect: %v", err)
	}
	var mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
	}
	if err := json.Unmarshal(out, &mounts); err != nil {
		return "", fmt.Errorf("hermes: parse mounts: %v", err)
	}
	for _, m := range mounts {
		if m.Destination == "/opt/data" && m.Type == "volume" {
			// --mount type=volume espera o NOME do volume (não o path
			// Source no host) — o campo Name vem no inspect.
			if m.Name != "" {
				hermesVolCache = m.Name
				return m.Name, nil
			}
			// fallback: extrai o nome do path /var/lib/docker/volumes/<name>/_data
			parts := strings.Split(m.Source, "/")
			if len(parts) >= 2 && parts[len(parts)-1] == "_data" {
				hermesVolCache = parts[len(parts)-2]
				return hermesVolCache, nil
			}
			return "", fmt.Errorf("hermes: volume sem nome resolvivel: %s", m.Source)
		}
	}
	return "", fmt.Errorf("hermes: volume /opt/data nao encontrado no hermes-gateway")
}

var (
	hermesVolMu    sync.Mutex
	hermesVolCache string
)

// hermesIsolatedArgs — GATE PLAN reforçado: docker run efêmero com isolamento
// físico. O prompt vai em base64 (seguro para o shell -c). O hermes lê o
// config/auth do volume (read-only), trabalha em /opt/data tmpfs descartável
// e o container é removido no final (--rm): fisicamente impossível persistir
// qualquer escrita no host.
func hermesIsolatedArgs(model string, prompt string, vol string) []string {
	promptB64 := base64.StdEncoding.EncodeToString([]byte(prompt))
	inner := fmt.Sprintf("echo %s | base64 -d > /tmp/p.txt; cp -a /opt/data-ro/. /opt/data/ 2>/dev/null; /opt/hermes/bin/hermes -z \"$(cat /tmp/p.txt)\" -m %s --provider openrouter",
		promptB64, model)
	return []string{
		"run", "--rm",
		"--read-only",
		"--tmpfs", "/tmp",
		"--tmpfs", "/run",
		"--mount", "type=volume,src=" + vol + ",dst=/opt/data-ro,readonly",
		"--tmpfs", "/opt/data",
		"--entrypoint", "/bin/sh",
		"nousresearch/hermes-agent",
		"-c", inner,
	}
}
