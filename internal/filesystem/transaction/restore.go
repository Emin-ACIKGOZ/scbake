// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"sort"
	"strings"
)

// Rollback undoes all tracked changes.
// 1. Deletes verified created files and empty created directories deepest-first.
// 2. Restores backed-up files (overwriting current state).
// 3. Deletes the temp directory and structural scaffolding.
//
// Once Commit publishes the commit marker, rollback is disabled: the transaction's
// changes are permanent and Rollback is a no-op, including when Commit returns
// ErrCommitted.
func (m *Manager) Rollback() error {
	return m.RollbackContext(context.Background())
}

// RollbackContext is Rollback with cancellation support. It checks the context
// before the rollback, between created-path and backup entries, and before the
// final temp-directory cleanup. On cancellation it leaves the journal and
// remaining backups in place so recovery can be retried.
func (m *Manager) RollbackContext(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.rollback(ctx, true)
}

// rollbackChanges performs the same file restoration as RollbackContext but
// leaves the transaction directory and journal in place. Recovery uses it when
// removing a bootstrap root must happen before the retry record is discarded.
func (m *Manager) rollbackChanges(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.rollback(ctx, false)
}

// rollback restores tracked state, optionally removing the temp directory and
// journal once every mutation succeeded.
func (m *Manager) rollback(ctx context.Context, cleanup bool) error {
	if m.committed {
		return nil
	}

	errs := m.deleteCreatedPaths(ctx)
	ctxErr := ctx.Err()
	if ctxErr == nil {
		errs = append(errs, m.restoreBackups(ctx)...)
		ctxErr = ctx.Err()
		if ctxErr == nil && len(errs) == 0 && cleanup {
			errs = append(errs, m.cleanupTempDir()...)
			ctxErr = ctx.Err()
		}
	}
	// On failure, keep the journal and backups: the directory is the only way
	// to retry recovery after the user fixes the cause.

	if ctxErr != nil {
		if len(errs) > 0 {
			return fmt.Errorf("rollback canceled with errors: %v: %w", errs, ctxErr)
		}
		return fmt.Errorf("rollback canceled: %w", ctxErr)
	}
	if len(errs) > 0 {
		return fmt.Errorf("rollback completed with errors: %v", errs)
	}

	return nil
}

// deleteCreatedPaths removes created paths under the rollback safety rule: a
// path is deleted only when its identity is verified. Paths are processed
// deepest-first (independently of tracking order) so empty created directories
// can be removed after their contents. Containment (SCB-001/SCB-002) is
// revalidated before each operation and a violation remains a hard error.
//
// A created regular file is removed only when a recorded raw SHA-256 matches
// its current bytes; a missing hash, a mismatch, or a read failure keeps it and
// prints a non-fatal warning. A created directory with no hash is removed only
// when empty (never with RemoveAll); a recorded file hash at a directory keeps
// it because a file hash must not authorize removing a replaced directory.
// Warnings are deliberately not returned as errors: keeping residue must not
// retain the journal or make recovery retry forever.
func (m *Manager) deleteCreatedPaths(ctx context.Context) []error {
	paths := m.createdPathsDeepestFirst()

	var errs []error
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return errs
		}
		if err := m.deleteCreatedPath(path); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// createdPathsDeepestFirst returns a copy of the created paths ordered by path
// depth descending, so children are handled before their parents.
func (m *Manager) createdPathsDeepestFirst() []string {
	paths := append([]string(nil), m.created...)
	sort.SliceStable(paths, func(i, j int) bool {
		return pathDepth(paths[i]) > pathDepth(paths[j])
	})
	return paths
}

// deleteCreatedPath removes one created path if its identity is verified.
// Returning nil covers both removal and a deliberate keep; only containment and
// I/O failures are reported as errors.
func (m *Manager) deleteCreatedPath(path string) error {
	if err := m.validateCreatedPath(path); err != nil {
		return err
	}

	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to inspect created path %s: %w", path, err)
	}

	hash := m.createdHashes[path]
	switch {
	case info.IsDir():
		return m.deleteCreatedDirectory(path, hash)
	case info.Mode().IsRegular():
		return m.deleteCreatedFile(path, hash)
	default:
		// Symlinks and other special files are never followed or deleted:
		// a recorded hash describes a regular file, not an arbitrary path.
		warnKeptCreatedPath(path, "no longer a regular file")
		return nil
	}
}

// validateCreatedPath revalidates containment before any rollback operation.
func (m *Manager) validateCreatedPath(path string) error {
	if _, err := fileutil.SafePath(m.rootPath, path); err != nil {
		return fmt.Errorf("refusing to delete created path %s: %w", path, err)
	}
	return nil
}

// deleteCreatedDirectory removes a created directory only when it has no
// recorded file hash and is empty.
func (m *Manager) deleteCreatedDirectory(path, hash string) error {
	if hash != "" {
		warnKeptCreatedPath(path, "created file was replaced by a directory")
		return nil
	}
	return m.removeCreatedDir(path)
}

// deleteCreatedFile removes a created regular file only when its current raw
// bytes match the recorded hash.
func (m *Manager) deleteCreatedFile(path, hash string) error {
	if hash == "" {
		warnKeptCreatedPath(path, "no recorded content hash")
		return nil
	}
	currentHash, err := hashFile(m.rootPath, path)
	if err != nil {
		warnKeptCreatedPath(path, fmt.Sprintf("could not verify content: %v", err))
		return nil
	}
	if currentHash != hash {
		warnKeptCreatedPath(path, "content changed since this transaction wrote it")
		return nil
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete created path %s: %w", path, err)
	}
	return nil
}

// removeCreatedDir removes an unchanged created directory only when it is
// empty. A directory with external children is kept with a warning; any other
// removal failure is a real error.
func (m *Manager) removeCreatedDir(path string) error {
	err := os.Remove(path)
	if err == nil {
		return nil
	}
	entries, readErr := os.ReadDir(path)
	if readErr == nil && len(entries) > 0 {
		warnKeptCreatedPath(path, "created directory is not empty")
		return nil
	}
	return fmt.Errorf("failed to delete created path %s: %w", path, err)
}

// warnKeptCreatedPath reports a path rollback deliberately left in place.
func warnKeptCreatedPath(path, reason string) {
	fmt.Fprintf(os.Stderr, "⚠️  Keeping created path %s: %s.\n", path, reason)
}

// pathDepth returns the number of path components below the filesystem root.
// It is used only to order rollback removal deepest-first.
func pathDepth(path string) int {
	return strings.Count(filepath.Clean(path), string(filepath.Separator))
}

// restoreBackups restores every backed-up file, re-validating containment first
// so a path swapped for an escaping symlink after tracking cannot redirect the
// write. The current content at each original path is overwritten.
func (m *Manager) restoreBackups(ctx context.Context) []error {
	var errs []error
	for originalPath, backup := range m.backups {
		if err := ctx.Err(); err != nil {
			return errs
		}
		if _, err := fileutil.SafePath(m.rootPath, originalPath); err != nil {
			errs = append(errs, fmt.Errorf("refusing to restore %s: %w", originalPath, err))
			continue
		}

		if err := copyFile(backup.tempPath, originalPath, backup.mode); err != nil {
			errs = append(errs, fmt.Errorf("failed to restore %s: %w", originalPath, err))
		}
	}
	return errs
}

// cleanupTempDir removes the transaction's temp directory and structural
// scaffolding, then resets the manager state. It is a no-op when no temp
// directory was ever created.
func (m *Manager) cleanupTempDir() []error {
	if m.tempDir == "" {
		return nil
	}
	var errs []error
	if err := os.RemoveAll(m.tempDir); err != nil {
		errs = append(errs, fmt.Errorf("failed to remove temp dir: %w", err))
	}
	m.cleanupStructure()
	m.resetState()
	return errs
}
