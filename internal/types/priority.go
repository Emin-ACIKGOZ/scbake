package types

import (
	"fmt"
	"sync"
)

// Priority defines the execution order of a task.
type Priority int

// PrioritySequence is a thread-safe helper to generate strictly sequential priorities within a band.
type PrioritySequence struct {
	mu      sync.Mutex
	current Priority
	max     Priority
}

// NewPrioritySequence creates a sequence starting at `base`.
// Optional `max` allows to set an upper bound of a band flexibly.
// (use 0 for no max, meaning unlimited).
func NewPrioritySequence(base, max Priority) *PrioritySequence {
	if max != 0 && max < base {
		panic(fmt.Sprintf("invalid priority band: base=%d > max=%d", base, max))
	}
	return &PrioritySequence{current: base, max: max}
}

// Next returns the next priority and increments the counter.
// Returns error if it would exceed the max (if set), enforcing band safety.
func (s *PrioritySequence) Next() (Priority, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if current is beyond max before returning.
	if s.max != 0 && s.current > s.max {
		return 0, fmt.Errorf("priority sequence exceeded max: %d > %d", s.current, s.max)
	}

	p := s.current
	s.current++
	return p, nil
}

// --- Priority Band Definitions ---

const (
	// PrioDirCreate: Foundation (used only for CreateDir tasks)
	PrioDirCreate Priority = 50

	// PrioLangSetup: The band for all language tasks (setup, init, deps).
	PrioLangSetup Priority = 100

	// --- Tooling Bands ---
	PrioConfigUniversal Priority = 1000
	PrioCI              Priority = 1100
	PrioLinter          Priority = 1200
	PrioBuildSystem     Priority = 1400
	PrioDevEnv          Priority = 1500

	// --- Max Values ---

	// Inclusive ceiling for each band.
	// Defined explicitly using the next base value minus one.

	MaxDirCreate       Priority = PrioLangSetup - 1       // 99
	MaxLangSetup       Priority = PrioConfigUniversal - 1 // 999
	MaxConfigUniversal Priority = PrioCI - 1              // 1099
	MaxCI              Priority = PrioLinter - 1          // 1199
	MaxLinter          Priority = PrioBuildSystem - 1     // 1399
	MaxBuildSystem     Priority = PrioDevEnv - 1          // 1499
	// PrioDevEnv has no defined max; it runs last and is unlimited (max=0).
)
