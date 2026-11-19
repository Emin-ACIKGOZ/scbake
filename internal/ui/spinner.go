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

// spinnerTickHook is a test seam invoked when a ticker frame is selected,
// before it takes the output mutex. It is nil in production. Tests that set it
// must remain serial (no t.Parallel) and restore it in t.Cleanup.
var spinnerTickHook func()

// SpinnerReporter provides an interactive terminal UI with an animated spinner.
type SpinnerReporter struct {
	// lifecycleMu serializes TaskStart and TaskEnd so concurrent callers cannot
	// overwrite each other's lifecycle fields or orphan a renderer.
	lifecycleMu   sync.Mutex
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	// A previous task that was never ended is superseded here. Join it before
	// installing the new state so no stale renderer can write after this task's
	// TaskEnd returns. This join is unbounded by design; a renderer blocked in a
	// stalled stdout write delays TaskStart until the write drains.
	if previous, ok := r.detachTask(); ok {
		r.stopTask(previous)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Create context with timeout to prevent goroutine leaks if TaskEnd() is never called.
	// cancel is stored on the reporter and invoked by stopTask.
	ctx, cancel := context.WithTimeout(context.Background(), spinnerTimeout) //nolint:gosec // cancel is retained and called by stopTask
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

// spinnerTask is a detached task lifecycle.
type spinnerTask struct {
	done    chan struct{}
	stopped chan struct{}
	cancel  context.CancelFunc
	desc    string
	index   int
	total   int
}

// detachTask atomically clears and returns the active task state. It returns
// false when no task is active, making repeated TaskEnd calls no-ops.
func (r *SpinnerReporter) detachTask() (spinnerTask, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done == nil {
		return spinnerTask{}, false
	}
	task := spinnerTask{
		done:    r.done,
		stopped: r.spinnerStopped,
		cancel:  r.spinnerCancel,
		desc:    r.activeDesc,
		index:   r.activeIndex,
		total:   r.activeTotal,
	}
	r.done = nil
	r.spinnerStopped = nil
	r.spinnerCancel = nil
	return task, true
}

// stopTask signals and joins a detached renderer. The join has no timeout: a
// renderer already blocked in a stdout write cannot observe cancellation, and a
// bounded wait would permit output after the caller believes the task ended.
// This means a stalled stdout delays both TaskEnd and a superseding TaskStart
// until the write drains.
func (r *SpinnerReporter) stopTask(task spinnerTask) {
	close(task.done)
	if task.cancel != nil {
		task.cancel()
	}
	<-task.stopped
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
			if spinnerTickHook != nil {
				spinnerTickHook()
			}
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
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()

	task, ok := r.detachTask()
	if !ok {
		return
	}
	// Join before printing so no spinner write can follow the final indicator.
	r.stopTask(task)

	outputMux.Lock()
	defer outputMux.Unlock()
	prefix := fmt.Sprintf("[%d/%d]", task.index, task.total)
	if err != nil {
		fmt.Printf("\r%s ❌ %s\n", prefix, task.desc)
		return
	}
	fmt.Printf("\r%s ✅ %s\n", prefix, task.desc)
}
