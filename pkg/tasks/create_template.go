package tasks

import (
	"embed"
	"errors" // Import
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"scbake/internal/types"
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

	// TemplateData is DEPRECATED. We now pass the entire manifest.
	// We leave this field here for now to avoid breaking handlers.
	TemplateData any

	Desc     string // Human-readable description
	TaskPrio int    // Execution priority
}

func (t *CreateTemplateTask) Description() string {
	return t.Desc
}

func (t *CreateTemplateTask) Priority() int {
	return t.TaskPrio
}

func (t *CreateTemplateTask) Execute(tc types.TaskContext) error {
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
	finalPath := filepath.Join(tc.TargetPath, t.OutputPath)

	// 4. Ensure the directory exists
	dir := filepath.Dir(finalPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// 5. --- THIS IS THE FIX ---
	// Check if the file exists *before* creating it.
	if !tc.Force {
		if _, err := os.Stat(finalPath); err == nil {
			// File exists and Force is false.
			return fmt.Errorf("file already exists: %s. Use --force to overwrite", finalPath)
		} else if !errors.Is(err, os.ErrNotExist) {
			// Some other error occurred (e.g., permissions).
			return err
		}
	}

	// 6. Create the output file (now we know it's safe to do so)
	f, err := os.Create(finalPath)
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", finalPath, err)
	}
	defer f.Close()

	// 7. Execute the template and write to the file
	if err := tpl.Execute(f, tc.Manifest); err != nil {
		return fmt.Errorf("failed to render template %s: %w", t.TemplatePath, err)
	}

	return nil
}
