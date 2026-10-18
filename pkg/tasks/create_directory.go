// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"fmt"
	"os"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// CreateDirTask ensures a directory exists.
type CreateDirTask struct {
	Path     string
	Desc     string
	TaskPrio int
}

// Execute performs the task of creating the directory.
func (t *CreateDirTask) Execute(tc types.TaskContext) error {
	// Resolve the path against the target directory and verify it stays inside
	// it. Relative paths are joined to the target; absolute paths must already
	// lie within the target.
	absPath, err := resolveTaskPath(tc.TargetPath, t.Path, "directory")
	if err != nil {
		return err
	}

	// Safety Tracking: If a transaction manager is present, register this path.
	// If the directory doesn't exist, it will be marked for cleanup on rollback.
	// If it does exist, this is a no-op (idempotent).
	if !tc.DryRun && tc.Tx != nil {
		if err := tc.Tx.Track(absPath); err != nil {
			return fmt.Errorf("failed to track directory %s: %w", t.Path, err)
		}
	}

	// Register any intermediate directories that MkdirAll is about to create
	// implicitly so that rollback removes them as well.
	if err := trackMissingAncestors(tc, absPath); err != nil {
		return err
	}

	// Use the constant from the centralized location for secure permissions.
	if err := os.MkdirAll(absPath, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", t.Path, err)
	}
	return nil
}

// Description implements types.Task.
func (t *CreateDirTask) Description() string { return t.Desc }

// Priority implements types.Task.
func (t *CreateDirTask) Priority() int { return t.TaskPrio }
