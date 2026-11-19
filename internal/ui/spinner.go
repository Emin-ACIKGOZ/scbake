// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

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
	spinnerDelay   = 100 * time.Millisecond
	spinnerTimeout = 30 * time.Minute
)

// SpinnerReporter provides an interactive terminal UI with an animated spinner.
type SpinnerReporter struct {
	mu            sync.Mutex
	currentStep   int
	totalSteps    int
	activeDesc    string
	activeIndex   int
	activeTotal   int
	done          chan struct{}
	spinnerCancel context.CancelFunc
	// spinnerStopped is closed by the animation goroutine when it exits. TaskEnd
	// waits on it so no spinner write can happen after TaskEnd returns.
	spinnerStopped chan struct{}
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
	// A previous task that was never ended is superseded here. Join it before
	// installing the new state so no stale renderer can write after this task's
	// TaskEnd returns.
	r.stopActiveSpinner()

	r.mu.Lock()
	defer r.mu.Unlock()

	// Create context with timeout to prevent goroutine leaks if TaskEnd() is never called
	ctx, cancel := context.WithTimeout(context.Background(), spinnerTimeout)
	done := make(chan struct{})
	stopped := make(chan struct{})

	r.spinnerCancel = cancel
	r.done = done
	r.spinnerStopped = stopped
	r.activeDesc = desc
	r.activeIndex = curr
	r.activeTotal = total

	go r.animate(ctx, done, stopped, desc, curr, total)
}

// stopActiveSpinner cancels and joins the active renderer, if any. It clears
// the lifecycle fields so a later TaskEnd for the superseded task is a no-op.
func (r *SpinnerReporter) stopActiveSpinner() {
	r.mu.Lock()
	done := r.done
	stopped := r.spinnerStopped
	cancel := r.spinnerCancel
	r.done = nil
	r.spinnerStopped = nil
	r.spinnerCancel = nil
	r.mu.Unlock()

	if done == nil {
		return
	}
	close(done)
	if cancel != nil {
		cancel()
	}
	<-stopped
}

// animate handles the ticker logic and ANSI escape sequences for the spinner animation.
// It owns the task state passed by TaskStart, so it never reads fields guarded by r.mu.
func (r *SpinnerReporter) animate(ctx context.Context, done <-chan struct{}, stopped chan<- struct{}, desc string, index, total int) {
	defer close(stopped)
	ticker := time.NewTicker(spinnerDelay)
	defer ticker.Stop()
	i := 0
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			outputMux.Lock()
			prefix := fmt.Sprintf("[%d/%d]", index, total)
			fmt.Printf("\r%s %s %s", prefix, spinnerChars[i%len(spinnerChars)], desc)
			outputMux.Unlock()
			i++
		}
	}
}

// TaskEnd stops the active spinner goroutine and prints a final success or failure indicator.
// It is idempotent: once a task has ended, further calls are no-ops.
func (r *SpinnerReporter) TaskEnd(err error) {
	r.mu.Lock()
	done := r.done
	stopped := r.spinnerStopped
	cancel := r.spinnerCancel
	desc := r.activeDesc
	index := r.activeIndex
	total := r.activeTotal
	// Mark the task as ended. A repeated TaskEnd, or a TaskEnd before TaskStart,
	// then observes nil and returns without touching the closed channel.
	r.done = nil
	r.spinnerStopped = nil
	r.spinnerCancel = nil
	r.mu.Unlock()

	if done == nil {
		return
	}

	// Signal the goroutine with both mechanisms, then join it so it cannot write
	// after TaskEnd returns. Canceling first makes the select observe ctx.Done
	// at its next iteration; the join still waits for any in-flight print.
	close(done)
	if cancel != nil {
		cancel()
	}
	<-stopped

	outputMux.Lock()
	defer outputMux.Unlock()
	prefix := fmt.Sprintf("[%d/%d]", index, total)
	if err != nil {
		fmt.Printf("\r%s ❌ %s\n", prefix, desc)
		return
	}
	fmt.Printf("\r%s ✅ %s\n", prefix, desc)
}
