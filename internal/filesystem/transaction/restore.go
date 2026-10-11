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

	var errs []error

	// 1. Delete created paths in REVERSE order (Deepest first)
	// This ensures that if we created 'dir/subdir/file', we delete 'file', then 'subdir', then 'dir'.
	for i := len(m.created) - 1; i >= 0; i-- {
		path := m.created[i]

		// Re-validate containment (SCB-001/SCB-002) before deleting. A created
		// entry must still resolve inside the root: a symlink swapped in after
		// tracking must never redirect RemoveAll outside the project.
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

	// 2. Restore backed up files
	for originalPath, backup := range m.backups {
		// Re-validate containment before restoring so a path swapped for an
		// escaping symlink after tracking cannot redirect the write.
		if _, err := fileutil.SafePath(m.rootPath, originalPath); err != nil {
			errs = append(errs, fmt.Errorf("refusing to restore %s: %w", originalPath, err))
			continue
		}

		// Restore content
		// We purposefully overwrite whatever is currently at originalPath
		if err := copyFile(backup.tempPath, originalPath, backup.mode); err != nil {
			errs = append(errs, fmt.Errorf("failed to restore %s: %w", originalPath, err))
		}
	}

	// 3. Cleanup temp dir and structural scaffolding
	if m.tempDir != "" {
		if err := os.RemoveAll(m.tempDir); err != nil {
			errs = append(errs, fmt.Errorf("failed to remove temp dir: %w", err))
		}
		m.cleanupStructure()
		m.resetState()
	}

	if len(errs) > 0 {
		return fmt.Errorf("rollback completed with errors: %v", errs)
	}

	return nil
}
