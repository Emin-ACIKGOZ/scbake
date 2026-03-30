// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package types

// Reporter defines the interface for project status and task progress reporting.
type Reporter interface {
	// Step logs a high-level orchestration milestone.
	Step(emoji, message string)

	// SetTotalSteps updates the denominator for step logging.
	SetTotalSteps(total int)

	// TaskStart initiates a progress indicator for a specific sub-task.
	TaskStart(description string, current, total int)

	// TaskEnd finalizes the progress indicator for the current sub-task.
	TaskEnd(err error)
}
