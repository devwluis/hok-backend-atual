package main

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
