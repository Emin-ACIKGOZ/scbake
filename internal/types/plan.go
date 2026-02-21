// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package types holds the core data structures for the scbake manifest and tasks.
package types

import (
	"context"
	"scbake/internal/filesystem/transaction"
)

// TaskContext holds all the data a Task needs to run.
type TaskContext struct {
	// Ctx is the application context, for cancellation.
	Ctx context.Context

	// TargetPath is the path the task should operate in (e.g., "./backend").
	TargetPath string

	// Manifest is the *current* state of the manifest, read-only.
	Manifest *Manifest

	// DryRun indicates if we are in dry-run mode.
	DryRun bool

	// Force indicates if we should overwrite existing files.
	Force bool

	// Tx is the active filesystem transaction manager.
	// If nil, tasks perform operations without safety tracking (legacy/testing mode).
	Tx *transaction.Manager
}

// Task is the interface for all atomic operations (e.g., create file, exec command).
type Task interface {
	// Description provides a human-readable summary for logging.
	Description() string

	// Priority determines the execution order. Lower numbers run first.
	Priority() int

	// Execute performs the actual work.
	Execute(tc TaskContext) error
}

// Plan is a sorted list of tasks to be executed.
type Plan struct {
	Tasks []Task
}
