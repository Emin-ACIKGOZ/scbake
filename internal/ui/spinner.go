// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package ui

import (
	"fmt"
	"sync"
	"time"
)

var spinnerChars = []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}

const spinnerDelay = 100 * time.Millisecond

// SpinnerReporter provides an interactive terminal UI with an animated spinner.
type SpinnerReporter struct {
	mu          sync.Mutex
	currentStep int
	totalSteps  int
	activeDesc  string
	activeIndex int
	activeTotal int
	done        chan struct{}
}

// NewSpinnerReporter initializes a reporter with a specified number of high-level steps.
func NewSpinnerReporter(totalSteps int) *SpinnerReporter {
	return &SpinnerReporter{totalSteps: totalSteps}
}

// Step logs a high-level orchestration milestone with a progress prefix and emoji.
func (r *SpinnerReporter) Step(emoji, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentStep++
	fmt.Printf("[%d/%d] %s %s\n", r.currentStep, r.totalSteps, emoji, message)
}

// SetTotalSteps updates the total number of high-level steps for future progress logging.
func (r *SpinnerReporter) SetTotalSteps(total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalSteps = total
}

// TaskStart initiates the spinner animation for a specific sub-task in a separate goroutine.
func (r *SpinnerReporter) TaskStart(desc string, curr, total int) {
	r.mu.Lock()
	r.activeDesc = desc
	r.activeIndex = curr
	r.activeTotal = total
	r.done = make(chan struct{})
	r.mu.Unlock()

	go r.animate()
}

// animate handles the ticker logic and ANSI escape sequences for the spinner animation.
func (r *SpinnerReporter) animate() {
	ticker := time.NewTicker(spinnerDelay)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			prefix := fmt.Sprintf("[%d/%d]", r.activeIndex, r.activeTotal)
			fmt.Printf("\r%s %s %s", prefix, spinnerChars[i%len(spinnerChars)], r.activeDesc)
			i++
		}
	}
}

// TaskEnd stops the active spinner goroutine and prints a final success or failure indicator.
func (r *SpinnerReporter) TaskEnd(err error) {
	close(r.done)
	prefix := fmt.Sprintf("[%d/%d]", r.activeIndex, r.activeTotal)
	if err != nil {
		fmt.Printf("\r%s ❌ %s\n", prefix, r.activeDesc)
		return
	}
	fmt.Printf("\r%s ✅ %s\n", prefix, r.activeDesc)
}
