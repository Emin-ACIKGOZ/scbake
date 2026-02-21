// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package golinter

import (
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// TestGetTasks_GoLinter_Deterministic validates exact positioning and resource binding.
// Logical Validation: The configuration must be named ".golangci.yml" and occupy the
// base priority of the Linter band to ensure deterministic execution.
func TestGetTasks_GoLinter(t *testing.T) {
	handler := &Handler{}

	plan, err := handler.GetTasks("")
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}

	if len(plan) != 1 {
		t.Fatalf("Expected exactly 1 task, got %d", len(plan))
	}

	task, ok := plan[0].(*tasks.CreateTemplateTask)
	if !ok {
		t.Fatal("Task type mismatch: expected *tasks.CreateTemplateTask")
	}

	// 1. Assert Deterministic First-Slot Priority
	// Logic: This task must start at the exact base of the Linter band (1200).
	expectedPrio := int(types.PrioLinter)
	if task.TaskPrio != expectedPrio {
		t.Errorf("Priority mismatch: expected first slot in band (%d), got %d", expectedPrio, task.TaskPrio)
	}

	// 2. Assert Exact Template Path and Output
	if task.TemplatePath != ".golangci.yml.tpl" {
		t.Errorf("Wrong template path: expected '.golangci.yml.tpl', got '%s'", task.TemplatePath)
	}
	if task.OutputPath != ".golangci.yml" {
		t.Errorf("Wrong output filename: expected '.golangci.yml', got '%s'", task.OutputPath)
	}

	// 3. Verify TemplateFS and Readability
	// Logic: Ensure the bound FS is not nil and contains the required asset.
	if _, err := task.TemplateFS.ReadFile(task.TemplatePath); err != nil {
		t.Errorf("Task TemplateFS is invalid or template file is missing: %v", err)
	}
}

// TestGoLinter_TemplateExists verifies the embedded template is bundled correctly.
func TestGoLinter_TemplateExists(t *testing.T) {
	handler := &Handler{}
	plan, _ := handler.GetTasks("")
	task := plan[0].(*tasks.CreateTemplateTask)

	_, err := templates.ReadFile(task.TemplatePath)
	if err != nil {
		t.Errorf("Embedded template '%s' is missing from binary: %v", task.TemplatePath, err)
	}
}
