// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// ScaffoldTask runs an external project generator in a fresh staging leaf and
// moves its top-level output into the target. It is for generators such as
// create-vite that cancel when the target is non-empty; scbake may already have
// created scbake.toml, .scbake, or other project files there.
//
// The staging leaf is named after the resolved target basename so generators
// that derive a package name from the cwd produce the name a direct invocation
// would have.
//
// Staging cleanup after a successful move is best-effort; see
// scaffoldStaging.remove. Commit/Rollback also remove the staging tree with the
// transaction temp dir.
type ScaffoldTask struct {
	// Cmd and Args are the command to run (e.g. npm create vite@latest ...).
	Cmd  string
	Args []string
	Desc string

	TaskPrio int

	// Sentinel is a path relative to the staging leaf that must exist as a
	// regular file after the command. Empty disables the check.
	Sentinel string
}

// scaffoldTargetPaths holds the two forms of the target directory the task
// needs. They are kept separate so destination tracking stays inside the
// transaction's lexical containment domain while the staging leaf still gets
// the name a direct scaffolder invocation would see.
type scaffoldTargetPaths struct {
	// lexical is the absolute target spelling without resolving symlinks. It
	// stays under the same lexical root as Tx.Root(), so fileutil.SafePath
	// accepts destinations built from it even when the project was reached
	// through a symlinked path. Destination Lstat, Track/TrackWrite, and Rename
	// all use this form.
	lexical string

	// resolved is the symlink-free target. It is used only to derive the
	// staging leaf basename and to validate the target, because the scaffolder
	// observes the resolved directory name when it derives a package name.
	resolved string
}

// scaffoldStaging is the transaction-owned staging tree for one scaffold run.
type scaffoldStaging struct {
	// root is the stage-* directory removed after the run.
	root string

	// leaf is the empty directory the scaffolder runs in, named after the
	// canonical target basename.
	leaf string
}

// Description implements types.Task.
func (t *ScaffoldTask) Description() string { return t.Desc }

// Priority implements types.Task.
func (t *ScaffoldTask) Priority() int { return t.TaskPrio }

// Execute stages the scaffold, validates its sentinel, preflights every
// destination, tracks the output, and only then moves it into the target.
// Failures before the move leave the target untouched; a partial move is undone
// by the caller's transaction rollback.
func (t *ScaffoldTask) Execute(tc types.TaskContext) error {
	if tc.DryRun {
		return nil
	}
	if tc.Tx == nil {
		return errors.New("scaffold task requires an active transaction")
	}

	target, err := resolveScaffoldTarget(tc.TargetPath)
	if err != nil {
		return err
	}
	// Fail an escaping target before starting the generator. Tracking would
	// reject it later, but only after the child has already run.
	if _, err := fileutil.SafePath(tc.Tx.Root(), target.lexical); err != nil {
		return fmt.Errorf("refusing to scaffold into %s: %w", target.lexical, err)
	}
	staging, err := t.prepareStaging(tc, target)
	if err != nil {
		return err
	}
	defer staging.remove()

	if err := t.runScaffolder(tc, staging.leaf); err != nil {
		return err
	}

	entries, err := readScaffoldEntries(staging.leaf)
	if err != nil {
		return err
	}

	// Preflight every top-level destination before tracking or moving anything
	// so a collision cannot leave tracked state or a partially moved project.
	if err := preflightDestinations(target.lexical, entries); err != nil {
		return err
	}

	// Track destinations before the move so rollback can remove exactly the
	// bytes this transaction moved. Files are hashed; directories are tracked
	// as creates and removed only when empty.
	if err := trackScaffoldOutput(tc.Tx, staging.leaf, target.lexical); err != nil {
		return fmt.Errorf("failed to track scaffold output: %w", err)
	}

	return moveScaffoldEntries(staging.leaf, target.lexical, entries)
}

// resolveScaffoldTarget converts the task target into its lexical absolute
// form and its symlink-resolved form, then validates the resolved directory.
func resolveScaffoldTarget(targetPath string) (scaffoldTargetPaths, error) {
	if targetPath == "" {
		targetPath = "."
	}
	lexical, err := filepath.Abs(targetPath)
	if err != nil {
		return scaffoldTargetPaths{}, fmt.Errorf("failed to resolve scaffold target %q: %w", targetPath, err)
	}
	resolved, err := filepath.EvalSymlinks(lexical)
	if err != nil {
		return scaffoldTargetPaths{}, fmt.Errorf("failed to canonicalize scaffold target %q: %w", targetPath, err)
	}
	if err := validateScaffoldTarget(resolved); err != nil {
		return scaffoldTargetPaths{}, err
	}
	return scaffoldTargetPaths{lexical: lexical, resolved: resolved}, nil
}

// validateScaffoldTarget rejects the filesystem root and targets that are not
// accessible directories.
func validateScaffoldTarget(resolved string) error {
	if filepath.Dir(resolved) == resolved {
		return fmt.Errorf("refusing to scaffold into filesystem root %s", resolved)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return fmt.Errorf("scaffold target %s is not accessible: %w", resolved, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("scaffold target %s is not a directory", resolved)
	}
	return nil
}

// prepareStaging creates the empty staging tree under the transaction temp dir.
func (t *ScaffoldTask) prepareStaging(tc types.TaskContext, target scaffoldTargetPaths) (*scaffoldStaging, error) {
	root := tc.Tx.Root()
	txTemp, err := tc.Tx.TempDir()
	if err != nil {
		return nil, fmt.Errorf("failed to prepare transaction staging area: %w", err)
	}
	if _, err := fileutil.SafePath(root, txTemp); err != nil {
		return nil, fmt.Errorf("refusing to stage scaffold outside transaction root: %w", err)
	}

	// Staging lives under the transaction's own temp dir. The journal already
	// exists (TempDir started it), so a crash leaves a tx-* that ListPending
	// and Recover can see; Commit/Rollback remove this directory with the temp
	// dir, so no separate staging prefix or sweep is needed.
	stagingRoot, err := os.MkdirTemp(txTemp, "stage-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create scaffold staging directory: %w", err)
	}
	staging := &scaffoldStaging{root: stagingRoot}

	staging.leaf = filepath.Join(stagingRoot, filepath.Base(target.resolved))
	if err := os.Mkdir(staging.leaf, fileutil.DirPerms); err != nil {
		staging.remove()
		return nil, fmt.Errorf("failed to create scaffold staging leaf %s: %w", staging.leaf, err)
	}
	return staging, nil
}

// remove best-effort deletes the staging tree. It returns no error: after a
// successful move, a cleanup failure must not fail the task and roll back the
// completed project. Commit/Rollback remove the staging tree with the temp dir.
func (s *scaffoldStaging) remove() {
	if err := os.RemoveAll(s.root); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Could not remove scaffold staging %s: %v\n", s.root, err)
	}
}

// runScaffolder executes the generator in the staging leaf and, when a sentinel
// is configured, verifies that it produced the expected regular file. The
// sentinel turns a silent "canceled, wrote nothing" exit into a clear error.
func (t *ScaffoldTask) runScaffolder(tc types.TaskContext, stagingLeaf string) error {
	if err := runCommand(tc.Ctx, t.Cmd, t.Args, stagingLeaf); err != nil {
		return err
	}

	if t.Sentinel == "" {
		return nil
	}
	sentinel := filepath.Join(stagingLeaf, t.Sentinel)
	sentinelInfo, statErr := os.Lstat(sentinel)
	if statErr != nil {
		return fmt.Errorf("scaffold command did not create %s in %s (the scaffolder may have canceled): %w", t.Sentinel, stagingLeaf, statErr)
	}
	if !sentinelInfo.Mode().IsRegular() {
		return fmt.Errorf("scaffold sentinel %s is not a regular file", sentinel)
	}
	return nil
}

// readScaffoldEntries returns the top-level entries the scaffolder produced.
func readScaffoldEntries(stagingLeaf string) ([]os.DirEntry, error) {
	entries, err := os.ReadDir(stagingLeaf)
	if err != nil {
		return nil, fmt.Errorf("failed to read scaffold output %s: %w", stagingLeaf, err)
	}
	return entries, nil
}

// preflightDestinations fails before any tracked state or move if a top-level
// destination already exists.
func preflightDestinations(target string, entries []os.DirEntry) error {
	for _, entry := range entries {
		if err := ensureDestinationAbsent(filepath.Join(target, entry.Name()), entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// ensureDestinationAbsent refuses an existing destination, or reports an
// inspection error that is not simple non-existence.
func ensureDestinationAbsent(dest, entryName string) error {
	if _, statErr := os.Lstat(dest); statErr == nil {
		return fmt.Errorf("refusing to move scaffold entry %q: destination %s already exists", entryName, dest)
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to inspect scaffold destination %s: %w", dest, statErr)
	}
	return nil
}

// trackScaffoldOutput recursively records every staged entry at its eventual
// destination before anything moves.
func trackScaffoldOutput(tx *transaction.Manager, stagingLeaf, target string) error {
	return filepath.WalkDir(stagingLeaf, func(path string, entry os.DirEntry, walkErr error) error {
		return trackScaffoldEntry(tx, stagingLeaf, target, path, entry, walkErr)
	})
}

// trackScaffoldEntry records one staged directory or file. Files are read
// through the symlink-safe helper so the walk cannot follow a planted link out
// of the staging tree.
func trackScaffoldEntry(tx *transaction.Manager, stagingLeaf, target, path string, entry os.DirEntry, walkErr error) error {
	if walkErr != nil {
		return walkErr
	}
	rel, relErr := filepath.Rel(stagingLeaf, path)
	if relErr != nil {
		return relErr
	}
	if rel == "." {
		return nil
	}
	dest := filepath.Join(target, rel)
	if entry.IsDir() {
		return tx.Track(dest)
	}
	if !entry.Type().IsRegular() {
		return fmt.Errorf("unsupported scaffold entry %s: not a regular file or directory", path)
	}
	content, readErr := fileutil.SafeReadFile(stagingLeaf, path)
	if readErr != nil {
		return fmt.Errorf("failed to read scaffold file %s: %w", path, readErr)
	}
	return tx.TrackWrite(dest, content)
}

// moveScaffoldEntries renames each top-level staged entry into the lexical
// target.
func moveScaffoldEntries(stagingLeaf, target string, entries []os.DirEntry) error {
	for _, entry := range entries {
		source := filepath.Join(stagingLeaf, entry.Name())
		dest := filepath.Join(target, entry.Name())

		// Re-check immediately before the rename: a destination created after
		// preflight (for example by an editor or a concurrent checkout) must not
		// be overwritten untracked. No portable no-replace rename exists in the
		// standard library, so a single-writer assumption still bounds the
		// remaining window.
		if err := ensureDestinationAbsent(dest, entry.Name()); err != nil {
			return err
		}

		if err := os.Rename(source, dest); err != nil {
			return fmt.Errorf("failed to move scaffold entry %q into %s: %w", entry.Name(), target, err)
		}
	}
	return nil
}
