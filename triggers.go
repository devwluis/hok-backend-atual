package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	diskThresholdPercent = 80.0
	diskCooldown         = 30 * time.Minute
	memoryCooldown       = 5 * time.Minute
	errorCooldown        = 2 * time.Minute
	triggerCheckInterval = 60 * time.Second
)

type triggerState struct {
	lastFired time.Time
	lastMsg   string
}

var (
	triggerMu     sync.Mutex
	triggerStates = map[string]*triggerState{
		"on_disk":           {},
		"on_memory_insert":  {},
		"on_error_detected": {},
	}

	failureMu       sync.Mutex
	pendingFailures []string

	lastSeenMemoryCount = -1
)

func markSkillFailure(skillName, detail string) {
	failureMu.Lock()
	defer failureMu.Unlock()
	pendingFailures = append(pendingFailures, fmt.Sprintf("%s: %s", skillName, detail))
}

func drainFailures() []string {
	failureMu.Lock()
	defer failureMu.Unlock()
	out := pendingFailures
	pendingFailures = nil
	return out
}

func diskUsedPercent(path string) (float64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	total := float64(stat.Blocks)
	avail := float64(stat.Bavail)
	if total == 0 {
		return 0, fmt.Errorf("total de blocos zero")
	}
	return (1 - avail/total) * 100, nil
}

func canFireTrigger(name string, cooldown time.Duration) bool {
	triggerMu.Lock()
	defer triggerMu.Unlock()
	st := triggerStates[name]
	if st == nil {
		st = &triggerState{}
		triggerStates[name] = st
	}
	return time.Since(st.lastFired) >= cooldown
}

func recordTriggerFire(name, msg string) {
	triggerMu.Lock()
	defer triggerMu.Unlock()
	st := triggerStates[name]
	if st == nil {
		st = &triggerState{}
		triggerStates[name] = st
	}
	st.lastFired = time.Now()
	st.lastMsg = msg
}

func triggerTruncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func fireTrigger(triggerName, skillName, input string) {
	output, success, source, err := runSkillWithRetry(skillName, input)
	msg := fmt.Sprintf("skill=%s success=%v source=%s", skillName, success, source)
	if err != nil {
		msg = fmt.Sprintf("erro: %s", err.Error())
	}
	recordTriggerFire(triggerName, msg)
	log.Printf("trigger %s disparou skill '%s' — %s | output: %s",
		triggerName, skillName, msg, triggerTruncate(output, 300))
}

func runTriggerLoop() {
	time.Sleep(10 * time.Second)

	teleMu.RLock()
	lastSeenMemoryCount = getSQLiteCount("memory")
	teleMu.RUnlock()

	log.Println("Trigger loop ativo (on_disk, on_memory_insert, on_error_detected)")

	for {
		if pct, err := diskUsedPercent(ROOT_PATH); err == nil {
			if pct > diskThresholdPercent && canFireTrigger("on_disk", diskCooldown) {
				go fireTrigger("on_disk", "Docker Limpar Tudo",
					fmt.Sprintf("disco em %.1f%%, limpar imagens e containers nao usados", pct))
			}
		}

		teleMu.RLock()
		current := getSQLiteCount("memory")
		teleMu.RUnlock()
		if lastSeenMemoryCount >= 0 && current > lastSeenMemoryCount && canFireTrigger("on_memory_insert", memoryCooldown) {
			go fireTrigger("on_memory_insert", "Consolidar Mente",
				fmt.Sprintf("%d novas memorias desde a ultima consolidacao", current-lastSeenMemoryCount))
		}
		lastSeenMemoryCount = current

		if failures := drainFailures(); len(failures) > 0 && canFireTrigger("on_error_detected", errorCooldown) {
			go fireTrigger("on_error_detected", "Ver Erros Backend", strings.Join(failures, "; "))
		}

		time.Sleep(triggerCheckInterval)
	}
}

func handleTriggersStatus(w http.ResponseWriter, r *http.Request) {
	setCORS(w)
	if r.Method == "OPTIONS" {
		w.WriteHeader(204)
		return
	}
	if !requireHokAuth(w, r) {
		return
	}
	triggerMu.Lock()
	out := make(map[string]interface{}, len(triggerStates))
	for name, st := range triggerStates {
		out[name] = map[string]interface{}{
			"last_fired": st.lastFired,
			"last_msg":   st.lastMsg,
		}
	}
	triggerMu.Unlock()
	respondJSON(w, out)
}
