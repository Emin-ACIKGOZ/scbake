// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package ui

import (
	"os"
	"scbake/internal/types"
)

// NewReporter selects and returns the optimal Reporter implementation based on
// the execution context and terminal capabilities.
func NewReporter(totalSteps int, dryRun bool) types.Reporter {
	// Dry run always uses the plain reporter to avoid unnecessary goroutines
	// and to maintain a clean preview of the plan.
	if dryRun {
		return NewPlainReporter(totalSteps, true)
	}

	// Automated detection for non-interactive environments (CI, pipes, redirects).
	if !isTerminal() {
		return NewPlainReporter(totalSteps, false)
	}

	return NewSpinnerReporter(totalSteps)
}

// isTerminal checks if os.Stdout is connected to an interactive terminal.
func isTerminal() bool {
	fileInfo, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	// Check if the output is a character device (TTY).
	return (fileInfo.Mode() & os.ModeCharDevice) != 0
}
