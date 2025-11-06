package types

import "context"

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
