// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"fmt"
	"os"
	"scbake/internal/util/fileutil"
)

// Rollback undoes all tracked changes.
// 1. Deletes created files/directories (LIFO order).
// 2. Restores backed-up files (overwriting current state).
// 3. Deletes the temp directory and structural scaffolding.
//
// After a successful Commit the rollback is disabled: the transaction's
// changes are permanent and Rollback is a no-op.
func (m *Manager) Rollback() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.committed {
		return nil
	}

	errs := m.deleteCreatedPaths()
	errs = append(errs, m.restoreBackups()...)
	errs = append(errs, m.cleanupTempDir()...)

	if len(errs) > 0 {
		return fmt.Errorf("rollback completed with errors: %v", errs)
	}

	return nil
}

// deleteCreatedPaths deletes created paths in REVERSE order (deepest first),
// re-validating containment (SCB-001/SCB-002) before each delete. A created
// entry must still resolve inside the root: a symlink swapped in after tracking
// must never redirect RemoveAll outside the project. It returns the accumulated
// errors instead of stopping at the first failure.
func (m *Manager) deleteCreatedPaths() []error {
	var errs []error
	for i := len(m.created) - 1; i >= 0; i-- {
		path := m.created[i]

		if _, err := fileutil.SafePath(m.rootPath, path); err != nil {
			errs = append(errs, fmt.Errorf("refusing to delete created path %s: %w", path, err))
			continue
		}

		// We use RemoveAll for safety, but Remove would suffice if strict LIFO is met.
		// Lstat is used so a symlink is never followed: RemoveAll removes the
		// link itself rather than its target.
		if _, err := os.Lstat(path); err == nil {
			if err := os.RemoveAll(path); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete created path %s: %w", path, err))
			}
		}
	}
	return errs
}

// restoreBackups restores every backed-up file, re-validating containment first
// so a path swapped for an escaping symlink after tracking cannot redirect the
// write. The current content at each original path is overwritten.
func (m *Manager) restoreBackups() []error {
	var errs []error
	for originalPath, backup := range m.backups {
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
