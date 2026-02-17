// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package transaction

import (
	"fmt"
	"os"
)

// Rollback undoes all tracked changes.
// 1. Deletes created files/directories (LIFO order).
// 2. Restores backed-up files (overwriting current state).
// 3. Deletes the temp directory and structural scaffolding.
func (m *Manager) Rollback() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error

	// 1. Delete created paths in REVERSE order (Deepest first)
	// This ensures that if we created 'dir/subdir/file', we delete 'file', then 'subdir', then 'dir'.
	for i := len(m.created) - 1; i >= 0; i-- {
		path := m.created[i]

		// We use RemoveAll for safety, but Remove would suffice if strict LIFO is met.
		// Check existence first to avoid errors if a task failed before creation.
		if _, err := os.Stat(path); err == nil {
			if err := os.RemoveAll(path); err != nil {
				errs = append(errs, fmt.Errorf("failed to delete created path %s: %w", path, err))
			}
		}
	}

	// 2. Restore backed up files
	for originalPath, backup := range m.backups {
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
