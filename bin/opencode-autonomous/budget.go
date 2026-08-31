package main

import (
	"sync"
)

// BudgetTracker — contador de ações com mutex para concorrência segura
type BudgetTracker struct {
	mu       sync.Mutex
	budget   int
	used     int
	warnAt   int // avisa quando restam X ações (default 3)
	warned   bool
}

func NewBudgetTracker(budget int) *BudgetTracker {
	return &BudgetTracker{
		budget: budget,
		used:   0,
		warnAt: 3,
	}
}

// Consume — decrementa 1 ação. Retorna (remaining, blocked).
// blocked=true quando budget esgotado.
func (b *BudgetTracker) Consume() (remaining int, blocked bool, warned bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used++
	remaining = b.budget - b.used
	if remaining <= 0 {
		return 0, true, false
	}
	if remaining <= b.warnAt && !b.warned {
		b.warned = true
		return remaining, false, true
	}
	return remaining, false, false
}

func (b *BudgetTracker) Used() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

func (b *BudgetTracker) Budget() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.budget
}