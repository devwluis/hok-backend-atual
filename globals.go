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
// Modelo B: fallback pago/estavel (Gemini 2.5 Flash via OpenRouter) — fallback.
const (
	ModelA = "deepseek/deepseek-chat-v3.1"
	ModelB = "google/gemini-2.5-flash"
)

// modelos compatíveis/validados para todos os motores (true=validado).
// o restante da lista (opencode models) fica compatible=null no frontend.
var validatedModels = map[string]bool{
	ModelA: true,
	ModelB: true,
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
