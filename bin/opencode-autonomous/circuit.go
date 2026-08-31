package main

import (
	"sync"
	"time"
)

// CBEvent — evento do circuit breaker. EXPORTADO (uppercase) pra JSON.
// Incluído em SessionState.CBEvents e persistido em state/current.
type CBEvent struct {
	Hash    string    `json:"hash"`
	Summary string    `json:"summary"`
	At      time.Time `json:"at"`
}

// CircuitBreaker — detecta ações idênticas repetidas dentro de uma janela.
// Mesma lógica do autonomous.go do HOK: N ações idênticas em W minutos → bloqueia.
// Os events são PERSISTIDOS (carregados/salvos via Snapshot/LoadSnapshot).
type CircuitBreaker struct {
	mu        sync.Mutex
	window    time.Duration
	maxRepeat int
	events    []CBEvent // append-only durante a sessão; persiste via Snapshot
}

func NewCircuitBreaker(maxRepeat int, windowMins int) *CircuitBreaker {
	return &CircuitBreaker{
		window:    time.Duration(windowMins) * time.Minute,
		maxRepeat: maxRepeat,
	}
}

// NewCircuitBreakerWithEvents — restaura CB com events de uma sessão anterior
// (item 6: continuidade após crash/reinício).
func NewCircuitBreakerWithEvents(maxRepeat, windowMins int, events []CBEvent) *CircuitBreaker {
	cb := NewCircuitBreaker(maxRepeat, windowMins)
	now := time.Now()
	cutoff := now.Add(-cb.window)
	for _, e := range events {
		if e.At.After(cutoff) {
			cb.events = append(cb.events, e)
		}
	}
	return cb
}

// Snapshot — retorna cópia dos events atuais (para persistir no state/current).
func (cb *CircuitBreaker) Snapshot() []CBEvent {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	out := make([]CBEvent, len(cb.events))
	copy(out, cb.events)
	return out
}

// Record — registra uma ação e retorna (repeatCount, triggered).
// triggered=true quando hash apareceu maxRepeat vezes dentro da janela.
func (cb *CircuitBreaker) Record(hash string, summary string) (repeatCount int, triggered bool) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-cb.window)

	// Poda events fora da janela
	pruned := cb.events[:0]
	for _, e := range cb.events {
		if e.At.After(cutoff) {
			pruned = append(pruned, e)
		}
	}
	cb.events = pruned

	// Adiciona este
	cb.events = append(cb.events, CBEvent{Hash: hash, Summary: summary, At: now})

	// Conta ocorrências deste hash na janela
	count := 0
	for _, e := range cb.events {
		if e.Hash == hash {
			count++
		}
	}

	if count >= cb.maxRepeat {
		return count, true
	}
	return count, false
}

// TriggeredReason — mensagem humana do motivo do bloqueio
func (cb *CircuitBreaker) TriggeredReason() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if len(cb.events) == 0 {
		return ""
	}
	last := cb.events[len(cb.events)-1]
	count := 0
	for _, e := range cb.events {
		if e.Hash == last.Hash {
			count++
		}
	}
	if count < cb.maxRepeat {
		return ""
	}
	return last.Summary
}