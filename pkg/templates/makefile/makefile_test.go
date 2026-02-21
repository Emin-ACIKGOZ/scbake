// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package makefile

import (
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// TestGetTasks_Makefile validates the generation of the smart Makefile.
// Logical Validation: The file must be named "Makefile" and occupy the
// base priority of the Build System band for deterministic execution.
func TestGetTasks_Makefile(t *testing.T) {
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

	// 1. Assert Deterministic Priority
	// Logic: This task must start at the exact base of the Build System band (1400).
	expectedPrio := int(types.PrioBuildSystem)
	if task.TaskPrio != expectedPrio {
		t.Errorf("Priority mismatch: expected first slot in band (%d), got %d", expectedPrio, task.TaskPrio)
	}

	// 2. Assert Exact Template Path and Output
	if task.TemplatePath != "makefile.tpl" {
		t.Errorf("Wrong template path: expected 'makefile.tpl', got '%s'", task.TemplatePath)
	}
	if task.OutputPath != "Makefile" {
		t.Errorf("Wrong output filename: expected 'Makefile', got '%s'", task.OutputPath)
	}

	// 3. Verify TemplateFS accessibility
	if _, err := task.TemplateFS.ReadFile(task.TemplatePath); err != nil {
		t.Errorf("Task TemplateFS is invalid or template is missing: %v", err)
	}
}

// TestMakefile_TemplateExists verifies the embedded template is bundled correctly.
func TestMakefile_TemplateExists(t *testing.T) {
	handler := &Handler{}
	plan, _ := handler.GetTasks("")
	task := plan[0].(*tasks.CreateTemplateTask)

	_, err := templates.ReadFile(task.TemplatePath)
	if err != nil {
		t.Errorf("Embedded template '%s' is missing from binary: %v", task.TemplatePath, err)
	}
}
