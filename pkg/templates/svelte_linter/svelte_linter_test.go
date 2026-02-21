// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package sveltelinter

import (
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// TestGetTasks_SvelteLinter validates the deterministic 3-step plan for Svelte linting.
// Logical Validation:
// 1. Write eslint.config.js (Base priority)
// 2. npm install dependencies (Base + 1)
// 3. npm pkg set scripts (Base + 2)
func TestGetTasks_SvelteLinter(t *testing.T) {
	handler := &Handler{}

	plan, err := handler.GetTasks("")
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}

	if len(plan) != 3 {
		t.Fatalf("Expected 3 tasks, got %d", len(plan))
	}

	// Decompose validation to keep cyclomatic complexity low
	t.Run("PrioritySequence", func(t *testing.T) {
		assertSvelteLinterPriorities(t, plan)
	})

	t.Run("StructuralIntegrity", func(t *testing.T) {
		assertSvelteLinterStructure(t, plan)
	})
}

// assertSvelteLinterPriorities enforces the strict p, p+1, p+2 sequence.
func assertSvelteLinterPriorities(t *testing.T, plan []types.Task) {
	t.Helper()
	basePrio := int(types.PrioLinter)

	for i, task := range plan {
		expected := basePrio + i
		if task.Priority() != expected {
			t.Errorf("Task %d priority mismatch: expected %d, got %d", i, expected, task.Priority())
		}
	}
}

// assertSvelteLinterStructure validates command strings and template paths.
func assertSvelteLinterStructure(t *testing.T, plan []types.Task) {
	t.Helper()

	configTask := plan[0].(*tasks.CreateTemplateTask)
	installTask := plan[1].(*tasks.ExecCommandTask)
	scriptTask := plan[2].(*tasks.ExecCommandTask)

	// Validate Config File
	if configTask.TemplatePath != "eslint.config.js.tpl" || configTask.OutputPath != "eslint.config.js" {
		t.Errorf("Config task path mismatch: %s -> %s", configTask.TemplatePath, configTask.OutputPath)
	}

	// Validate Command Invocation
	if installTask.Cmd != "npm" || scriptTask.Cmd != "npm" {
		t.Error("NPM tasks must use the 'npm' binary")
	}

	// Validate Context and Dependencies
	if !installTask.RunInTarget || !scriptTask.RunInTarget {
		t.Error("NPM tasks must execute within the target project directory")
	}

	assertDependencyExists(t, installTask.Args, "eslint-plugin-svelte")
}

// assertDependencyExists validates the presence of a specific package in the install list.
func assertDependencyExists(t *testing.T, args []string, pkg string) {
	t.Helper()
	for _, arg := range args {
		if arg == pkg {
			return
		}
	}
	t.Errorf("Dependency list missing core package: %s", pkg)
}

// TestSvelteLinter_TemplateIntegrity ensures the embedded file is readable.
func TestSvelteLinter_TemplateIntegrity(t *testing.T) {
	handler := &Handler{}
	plan, _ := handler.GetTasks("")
	task := plan[0].(*tasks.CreateTemplateTask)

	if _, err := task.TemplateFS.ReadFile(task.TemplatePath); err != nil {
		t.Errorf("Task TemplateFS is invalid or template is missing: %v", err)
	}
}
