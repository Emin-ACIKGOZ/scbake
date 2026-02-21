// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"fmt"
	"os"
	"path/filepath"
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
	// Canonicalize the path to ensure consistency across relative/absolute calls.
	absPath, err := filepath.Abs(t.Path)
	if err != nil {
		return fmt.Errorf("failed to resolve directory path %s: %w", t.Path, err)
	}

	// Safety Tracking: If a transaction manager is present, register this path.
	// If the directory doesn't exist, it will be marked for cleanup on rollback.
	// If it does exist, this is a no-op (idempotent).
	if !tc.DryRun && tc.Tx != nil {
		if err := tc.Tx.Track(absPath); err != nil {
			return fmt.Errorf("failed to track directory %s: %w", t.Path, err)
		}
	}

	// Use the constant from the centralized location for secure permissions.
	if err := os.MkdirAll(absPath, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", t.Path, err)
	}
	return nil
}

// Description returns a human-readable summary of the task.
func (t *CreateDirTask) Description() string { return t.Desc }

// Priority returns the execution priority level.
func (t *CreateDirTask) Priority() int { return t.TaskPrio }
