// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
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

	// Case 1: Render a valid template (Standard relative path)
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

	// Case 2: Idempotent re-run (should succeed — same hash, no drift)
	if err := task.Execute(tc); err != nil {
		t.Errorf("Idempotent re-run should succeed: %v", err)
	}

	// Case 3: Modify file externally, then re-run (drift detection)
	existingContent := append([]byte("MANUAL EDIT"), []byte("Hello MyProject from v1.0.0")...)
	//nolint:gosec
	if err := os.WriteFile(filepath.Join(tmpDir, "output.txt"), existingContent, 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}
	if err := task.Execute(tc); err == nil {
		t.Error("Expected error when overwriting drifted file without Force")
	}

	// Case 4: Force Overwrite (drifted file)
	tc.Force = true
	if err := task.Execute(tc); err != nil {
		t.Errorf("Force overwrite of drifted file failed: %v", err)
	}

	// Case 5: Path Traversal Attack (Security Check)
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

func TestCreateTemplateTask_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "DryRun"}},
	}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     true,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "dryrun.txt",
		Desc:         "Dry run test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with dry-run failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "dryrun.txt")); !os.IsNotExist(err) {
		t.Error("Dry-run should not create the file")
	}
}

func TestCreateTemplateTask_ConflictStrategyOverwrite(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "OverwriteTest"}},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         manifest,
		ConflictStrategy: "overwrite",
		DryRun:           false,
		Force:            false,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "overwrite.txt",
		Desc:         "Overwrite test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute failed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "overwrite.txt"), []byte("DRIFTED CONTENT"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Overwrite strategy should succeed: %v", err)
	}
}

func TestCreateTemplateTask_ConflictStrategyArtifact(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "ArtifactTest"}},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         manifest,
		ConflictStrategy: "artifact",
		DryRun:           false,
		Force:            false,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "artifact.txt",
		Desc:         "Artifact test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute failed: %v", err)
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "artifact.txt"), []byte("DRIFTED"), 0644); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Artifact strategy should succeed: %v", err)
	}

	artifactPath := filepath.Join(tmpDir, "artifact.txt.scbake-new")
	if _, err := os.Stat(artifactPath); os.IsNotExist(err) {
		t.Error("Artifact file was not created")
	}
}

func TestCreateTemplateTask_ConflictStrategyKeepLocal(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "KeepLocalTest"}},
	}
	tc := types.TaskContext{
		TargetPath:       tmpDir,
		Manifest:         manifest,
		ConflictStrategy: "keep-local",
		DryRun:           false,
		Force:            false,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "keeplocal.txt",
		Desc:         "Keep local test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execute failed: %v", err)
	}

	drifterContent := []byte("DRIFTED")
	if err := os.WriteFile(filepath.Join(tmpDir, "keeplocal.txt"), drifterContent, 0600); err != nil {
		t.Fatalf("Failed to modify file: %v", err)
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Keep-local strategy should succeed without error: %v", err)
	}

	//nolint:gosec // Test temp directory
	content, _ := os.ReadFile(filepath.Join(tmpDir, "keeplocal.txt"))
	if string(content) != string(drifterContent) {
		t.Errorf("Keep-local should preserve local content. Got %q, want %q", string(content), string(drifterContent))
	}
}

func TestCreateTemplateTask_FileNeverManaged(t *testing.T) {
	tmpDir := t.TempDir()

	manifest := &types.Manifest{
		SbakeVersion:  "v1.0.0",
		Projects:      []types.Project{{Name: "UnmanagedTest"}},
		ManagedFiles:  nil,
	}
	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest:   manifest,
		DryRun:     false,
		Force:      false,
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(filepath.Join(tmpDir, "preexisting.txt"), []byte("preexisting"), 0644); err != nil {
		t.Fatalf("Failed to create preexisting file: %v", err)
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "preexisting.txt",
		Desc:         "Unmanaged file test",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err == nil {
		t.Error("Expected error for unmanaged preexisting file without force")
	}
}

func TestCreateTemplateTask_Transaction(t *testing.T) {
	// Setup with Transaction
	rootDir := t.TempDir()
	tx, _ := transaction.New(rootDir)

	// Provide a valid manifest so the template can render {{ (index .Projects 0).Name }}
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "TrackedProject"},
		},
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   manifest,
		Tx:         tx,
	}

	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "tracked_file.txt",
		Desc:         "Tracked File",
		TaskPrio:     100,
	}

	// Execute
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Task execution failed: %v", err)
	}

	// File should exist
	path := filepath.Join(rootDir, "tracked_file.txt")
	absPath, _ := filepath.Abs(path)

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		t.Fatal("File was not created")
	}

	// Rollback
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// File should be gone
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("File was not removed by rollback")
	}
}
