// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package editorconfig

import (
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// TestGetTasks_EditorConfig validates exact positioning and resource binding.
func TestGetTasks_EditorConfig(t *testing.T) {
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
	// Logic: This should be the very first task in the Universal Config band.
	expectedPrio := int(types.PrioConfigUniversal)
	if task.TaskPrio != expectedPrio {
		t.Errorf("Priority mismatch: expected first slot in band (%d), got %d", expectedPrio, task.TaskPrio)
	}

	// 2. Assert Template Path and Output
	if task.TemplatePath != ".editorconfig.tpl" {
		t.Errorf("Wrong template path: expected '.editorconfig.tpl', got '%s'", task.TemplatePath)
	}
	if task.OutputPath != ".editorconfig" {
		t.Errorf("Wrong output filename: expected '.editorconfig', got '%s'", task.OutputPath)
	}

	// 3. Validate TemplateFS readability
	if _, err := task.TemplateFS.ReadFile(task.TemplatePath); err != nil {
		t.Errorf("Task TemplateFS is invalid or template is missing: %v", err)
	}
}

// TestEditorConfig_TemplateExists verifies the embedded template is physically present.
func TestEditorConfig_TemplateExists(t *testing.T) {
	handler := &Handler{}
	plan, _ := handler.GetTasks("")
	task := plan[0].(*tasks.CreateTemplateTask)

	_, err := templates.ReadFile(task.TemplatePath)
	if err != nil {
		t.Errorf("Embedded template '%s' is missing: %v", task.TemplatePath, err)
	}
}
