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

	// Case 2: Existence Check (Should fail without Force)
	if err := task.Execute(tc); err == nil {
		t.Error("Overwriting existing file should fail without Force")
	}

	// Case 3: Force Overwrite
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
