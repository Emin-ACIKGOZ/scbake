// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
)

// AppendFileTask appends content to a file if it doesn't already exist.
type AppendFileTask struct {
	FilePath string
	Content  string
	Desc     string
	TaskPrio int
}

// Execute performs the task of appending content to the file.
//nolint:cyclop // Complex file operations require linear error checks
func (t *AppendFileTask) Execute(tc types.TaskContext) error {
	absPath, err := filepath.Abs(filepath.Join(tc.TargetPath, t.FilePath))
	if err != nil {
		return fmt.Errorf("failed to resolve path %s: %w", t.FilePath, err)
	}

	// Safety Tracking: Register the path for transaction management.
	if !tc.DryRun && tc.Tx != nil {
		if err := tc.Tx.Track(absPath); err != nil {
			return fmt.Errorf("failed to track file %s: %w", t.FilePath, err)
		}
	}

	// Check if file exists
	//nolint:gosec // Path is validated by filepath.Abs and tracked by tx manager
	existingContent, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// If file doesn't exist, create it with the content
			if tc.DryRun {
				return nil
			}
			return os.WriteFile(absPath, []byte(t.Content), fileutil.FilePerms)
		}
		return fmt.Errorf("failed to read file %s: %w", t.FilePath, err)
	}

	// Idempotency check: Don't append if content is already present
	if strings.Contains(string(existingContent), strings.TrimSpace(t.Content)) {
		return nil
	}

	if tc.DryRun {
		return nil
	}

	// Append content
	//nolint:gosec // Path is validated by filepath.Abs and tracked by tx manager
	f, err := os.OpenFile(absPath, os.O_APPEND|os.O_WRONLY, fileutil.FilePerms)
	if err != nil {
		return fmt.Errorf("failed to open file %s for appending: %w", t.FilePath, err)
	}

	// Ensure f.Close() is checked
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	// Ensure there's a newline before appending if the file doesn't end with one
	if len(existingContent) > 0 && existingContent[len(existingContent)-1] != '\n' {
		if _, err = f.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write newline to %s: %w", t.FilePath, err)
		}
	}

	if _, err = f.WriteString(t.Content); err != nil {
		return fmt.Errorf("failed to append to %s: %w", t.FilePath, err)
	}

	return nil
}

// Description returns a human-readable summary of the task.
func (t *AppendFileTask) Description() string { return t.Desc }

// Priority returns the execution priority level.
func (t *AppendFileTask) Priority() int { return t.TaskPrio }
