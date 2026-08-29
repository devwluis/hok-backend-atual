package main

import (
	"context"
	"log"
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ─── Monitor do celular (Redmi via túnel Termux) — 28/08 ────────────────────
// O celular do usuário (Termux + sshd + túnel reverso ssh -R 8022) fica
// acessível em localhost:8022 do servidor. Este endpoint lê RAM/espaço/load
// e alimenta o workflow de monitoramento no n8n (imoveischaves.com).

const (
	celularSshPort = "8022"
	celularSSHKey  = "/root/.ssh/id_ed25519"
	celularTimeout = 12 * time.Second
)

type celularStatus struct {
	Status         string  `json:"status"` // ok | tunel_fechado | erro
	Error          string  `json:"error,omitempty"`
	Ts             string  `json:"ts"`
	RAMTotalGB     float64 `json:"ram_total_gb"`
	RAMUsedGB      float64 `json:"ram_used_gb"`
	RAMUsedPercent float64 `json:"ram_used_percent"`
	RAMAvailableGB float64 `json:"ram_available_gb"`
	SpaceTotalGB   float64 `json:"space_total_gb"`
	SpaceUsedGB    float64 `json:"space_used_gb"`
	SpaceFreeGB    float64 `json:"space_free_gb"`
	SpaceUsedPct   float64 `json:"space_used_percent"`
	Load1          float64 `json:"load_1min"`
	Load5          float64 `json:"load_5min"`
	Load15         float64 `json:"load_15min"`
	BatteryPct     int     `json:"battery_percent"`
	BatteryHealth  string  `json:"battery_health"`
	BatteryTemp    float64 `json:"battery_temp_c"`
	BatteryCharging string `json:"battery_charging"` // charging | discharging | full
}

// parseGB converte "5.5Gi", "3.7G", "145Mi", "512K" em GB float.
func parseGB(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "i")
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "M"):
		s = strings.TrimSuffix(s, "M")
		mult = 1.0 / 1024
	case strings.HasSuffix(s, "K"):
		s = strings.TrimSuffix(s, "K")
		mult = 1.0 / (1024 * 1024)
	default:
		s = strings.TrimSuffix(s, "G")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v * mult
}

func celularRun(cmdline string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), celularTimeout)
	defer cancel()
	args := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "ConnectTimeout=5",
		"-i", celularSSHKey,
		"-p", celularSshPort,
		"localhost", cmdline,
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func handleCelularStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	st := celularStatus{Status: "ok", Ts: time.Now().Format(time.RFC3339)}

	out, err := celularRun(`free -h | head -2; echo '###'; df -h /storage/emulated 2>/dev/null | tail -1; echo '###'; uptime`)
	if err != nil {
		st.Status = "tunel_fechado"
		st.Error = err.Error()
		log.Printf("[AUDIT] celular/status: túnel fechado ou sshd offline: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(st)
		return
	}

	parts := strings.Split(out, "###")
	if len(parts) >= 1 {
		reMem := regexp.MustCompile(`Mem:\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)\s+(\S+)`)
		if m := reMem.FindStringSubmatch(parts[0]); len(m) >= 7 {
			st.RAMTotalGB = parseGB(m[1])
			st.RAMUsedGB = parseGB(m[2])
			st.RAMAvailableGB = parseGB(m[6])
			if st.RAMTotalGB > 0 {
				st.RAMUsedPercent = float64(int(st.RAMUsedGB/st.RAMTotalGB*10000+0.5)) / 100
			}
		}
	}
	if len(parts) >= 2 {
		fields := strings.Fields(parts[1])
		if len(fields) >= 5 {
			st.SpaceTotalGB = parseGB(fields[1])
			st.SpaceUsedGB = parseGB(fields[2])
			st.SpaceFreeGB = parseGB(fields[3])
			pct := strings.TrimSuffix(fields[4], "%")
			if v, err := strconv.ParseFloat(pct, 64); err == nil {
				st.SpaceUsedPct = v
			}
		}
	}
	if len(parts) >= 3 {
		// Formato: "22:52:44 up 1:22, load average: 20.94, 21.20, 21.71"
		loads := regexp.MustCompile(`load average:\s*([\d.]+),\s*([\d.]+),\s*([\d.]+)`)
		if m := loads.FindStringSubmatch(parts[2]); len(m) >= 4 {
			st.Load1, _ = strconv.ParseFloat(m[1], 64)
			st.Load5, _ = strconv.ParseFloat(m[2], 64)
			st.Load15, _ = strconv.ParseFloat(m[3], 64)
		}
	}
	// Bateria via Termux:API (termux-battery-status → JSON).
	if bout, berr := celularRun("termux-battery-status 2>/dev/null"); berr == nil {
		var bat struct {
			Percentage  int     `json:"percentage"`
			Health      string  `json:"health"`
			Temperature float64 `json:"temperature"`
			Status      string  `json:"status"`
		}
		if json.Unmarshal([]byte(bout), &bat) == nil {
			st.BatteryPct = bat.Percentage
			st.BatteryHealth = bat.Health
			st.BatteryTemp = bat.Temperature
			switch bat.Status {
			case "CHARGING":
				st.BatteryCharging = "charging"
			case "FULL":
				st.BatteryCharging = "full"
			default:
				st.BatteryCharging = "discharging"
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(st)
}

