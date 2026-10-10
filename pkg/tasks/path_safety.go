// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
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

// trackMissingAncestors records the not-yet-existing ancestor directories of
// absPath with the transaction manager.
//
// os.MkdirAll creates every missing directory in a path, but only the leaf was
// previously tracked, so rollback left the implicitly-created parents behind.
// This helper walks up from the parent of absPath, collecting each ancestor
// that does not exist yet and stopping at the first existing ancestor.
//
// The walk is bounded by the transaction's project root (falling back to
// tc.TargetPath without a manager). Bounding by tc.TargetPath alone would miss
// the case where the target directory itself does not exist yet, e.g.
// `apply <subproject>` in a monorepo: there the root is the existing manifest
// root and the not-yet-existing target lies below it.
//
// Missing ancestors are registered shallowest-first; Manager.Rollback iterates
// in reverse, and because it removes recursively the whole chain is cleaned up.
//
// It is a no-op when transactions are disabled or in dry-run mode.
func trackMissingAncestors(tc types.TaskContext, absPath string) error {
	if tc.Tx == nil || tc.DryRun {
		return nil
	}

	root := filepath.Clean(tc.TargetPath)
	if txRoot := tc.Tx.Root(); txRoot != "" {
		root = filepath.Clean(txRoot)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("failed to resolve target root %s: %w", root, err)
	}

	var missing []string
	for dir := filepath.Dir(absPath); isStrictlyWithin(absRoot, dir); dir = filepath.Dir(dir) {
		if _, statErr := os.Stat(dir); statErr == nil {
			break // First existing ancestor: everything above it already exists.
		} else if !os.IsNotExist(statErr) {
			return fmt.Errorf("failed to inspect ancestor directory %s: %w", dir, statErr)
		}
		missing = append(missing, dir)
	}

	// Register shallowest-first; Manager.Rollback iterates in reverse so the
	// deepest implicitly-created directory is removed first.
	for i := len(missing) - 1; i >= 0; i-- {
		if err := tc.Tx.Track(missing[i]); err != nil {
			return fmt.Errorf("failed to track ancestor directory %s: %w", missing[i], err)
		}
	}
	return nil
}

// isStrictlyWithin reports whether path is a strict descendant of root. The
// root itself and anything outside it are rejected.
func isStrictlyWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
