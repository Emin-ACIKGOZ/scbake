// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package cighub

import (
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// TestGetTasks_GitHubCI validates the workflow file creation task.
// Logical Validation: GitHub Actions files MUST be in .github/workflows/ to be functional.
func TestGetTasks_GitHubCI(t *testing.T) {
	handler := &Handler{}

	// targetPath is ignored by this handler as CI is usually root-level
	plan, err := handler.GetTasks("")
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}

	if len(plan) != 1 {
		t.Fatalf("Expected 1 task, got %d", len(plan))
	}

	task, ok := plan[0].(*tasks.CreateTemplateTask)
	if !ok {
		t.Fatal("Task should be a CreateTemplateTask")
	}

	// Verify the rigid directory requirement for GitHub Actions
	expectedPath := ".github/workflows/main.yml"
	if task.OutputPath != expectedPath {
		t.Errorf("Wrong output path. Want: %s, Got: %s", expectedPath, task.OutputPath)
	}

	// Verify Priority sequence
	if task.TaskPrio < int(types.PrioCI) || task.TaskPrio > int(types.MaxCI) {
		t.Errorf("Priority %d is outside CI band [%d, %d]", task.TaskPrio, types.PrioCI, types.MaxCI)
	}
}

// TestGetTasks_TemplateBinding checks if the embedded template is correctly referenced.
func TestGetTasks_TemplateBinding(t *testing.T) {
	handler := &Handler{}
	plan, _ := handler.GetTasks("")
	task := plan[0].(*tasks.CreateTemplateTask)

	// Ensure the embedded file path matches the go:embed directive
	_, err := templates.ReadFile(task.TemplatePath)
	if err != nil {
		t.Errorf("Embedded template file '%s' not found: %v", task.TemplatePath, err)
	}
}
