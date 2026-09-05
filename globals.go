package main

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// === Padronizacao de modelos HOK (investigacao + implementacao) ===
// Modelo A: gratuito/zen (DeepSeek chat v3.1 via OpenRouter) — uso geral.
// Modelo B: fallback gratuito (MiniMax M3 free via OpenRouter) — fallback.
// Modelo C: substituto FREE robusto (Nemotron 3 super 120B a12b) — substitui
//           o meta-llama/llama-3.3-70b-instruct:free que a OR marcou como
//           "unavailable for free" em 04/09 (caía no pago silenciosamente).
// FIX 01/09: ModelB era "google/gemini-2.5-flash" (PAGO) — o pool em cascata
// caia nele silenciosamente quando o modelo free selecionado atingia rate
// limit (429), gerando custo sem aviso (~$0.017 confirmado hoje). Troca para
// minimax/minimax-m3:free (pricing 0/0 confirmado na API OpenRouter).
// FIX 05/09: adicionado ModelC (Nemotron 3 super free) após descobrir que
// llama-3.3-70b-instruct:free da OR foi descontinuado do tier free.
const (
	ModelA = "deepseek/deepseek-chat-v3.1"
	ModelB = "minimax/minimax-m3:free"
	ModelC = "nvidia/nemotron-3-super-120b-a12b:free"
)

// modelos compatíveis/validados para todos os motores (true=validado).
// o restante da lista (opencode models) fica compatible=null no frontend.
var validatedModels = map[string]bool{
	ModelA: true,
	ModelB: true,
	ModelC: true,
}

// === OpenCode como quarta engine ===
var (
	opencodeBinary  = os.Getenv("OPENCODE_BINARY")
	opencodeWorkdir = func() string {
		rp := os.Getenv("ROOT_PATH")
		if rp == "" {
			rp = "/root/hokma"
		}
		return rp
	}()
)

func init() {
	if opencodeBinary == "" {
		opencodeBinary = "/root/.opencode/bin/opencode"
	}
}

// opencodeTimeout limita a duracao do CLI opencode por chamada (mesmo dos outros motores).
const opencodeTimeout = 300 * time.Second

var (
	teleMu         sync.RWMutex
	cachedSkills   int
	cachedMemories int
	cachedTurns    int
	cachedRAMPerc  float64
)

var SANDBOX_PATH = func() string {
	rp := os.Getenv("ROOT_PATH")
	if rp == "" {
		rp = "/root/hokma"
	}
	return rp + "/sandbox"
}()

// Telemetria de dispositivo (Android/Termux) — inerte na VPS, sem coletor ativo
var (
	cachedBatteryPerc int
	cachedBatteryStat string
	cachedWifiSSID    string
	cachedWifiIP      string
	cachedUptime      string
	errorsFixed       int
	errorsDetected    int
)

// Rate limiter simples por IP
var (
	rateLimiterMu sync.Mutex
	rateLimiter   = map[string][]time.Time{}
)

// getRAMUsedPercent le /proc/meminfo e calcula % de RAM em uso real do host.
func getRAMUsedPercent() float64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return -1
	}
	var totalKB, availableKB int64
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseInt(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			totalKB = val
		case "MemAvailable:":
			availableKB = val
		}
	}
	if totalKB == 0 {
		return -1
	}
	used := totalKB - availableKB
	return (float64(used) / float64(totalKB)) * 100
}
