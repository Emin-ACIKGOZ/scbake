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
	"scbake/internal/util"
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

// Execute performs the template creation task.
func (t *CreateTemplateTask) Execute(tc types.TaskContext) (err error) {
	if tc.DryRun {
		// In dry-run, log what we *would* have done.
		return nil
	}

	// 1. Read the embedded template file
	tplContent, err := fs.ReadFile(t.TemplateFS, t.TemplatePath)
	if err != nil {
		return fmt.Errorf("failed to read embedded template %s: %w", t.TemplatePath, err)
	}

	// 2. Parse the template
	tpl, err := template.New(t.TemplatePath).Parse(string(tplContent))
	if err != nil {
		return fmt.Errorf("failed to parse template %s: %w", t.TemplatePath, err)
	}

	// 3. Determine the final output path
	finalPath := filepath.Join(tc.TargetPath, filepath.Clean(t.OutputPath))
	if !strings.HasPrefix(finalPath, tc.TargetPath) {
		return fmt.Errorf("output path '%s' is outside the target path '%s'", t.OutputPath, tc.TargetPath)
	}

	// 4. Ensure the directory exists
	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, util.DirPerms); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 5. Check if the file exists *before* creating it.
	if !tc.Force {
		if _, err := os.Stat(finalPath); err == nil {
			// File exists and Force is false.
			return fmt.Errorf("file already exists: %s. Use --force to overwrite", finalPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			// Some other error occurred (e.g., permissions).
			return err
		}
	}

	// 6. Create the output file
	f, err := os.Create(finalPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", finalPath, err)
	}

	// Check the return value of f.Close()
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	// 7. Execute the template and write to the file
	if err := tpl.Execute(f, tc.Manifest); err != nil {
		return fmt.Errorf("failed to render template %s: %w", t.TemplatePath, err)
	}

	return nil
}
