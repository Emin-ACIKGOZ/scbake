// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"context"
	"fmt"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"strings"
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

// ConflictStrategies returns the conflict resolution strategies accepted by
// --conflict-strategy, in the order they are documented.
func ConflictStrategies() []string {
	return []string{ConflictMerge, ConflictFail, ConflictOverwrite, ConflictArtifact, ConflictKeepLocal}
}

// ValidateConflictStrategy reports whether strategy is an accepted
// --conflict-strategy value. The error names every accepted value so a typo is
// rejected instead of being treated as one of the strategies.
func ValidateConflictStrategy(strategy string) error {
	switch strategy {
	case ConflictMerge, ConflictFail, ConflictOverwrite, ConflictArtifact, ConflictKeepLocal:
		return nil
	default:
		return fmt.Errorf(
			"invalid conflict strategy %q: must be one of: %s",
			strategy,
			strings.Join(ConflictStrategies(), ", "),
		)
	}
}

// TaskContext holds all the data a Task needs to run.
type TaskContext struct {
	// Ctx is the application context, for cancellation.
	Ctx context.Context

	// TargetPath is the path the task should operate in (e.g., "./backend").
	TargetPath string

	// Root is the absolute manifest/project root. It anchors root-relative
	// decisions (managed_files keys, owning project, base shadow) when no
	// transaction is active, as in a dry run. A transaction root, when present,
	// remains authoritative.
	Root string

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

	// ConflictStrategy determines how to handle state-aware drift (merge, fail, overwrite, artifact, keep-local)
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

	// PlannedPaths lists absolute paths that earlier tasks in the current plan
	// declared they may create (see CreatedPathProvider). The executor fills it
	// between tasks; read-only validation uses it to distinguish an artifact
	// that is not there yet from one that is genuinely missing.
	PlannedPaths []string

	// PlannedWrites accumulates the canonical managed-file writes a dry run
	// would perform, keyed by root-relative managed_files key. It is nil when no
	// dry-run prediction is being collected; tasks only record into a non-nil
	// map, so a real run never populates it.
	PlannedWrites map[string]PlannedWrite
}

// PlannedWrite is a canonical managed-file write a dry-run plan would perform.
// It lets a preview predict which drift findings the plan repairs without
// executing anything.
type PlannedWrite struct {
	// Key is the root-relative managed_files key the write targets.
	Key string
	// Clean reports that the canonical file would end equal to the pristine
	// render after the write: there was no merge override, or the override
	// equals the pending render. A clean write clears content drift for Key.
	Clean bool
}

// PathPlanned reports whether absPath, or an ancestor of it, was declared by an
// earlier task in the current plan. It is read-only and makes no filesystem
// calls.
func (tc TaskContext) PathPlanned(absPath string) bool {
	if absPath == "" {
		return false
	}
	absPath = filepath.Clean(absPath)
	for _, planned := range tc.PlannedPaths {
		if planned == "" {
			continue
		}
		planned = filepath.Clean(planned)
		if absPath == planned || strings.HasPrefix(absPath, planned+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// CreatedPathProvider is an optional capability for tasks that can declare the
// paths they create. Declarations must be absolute and computed without side
// effects. The executor carries them into TaskContext.PlannedPaths so a later
// task's Validate does not fail in a dry run on an artifact an earlier task is
// expected to produce.
type CreatedPathProvider interface {
	CreatedPaths(tc TaskContext) []string
}

// Task is the interface for all atomic operations (e.g., create file, exec command).
type Task interface {
	// Description returns a short, user-facing summary shown in CLI progress output.
	Description() string

	// Priority returns the execution order; lower values run first within a band.
	Priority() int

	// Validate performs every read-only check Execute makes before its first
	// write: path safety, preconditions, conflict decisions, and up-front
	// parsing. It must not create, modify, or delete anything, and must return
	// the same error Execute would return for the same starting state.
	//
	// The executor calls Validate instead of Execute in a dry run, so a task
	// without an honest Validate silently reintroduces false-success previews.
	// Implementations must share their decision logic with Execute (for example
	// a single render/decide helper) so the two cannot drift.
	Validate(tc TaskContext) error

	// Execute performs the task under the supplied context and transaction. It
	// must remain consistent with Validate: the checks Validate performs come
	// first, then the writes.
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

// DryRunAnnotator is optionally implemented by tasks that can explain the
// reconciliation outcome their read-only Validate just decided: a no-op, a
// preserved local edit, a converged write, or a conflict diverted to an
// artifact. The executor calls DryRunNote after Validate and, only in a dry run,
// appends a non-empty note to the reported description. A note does not promise
// that no filesystem write occurs: an artifact writes a side file, and a
// merge-preserved reconciliation may still write bytes identical to the file on
// disk. It lets the preview distinguish those outcomes from an ordinary create
// or update. The note is computed during Validate because that is where the task
// has already made the same read-only decision the real run would make;
// computing it separately could disagree.
type DryRunAnnotator interface {
	DryRunNote() string
}

// DryRunVerbProvider is optionally implemented by tasks that can replace the
// leading verb of their dry-run description. The executor calls DryRunVerb after
// Validate, only in a dry run, and substitutes it for the description's first
// word while preserving the rest (including any source suffix). It lets a task
// that just decided on a no-op report "Skip main.go" instead of "Create
// main.go". An empty verb leaves the description unchanged.
type DryRunVerbProvider interface {
	DryRunVerb() string
}

// RegistryAwareHandler is optionally implemented by language and tooling
// handlers that resolve registry-cached templates at plan time. Core calls it
// with the configured registry allowlist. Handlers that only build
// CreateTemplateTask need not implement it because resolution happens at
// execution time, where the allowlist is already available.
type RegistryAwareHandler interface {
	GetTasksWithRegistries(targetPath, templateDir, registryCacheDir string, registries []string) ([]Task, error)
}
