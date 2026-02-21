package tasks

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"testing"
)

//go:embed testdata/simple.tpl
var testTemplates embed.FS

func TestCreateTemplateTask(t *testing.T) {
	// Setup temp workspace
	tmpDir := t.TempDir()

	// Setup Task Context
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "MyProject"},
		},
	}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     false,
		Force:      false,
	}

	// Case 1: Render a valid template
	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "output.txt",
		Desc:         "Render simple template",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Template execution failed: %v", err)
	}

	// Verify Content
	// G304: Reading file from trusted temp directory in test scope
	//nolint:gosec
	content, err := os.ReadFile(filepath.Join(tmpDir, "output.txt"))
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}
	expected := "Hello MyProject from v1.0.0"
	if string(content) != expected {
		t.Errorf("Render mismatch. Got '%s', want '%s'", string(content), expected)
	}

	// Case 2: Existence Check (Should fail without Force)
	if err := task.Execute(tc); err == nil {
		t.Error("Overwriting existing file should fail without Force, but it succeeded")
	}

	// Case 3: Force Overwrite (Should succeed)
	tc.Force = true
	if err := task.Execute(tc); err != nil {
		t.Errorf("Force overwrite failed: %v", err)
	}

	// Case 4: Path Traversal Attack (Security Check)
	// Try to write outside the target path
	attackTask := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "../escaped.txt", // Trying to go up
		Desc:         "Path traversal attempt",
	}

	// Reset Force
	tc.Force = false
	if err := attackTask.Execute(tc); err == nil {
		t.Error("Path traversal attack succeeded! It should have been blocked.")
	}
}
