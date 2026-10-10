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

// AppendFileTask appends content to a file if it doesn't already exist.
type AppendFileTask struct {
	FilePath string
	Content  string
	Desc     string
	TaskPrio int
}

// Execute performs the task of appending content to the file.
func (t *AppendFileTask) Execute(tc types.TaskContext) error {
	// Security: reject absolute or escaping paths before any filesystem access,
	// independent of dry-run or transaction state.
	if !filepath.IsLocal(t.FilePath) {
		return fmt.Errorf("invalid file path %q: must be a relative path inside the target directory", t.FilePath)
	}

	absPath, err := resolveTaskPath(tc.TargetPath, t.FilePath, "file")
	if err != nil {
		return err
	}

	if err := trackTaskFile(tc, absPath, t.FilePath); err != nil {
		return err
	}

	// Check if file exists
	existingContent, err := fileutil.SafeReadFile(tc.TargetPath, t.FilePath)
	if err != nil {
		if os.IsNotExist(err) {
			// If file doesn't exist, create it with the content
			if tc.DryRun {
				return nil
			}
			return fileutil.SafeWriteFile(tc.TargetPath, t.FilePath, []byte(t.Content), fileutil.FilePerms)
		}
		return fmt.Errorf("failed to read file %s: %w", t.FilePath, err)
	}

	// Idempotency check: Don't append if content is already present
	if strings.Contains(string(existingContent), strings.TrimSpace(t.Content)) || tc.DryRun {
		return nil
	}

	return appendFileContent(tc.TargetPath, t.FilePath, existingContent, t.Content)
}

// appendFileContent opens a file in targetPath for appending, ensures proper trailing
// newline padding, writes newContent, and safely closes the file.
func appendFileContent(targetPath, relPath string, existing []byte, newContent string) (err error) {
	f, err := fileutil.SafeOpenFile(targetPath, relPath, os.O_APPEND|os.O_WRONLY, fileutil.FilePerms)
	if err != nil {
		return fmt.Errorf("failed to open file %s for appending: %w", relPath, err)
	}

	// Ensure f.Close() is checked
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	// Ensure there's a newline before appending if the file doesn't end with one
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		if _, err = f.WriteString("\n"); err != nil {
			return fmt.Errorf("failed to write newline to %s: %w", relPath, err)
		}
	}

	if _, err = f.WriteString(newContent); err != nil {
		return fmt.Errorf("failed to append to %s: %w", relPath, err)
	}

	return nil
}

// Description returns a human-readable summary of the task.
func (t *AppendFileTask) Description() string { return t.Desc }

// Priority returns the execution priority level.
func (t *AppendFileTask) Priority() int { return t.TaskPrio }
