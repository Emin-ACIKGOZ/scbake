// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package fileutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SafePath resolves targetPath against baseDir and verifies that the resulting
// absolute path remains strictly contained inside baseDir.
//
// Relative target paths are joined to baseDir; absolute target paths are checked
// directly against baseDir containment.
func SafePath(baseDir, targetPath string) (string, error) {
	if baseDir == "" {
		return "", errors.New("base directory cannot be empty")
	}

	baseAbs, err := filepath.Abs(baseDir)
	if err != nil {
		return "", fmt.Errorf("invalid base directory %q: %w", baseDir, err)
	}
	baseAbs = filepath.Clean(baseAbs)

	absPath := filepath.Clean(targetPath)
	if !filepath.IsAbs(targetPath) {
		absPath = filepath.Clean(filepath.Join(baseAbs, targetPath))
	}

	rel, err := filepath.Rel(baseAbs, absPath)
	if err != nil {
		return "", fmt.Errorf("failed to compute relative path: %w", err)
	}

	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path traversal rejected: %q escapes base directory %q", targetPath, baseDir)
	}

	return absPath, nil
}

// SafeReadFile validates targetPath containment inside baseDir then reads the file.
func SafeReadFile(baseDir, targetPath string) ([]byte, error) {
	safePath, err := SafePath(baseDir, targetPath)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // Path containment verified via SafePath above
	return os.ReadFile(safePath)
}

// SafeOpenFile validates targetPath containment inside baseDir then opens the file.
func SafeOpenFile(baseDir, targetPath string, flag int, perm os.FileMode) (*os.File, error) {
	safePath, err := SafePath(baseDir, targetPath)
	if err != nil {
		return nil, err
	}
	//nolint:gosec // Path containment verified via SafePath above
	return os.OpenFile(safePath, flag, perm)
}

// SafeWriteFile validates targetPath containment inside baseDir then writes the file.
func SafeWriteFile(baseDir, targetPath string, data []byte, perm os.FileMode) error {
	safePath, err := SafePath(baseDir, targetPath)
	if err != nil {
		return err
	}
	return os.WriteFile(safePath, data, perm)
}
