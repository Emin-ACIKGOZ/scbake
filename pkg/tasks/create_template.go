// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

// Package tasks defines the executable units of work used in a scaffolding plan.
package tasks

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
	"text/template"
)

// CreateTemplateTask renders and writes a file from an embedded template.
type CreateTemplateTask struct {
	// TemplateFS is the embedded filesystem (e.g., lang.GoTemplates)
	TemplateFS embed.FS

	// TemplatePath is the path *within* the embed.FS (e.g., "go.mod.tpl")
	TemplatePath string

	// OutputPath is the destination path relative to the TargetPath (e.g., "go.mod")
	OutputPath string

	// Human-readable description
	Desc string

	// Execution priority
	TaskPrio int
}

// Description returns a human-readable summary of the task.
func (t *CreateTemplateTask) Description() string {
	return t.Desc
}

// Priority returns the execution priority level.
func (t *CreateTemplateTask) Priority() int {
	return t.TaskPrio
}

// checkFilePreconditions handles path safety, directory creation, and existence checks.
func checkFilePreconditions(finalPath, output, target string, force bool) error {
	// 1. Path Safety Check (Canonicalization)
	// We resolve both target and finalPath to absolute cleaned paths to ensure
	// that indicators like "." and relative subdirectories match correctly.
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("failed to resolve target path: %w", err)
	}
	absFinal, err := filepath.Abs(finalPath)
	if err != nil {
		return fmt.Errorf("failed to resolve output path: %w", err)
	}

	cleanTarget := filepath.Clean(absTarget)
	cleanFinalPath := filepath.Clean(absFinal)

	if !strings.HasPrefix(cleanFinalPath, cleanTarget) {
		return fmt.Errorf("task failed (%s): output path '%s' is outside the target path '%s'",
			filepath.Base(output), output, target)
	}

	// 2. Ensure the directory exists
	dir := filepath.Dir(cleanFinalPath)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 3. Existence Check
	if !force {
		if _, err := os.Stat(cleanFinalPath); err == nil {
			// File exists and Force is false.
			return fmt.Errorf("file already exists: %s. Use --force to overwrite", output)
		} else if !errors.Is(err, os.ErrNotExist) {
			// Some other error occurred (e.g., permissions).
			return err
		}
	}
	return nil
}

// Execute performs the template creation task.
func (t *CreateTemplateTask) Execute(tc types.TaskContext) (err error) {
	if tc.DryRun {
		return nil
	}

	// 1. Read and parse the template
	tplContent, err := fs.ReadFile(t.TemplateFS, t.TemplatePath)
	if err != nil {
		return fmt.Errorf("failed to read embedded template %s: %w", t.TemplatePath, err)
	}

	tpl, err := template.New(t.TemplatePath).Parse(string(tplContent))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", t.TemplatePath, err)
	}

	// 2. Determine and check the final output path
	finalPath := filepath.Join(tc.TargetPath, t.OutputPath)

	// Check directory and file existence using the helper function.
	if err = checkFilePreconditions(finalPath, t.OutputPath, tc.TargetPath, tc.Force); err != nil {
		return err
	}

	// Canonical path for tracking and writing
	absPath, _ := filepath.Abs(finalPath)

	// 3. Safety Tracking: Register the file with the transaction manager.
	// If it exists, it will be backed up. If not, it will be marked for cleanup.
	if tc.Tx != nil {
		if err := tc.Tx.Track(absPath); err != nil {
			return fmt.Errorf("failed to track file %s: %w", t.OutputPath, err)
		}
	}

	// 4. Create the output file
	// G304: Path is explicitly sanitized and verified in checkFilePreconditions
	//nolint:gosec
	f, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fileutil.FilePerms)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", t.OutputPath, err)
	}

	// Check the return value of f.Close()
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	// 5. Execute the template and write to the file
	if err = tpl.Execute(f, tc.Manifest); err != nil {
		return fmt.Errorf("failed to render template %s: %w", t.TemplatePath, err)
	}

	return nil
}
