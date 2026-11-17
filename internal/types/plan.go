// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"
	"scbake/internal/filesystem/transaction"
)

// Conflict resolution strategies accepted by --conflict-strategy.
const (
	ConflictFail      = "fail"
	ConflictOverwrite = "overwrite"
	ConflictArtifact  = "artifact"
	ConflictKeepLocal = "keep-local"
	// ConflictMerge performs a three-way merge against the pristine base shadow,
	// falling back to an artifact when no base exists or the merge conflicts.
	ConflictMerge = "merge"
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

	// Update indicates a manifest-driven reconciliation (`update` or
	// `drift --repair`) rather than a flag-driven apply. Tasks that fetch
	// external content may skip work when their managed output is current.
	Update bool

	// Force indicates if we should overwrite existing files.
	Force bool

	// ConflictStrategy determines how to handle state-aware drift (fail, overwrite, artifact, keep-local)
	ConflictStrategy string

	// TemplateDir is the path to an external directory containing template overrides.
	TemplateDir string

	// RegistryCacheDir is the path to the local cache of pulled registry templates.
	RegistryCacheDir string

	// RegistryNames is the set of configured registry names. Registry cache
	// resolution is limited to these names; an empty set disables the registry
	// tier (fail closed).
	RegistryNames []string

	// Tx is the active filesystem transaction manager.
	// If nil, tasks perform operations without safety tracking (legacy/testing mode).
	Tx *transaction.Manager
}

// Task is the interface for all atomic operations (e.g., create file, exec command).
type Task interface {
	// Description returns a short, user-facing summary shown in CLI progress output.
	Description() string

	// Priority returns the execution order; lower values run first within a band.
	Priority() int

	// Execute performs the task under the supplied context and transaction.
	Execute(tc TaskContext) error
}

// Plan is a sorted list of tasks to be executed.
type Plan struct {
	Tasks []Task
}

// SourceDescriber is implemented by tasks that can name the template source
// tier they would resolve. The executor calls it only for dry-run output so a
// preview shows whether a file comes from --template-dir, a configured
// registry, or the embedded defaults.
type SourceDescriber interface {
	DescriptionWithSource(tc TaskContext) string
}

// RegistryAwareHandler is optionally implemented by language and tooling
// handlers that resolve registry-cached templates at plan time. Core calls it
// with the configured registry allowlist. Handlers that only build
// CreateTemplateTask need not implement it because resolution happens at
// execution time, where the allowlist is already available.
type RegistryAwareHandler interface {
	GetTasksWithRegistries(targetPath, templateDir, registryCacheDir string, registries []string) ([]Task, error)
}
