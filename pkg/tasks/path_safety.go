// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"fmt"
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
