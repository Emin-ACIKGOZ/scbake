// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package ui

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var (
	spinnerChars = []string{"⣷", "⣯", "⣟", "⡿", "⢿", "⣻", "⣽", "⣾"}
	// outputMux synchronizes all output to prevent interleaving from concurrent goroutines
	outputMux sync.Mutex
)

const (
	spinnerDelay    = 100 * time.Millisecond
	spinnerTimeout  = 30 * time.Minute
)

// SpinnerReporter provides an interactive terminal UI with an animated spinner.
type SpinnerReporter struct {
	mu          sync.Mutex
	currentStep int
	totalSteps  int
	activeDesc  string
	activeIndex int
	activeTotal int
	done        chan struct{}
	spinnerCtx  context.Context
	spinnerCancel context.CancelFunc
}

// NewSpinnerReporter initializes a reporter with a specified number of high-level steps.
func NewSpinnerReporter(totalSteps int) *SpinnerReporter {
	return &SpinnerReporter{totalSteps: totalSteps}
}

// Step logs a high-level orchestration milestone with a progress prefix and emoji.
func (r *SpinnerReporter) Step(emoji, message string) {
	r.mu.Lock()
	r.currentStep++
	step := r.currentStep
	total := r.totalSteps
	r.mu.Unlock()
	outputMux.Lock()
	defer outputMux.Unlock()
	fmt.Printf("[%d/%d] %s %s\n", step, total, emoji, message)
}

// SetTotalSteps updates the total number of high-level steps for future progress logging.
func (r *SpinnerReporter) SetTotalSteps(total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.totalSteps = total
}

// TaskStart initiates the spinner animation for a specific sub-task in a separate goroutine.
func (r *SpinnerReporter) TaskStart(desc string, curr, total int) {
	// Cancel previous spinner if still running (cleanup)
	if r.spinnerCancel != nil {
		r.spinnerCancel()
	}

	// Create context with timeout to prevent goroutine leaks if TaskEnd() is never called
	r.spinnerCtx, r.spinnerCancel = context.WithTimeout(context.Background(), spinnerTimeout)

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
		case <-r.spinnerCtx.Done():
			return
		case <-ticker.C:
			r.mu.Lock()
			desc := r.activeDesc
			index := r.activeIndex
			total := r.activeTotal
			r.mu.Unlock()
			outputMux.Lock()
			prefix := fmt.Sprintf("[%d/%d]", index, total)
			fmt.Printf("\r%s %s %s", prefix, spinnerChars[i%len(spinnerChars)], desc)
			outputMux.Unlock()
			i++
		}
	}
}

// TaskEnd stops the active spinner goroutine and prints a final success or failure indicator.
func (r *SpinnerReporter) TaskEnd(err error) {
	// Signal goroutine to stop (primary method)
	close(r.done)

	// Cancel context to ensure cleanup if done channel was already consumed
	if r.spinnerCancel != nil {
		r.spinnerCancel()
	}

	r.mu.Lock()
	desc := r.activeDesc
	index := r.activeIndex
	total := r.activeTotal
	r.mu.Unlock()
	outputMux.Lock()
	defer outputMux.Unlock()
	prefix := fmt.Sprintf("[%d/%d]", index, total)
	if err != nil {
		fmt.Printf("\r%s ❌ %s\n", prefix, desc)
		return
	}
	fmt.Printf("\r%s ✅ %s\n", prefix, desc)
}
