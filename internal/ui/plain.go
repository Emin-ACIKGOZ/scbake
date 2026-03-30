// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package ui

import "fmt"

// PlainReporter provides a static, line-based output format.
type PlainReporter struct {
	currentStep int
	totalSteps  int
	isDryRun    bool
}

// NewPlainReporter initializes a reporter for non-interactive or dry-run environments.
func NewPlainReporter(totalSteps int, dryRun bool) *PlainReporter {
	return &PlainReporter{totalSteps: totalSteps, isDryRun: dryRun}
}

// Step logs a milestone. In dry-run mode, it suppresses output after the initial phase.
func (r *PlainReporter) Step(emoji, message string) {
	r.currentStep++
	if r.isDryRun && r.currentStep > 2 {
		return
	}
	fmt.Printf("[%d/%d] %s %s\n", r.currentStep, r.totalSteps, emoji, message)
}

// SetTotalSteps updates the denominator for progress tracking.
func (r *PlainReporter) SetTotalSteps(total int) {
	r.totalSteps = total
}

// TaskStart logs a dry-run task description. Standard tasks are silent in plain mode.
func (r *PlainReporter) TaskStart(desc string, _, _ int) {
	if r.isDryRun {
		fmt.Printf("  [DRY RUN] %s\n", desc)
	}
}

// TaskEnd is a no-op for the plain reporter to satisfy the Reporter interface.
func (r *PlainReporter) TaskEnd(_ error) {}
