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
// Optional `maxPrio` allows to set an upper bound of a band flexibly.
// (use 0 for no max, meaning unlimited).
func NewPrioritySequence(base, maxPrio Priority) *PrioritySequence {
	if maxPrio != 0 && maxPrio < base {
		panic(fmt.Sprintf("invalid priority band: base=%d > max=%d", base, maxPrio))
	}
	return &PrioritySequence{current: base, max: maxPrio}
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
	// PrioDirCreate is the foundation band used for directory creation tasks.
	PrioDirCreate Priority = 50

	// PrioLangSetup is the band for all language tasks (setup, init, deps).
	PrioLangSetup Priority = 100

	// --- Tooling Bands ---

	// PrioConfigUniversal is for universal configuration files (e.g., .editorconfig).
	PrioConfigUniversal Priority = 1000

	// PrioCI is for Continuous Integration setup tasks (e.g., .github/workflows).
	PrioCI Priority = 1100

	// PrioLinter is for linter configuration tasks.
	PrioLinter Priority = 1200 // FIXED: Added comment

	// PrioBuildSystem is for build system configuration tasks (e.g., Makefiles).
	PrioBuildSystem Priority = 1400 // FIXED: Added comment

	// PrioDevEnv is for environment setup tasks (e.g., Dev Containers).
	PrioDevEnv Priority = 1500 // FIXED: Added comment

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
