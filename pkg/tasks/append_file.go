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

	// Idempotency check: Don't append if content is already present as its own
	// standalone, line-aligned block. A plain substring match is not enough:
	// content that merely occurs inside unrelated text must still be appended.
	if contentAlreadyPresent(string(existingContent), t.Content) || tc.DryRun {
		return nil
	}

	return appendFileContent(tc.TargetPath, t.FilePath, existingContent, t.Content)
}

// contentAlreadyPresent reports whether content already exists inside existing as
// a standalone, line-aligned block. Each line is trimmed before comparison so
// that indentation-only differences are ignored, and the trimmed content lines
// must appear as a contiguous run of trimmed existing lines.
//
// Whitespace-only (or empty) content is treated as already present to preserve
// the historic no-op behavior.
func contentAlreadyPresent(existing, content string) bool {
	if strings.TrimSpace(content) == "" {
		return true
	}

	contentLines := trimmedLines(content)
	existingLines := trimmedLines(existing)
	if len(contentLines) > len(existingLines) {
		return false
	}

	for i := 0; i+len(contentLines) <= len(existingLines); i++ {
		matched := true
		for j, line := range contentLines {
			if existingLines[i+j] != line {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

// trimmedLines splits s into lines and trims surrounding whitespace from each
// line. A trailing newline does not produce a trailing empty element, so a block
// written with or without a final newline is considered identical.
func trimmedLines(s string) []string {
	if s == "" {
		return nil
	}

	lines := strings.Split(s, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines
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

// Description implements types.Task.
func (t *AppendFileTask) Description() string { return t.Desc }

// Priority implements types.Task.
func (t *AppendFileTask) Priority() int { return t.TaskPrio }
