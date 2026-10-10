// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"fmt"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// resolveTaskPath resolves a task path field against the target directory and
// verifies the resolved path stays inside it.
//
// Relative paths are joined to the target directory; absolute paths are
// accepted as-is but must still lie within the target. The returned path is
// cleaned and absolute.
func resolveTaskPath(target, field, what string) (string, error) {
	absPath, err := fileutil.SafePath(target, field)
	if err != nil {
		return "", fmt.Errorf("%s path %q is outside the target directory: %w", what, field, err)
	}
	return absPath, nil
}

// trackTaskFile registers the path with the transaction manager if transactions are active and not in dry-run mode.
func trackTaskFile(tc types.TaskContext, absPath, relPath string) error {
	if tc.DryRun || tc.Tx == nil {
		return nil
	}
	if err := tc.Tx.Track(absPath); err != nil {
		return fmt.Errorf("failed to track file %s: %w", relPath, err)
	}
	return nil
}
