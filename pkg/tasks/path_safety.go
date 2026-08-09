// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"fmt"
	"path/filepath"
	"strings"
)

// resolveTaskPath resolves a task path field against the target directory and
// verifies the resolved path stays inside it.
//
// Relative paths are joined to the target directory; absolute paths are
// accepted as-is but must still lie within the target. The returned path is
// cleaned and absolute.
func resolveTaskPath(target, field, what string) (string, error) {
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("failed to resolve target path %s: %w", target, err)
	}
	targetAbs = filepath.Clean(targetAbs)

	absPath := filepath.Clean(field)
	if !filepath.IsAbs(field) {
		absPath = filepath.Clean(filepath.Join(targetAbs, field))
	}

	rel, err := filepath.Rel(targetAbs, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s path %q: %w", what, field, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s path %q is outside the target directory", what, field)
	}

	return absPath, nil
}
