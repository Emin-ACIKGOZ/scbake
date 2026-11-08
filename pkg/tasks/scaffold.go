// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// ScaffoldTask runs an external project generator in a fresh empty staging
// directory and moves its top-level output into the target. It exists for
// scaffolders such as create-vite that refuse a non-empty target: the target
// already contains scbake.toml and .scbake, but the staging leaf is empty.
//
// The staging leaf is named after the canonical target basename so generators
// that derive a package/project name from the cwd's basename keep the name the
// direct invocation would have produced.
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

// Description implements types.Task.
func (t *ScaffoldTask) Description() string { return t.Desc }

// Priority implements types.Task.
func (t *ScaffoldTask) Priority() int { return t.TaskPrio }

// Execute stages the scaffold, validates it, tracks every destination, and
// moves the top-level entries into the target. A handled failure leaves the
// target untouched: nothing is moved until every destination is tracked.
func (t *ScaffoldTask) Execute(tc types.TaskContext) error {
	if tc.DryRun {
		// Dry-run prints the task description and performs no filesystem writes.
		return nil
	}
	if tc.Tx == nil {
		return errors.New("scaffold task requires an active transaction")
	}

	target, err := canonicalTarget(tc.TargetPath)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("scaffold target %s is not accessible: %w", target, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("scaffold target %s is not a directory", target)
	}

	root := tc.Tx.Root()
	txTemp, err := tc.Tx.TempDir()
	if err != nil {
		return fmt.Errorf("failed to prepare transaction staging area: %w", err)
	}
	if _, err := fileutil.SafePath(root, txTemp); err != nil {
		return fmt.Errorf("refusing to stage scaffold outside transaction root: %w", err)
	}

	// Staging lives under the transaction's own temp dir. The journal already
	// exists (TempDir started it), so a crash leaves a tx-* that ListPending
	// and Recover can see; Commit/Rollback remove this directory with the temp
	// dir, so no separate staging prefix or sweep is needed.
	stagingRoot, err := os.MkdirTemp(txTemp, "stage-*")
	if err != nil {
		return fmt.Errorf("failed to create scaffold staging directory: %w", err)
	}
	defer func() {
		// Staging is inside the transaction temp dir, so Commit/Rollback remove
		// it even if this best-effort cleanup fails. A failure here must never
		// become a task error: after a successful move that would roll back a
		// completed project.
		if rmErr := os.RemoveAll(stagingRoot); rmErr != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Could not remove scaffold staging %s: %v\n", stagingRoot, rmErr)
		}
	}()

	stagingLeaf := filepath.Join(stagingRoot, filepath.Base(target))
	if err := os.Mkdir(stagingLeaf, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create scaffold staging leaf %s: %w", stagingLeaf, err)
	}

	//nolint:gosec // Command and args are defined by the language handler.
	cmd := exec.CommandContext(tc.Ctx, t.Cmd, t.Args...)
	cmd.Dir = stagingLeaf
	cmd.WaitDelay = commandWaitDelay
	cmd.Env = scrubRegistryTokens(cmd.Environ())

	var stderr limitedBuffer
	cmd.Stderr = &stderr
	if runErr := cmd.Run(); runErr != nil {
		return commandExecutionError(tc.Ctx, t.Cmd, t.Args, runErr, &stderr)
	}

	if t.Sentinel != "" {
		sentinel := filepath.Join(stagingLeaf, t.Sentinel)
		sentinelInfo, statErr := os.Lstat(sentinel)
		if statErr != nil {
			return fmt.Errorf("scaffold command did not create %s in %s (the scaffolder may have cancelled): %w", t.Sentinel, stagingLeaf, statErr)
		}
		if !sentinelInfo.Mode().IsRegular() {
			return fmt.Errorf("scaffold sentinel %s is not a regular file", sentinel)
		}
	}

	entries, err := os.ReadDir(stagingLeaf)
	if err != nil {
		return fmt.Errorf("failed to read scaffold output %s: %w", stagingLeaf, err)
	}

	// Preflight every top-level destination before tracking or moving anything
	// so a collision cannot leave tracked state or a partially moved project.
	for _, entry := range entries {
		dest := filepath.Join(target, entry.Name())
		if _, statErr := os.Lstat(dest); statErr == nil {
			return fmt.Errorf("refusing to move scaffold entry %q: destination %s already exists", entry.Name(), dest)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("failed to inspect scaffold destination %s: %w", dest, statErr)
		}
	}

	// Track destinations before the move so rollback can remove exactly the
	// bytes this transaction moved. Files are hashed; directories are tracked
	// as creates and removed only when empty.
	err = filepath.WalkDir(stagingLeaf, func(path string, entry os.DirEntry, walkErr error) error {
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
			return tc.Tx.Track(dest)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported scaffold entry %s: not a regular file or directory", path)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return fmt.Errorf("failed to read scaffold file %s: %w", path, readErr)
		}
		return tc.Tx.TrackWrite(dest, content)
	})
	if err != nil {
		return fmt.Errorf("failed to track scaffold output: %w", err)
	}

	for _, entry := range entries {
		source := filepath.Join(stagingLeaf, entry.Name())
		dest := filepath.Join(target, entry.Name())

		// Re-check immediately before the rename: a destination created after
		// preflight (for example by an editor or a concurrent checkout) must not
		// be overwritten untracked. No portable no-replace rename exists in the
		// standard library, so a single-writer assumption still bounds the
		// remaining window.
		if _, statErr := os.Lstat(dest); statErr == nil {
			return fmt.Errorf("refusing to move scaffold entry %q: destination %s already exists", entry.Name(), dest)
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("failed to inspect scaffold destination %s: %w", dest, statErr)
		}

		if err := os.Rename(source, dest); err != nil {
			return fmt.Errorf("failed to move scaffold entry %q into %s: %w", entry.Name(), target, err)
		}
	}
	return nil
}

// canonicalTarget resolves the target to an absolute, symlink-free directory
// so the staging leaf basename matches what a direct invocation would see.
func canonicalTarget(targetPath string) (string, error) {
	if targetPath == "" {
		targetPath = "."
	}
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve scaffold target %q: %w", targetPath, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize scaffold target %q: %w", targetPath, err)
	}
	if filepath.Dir(resolved) == resolved {
		return "", fmt.Errorf("refusing to scaffold into filesystem root %s", resolved)
	}
	return resolved, nil
}
