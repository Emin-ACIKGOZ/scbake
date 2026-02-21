// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package golang

import (
	"os"
	"path/filepath"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// TestGetTasks_NewProject validates the initialization path when go.mod is missing.
func TestGetTasks_NewProject(t *testing.T) {
	tempDir := t.TempDir()
	handler := &Handler{}

	plan := getPlanOrFail(t, handler, tempDir)

	assertPlanLength(t, plan, 4)
	assertGoModInitTask(t, plan[2], filepath.Base(tempDir))
}

func getPlanOrFail(t *testing.T, h *Handler, path string) []types.Task {
	t.Helper()
	plan, err := h.GetTasks(path)
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}
	return plan
}

func assertPlanLength(t *testing.T, plan []types.Task, expected int) {
	t.Helper()
	if len(plan) != expected {
		t.Fatalf("Expected %d tasks, got %d", expected, len(plan))
	}
}

func assertGoModInitTask(t *testing.T, task types.Task, expectedModule string) {
	t.Helper()

	initTask, ok := task.(*tasks.ExecCommandTask)
	if !ok {
		t.Fatalf("Task should be ExecCommandTask")
	}

	if len(initTask.Args) < 3 || initTask.Args[1] != "init" {
		t.Fatalf("Task should be 'go mod init', got: %v", initTask.Args)
	}

	if !contains(initTask.Args, expectedModule) {
		t.Fatalf("Expected module name '%s' not found in args %v",
			expectedModule, initTask.Args)
	}
}

// TestGetTasks_ExistingProject validates the maintenance path when go.mod exists.
func TestGetTasks_ExistingProject(t *testing.T) {
	tempDir := t.TempDir()
	handler := &Handler{}

	// Create dummy go.mod with secure permissions (G306 fix)
	if err := os.WriteFile(
		filepath.Join(tempDir, "go.mod"),
		[]byte("module test"),
		0600,
	); err != nil {
		t.Fatal(err)
	}

	plan := getPlanOrFail(t, handler, tempDir)

	assertPlanLength(t, plan, 3)
	assertNoGoModInit(t, plan)
}

func assertNoGoModInit(t *testing.T, plan []types.Task) {
	t.Helper()

	for _, task := range plan {
		if exec, ok := task.(*tasks.ExecCommandTask); ok {
			if contains(exec.Args, "init") {
				t.Fatal("Plan should not contain 'go mod init' for an existing project")
			}
		}
	}
}

// TestGetTasks_PrioritySafety ensures tasks stay within the Language Setup band.
func TestGetTasks_PrioritySafety(t *testing.T) {
	handler := &Handler{}
	plan, err := handler.GetTasks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	for _, task := range plan {
		p := task.Priority()
		if p < int(types.PrioLangSetup) || p > int(types.MaxLangSetup) {
			t.Errorf("Task '%s' has priority %d outside band [%d, %d]",
				task.Description(), p,
				types.PrioLangSetup, types.MaxLangSetup)
		}
	}
}

func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
