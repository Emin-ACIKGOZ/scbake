// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package svelte

import (
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// TestGetTasks_NewSvelteProject validates the bootstrapping logic for a fresh Svelte app.
func TestGetTasks_NewSvelteProject(t *testing.T) {
	tempDir := t.TempDir()
	handler := &Handler{}
	targetPath := filepath.Join(tempDir, "my-app")

	plan := getPlanOrFail(t, handler, targetPath)
	assertNewProjectPlanLength(t, plan)
	assertCreateDirTask(t, plan[0], targetPath)
	assertViteInitTask(t, plan[1])
	assertScriptSetupTask(t, plan[3])
}

func getPlanOrFail(t *testing.T, h *Handler, path string) []types.Task {
	t.Helper()
	plan, err := h.GetTasks(path)
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}
	return plan
}

func assertNewProjectPlanLength(t *testing.T, plan []types.Task) {
	t.Helper()
	const expected = 4
	if len(plan) != expected {
		t.Fatalf("Expected %d tasks, got %d", expected, len(plan))
	}
}

func assertCreateDirTask(t *testing.T, task types.Task, expectedPath string) {
	t.Helper()
	dirTask, ok := task.(*tasks.CreateDirTask)
	if !ok || dirTask.Path != expectedPath {
		t.Fatal("First task must be CreateDirTask for the target path")
	}
}

func assertViteInitTask(t *testing.T, task types.Task) {
	t.Helper()
	viteTask, ok := task.(*tasks.ExecCommandTask)
	if !ok || viteTask.Cmd != "npm" {
		t.Fatal("Second task should be 'npm create'")
	}

	if !viteTask.RunInTarget {
		t.Fatal("Vite command must run in target directory")
	}

	if !contains(viteTask.Args, "svelte") {
		t.Fatal("Vite command missing 'svelte' template flag")
	}
}

func assertScriptSetupTask(t *testing.T, task types.Task) {
	t.Helper()
	pkgTask, ok := task.(*tasks.ExecCommandTask)
	if !ok || len(pkgTask.Args) == 0 || pkgTask.Args[0] != "pkg" {
		t.Fatalf("Final task should be 'npm pkg set', got %v", pkgTask)
	}
}

// TestGetTasks_ExistingSvelteProject validates idempotency.
func TestGetTasks_ExistingSvelteProject(t *testing.T) {
	tempDir := t.TempDir()
	handler := &Handler{}

	// Create dummy package.json to simulate existing project
	if err := os.WriteFile(
		filepath.Join(tempDir, "package.json"),
		[]byte("{}"),
		0600, // fixed for gosec G306
	); err != nil {
		t.Fatal(err)
	}

	plan := getPlanOrFail(t, handler, tempDir)

	if len(plan) != 1 {
		t.Errorf("Expected only 1 task for existing project, got %d", len(plan))
	}
}

// TestSveltePriorityBands verifies tasks are placed in the correct execution order.
func TestSveltePriorityBands(t *testing.T) {
	handler := &Handler{}
	plan, err := handler.GetTasks(filepath.Join(t.TempDir(), "app"))
	if err != nil {
		t.Fatal(err)
	}

	for i, task := range plan {
		assertPriorityBand(t, i, task.Priority())
	}
}

func assertPriorityBand(t *testing.T, index int, prio int) {
	t.Helper()
	if index == 0 {
		if prio < int(types.PrioDirCreate) || prio > int(types.MaxDirCreate) {
			t.Errorf("Task 0 priority %d out of DirCreate band", prio)
		}
		return
	}

	if prio < int(types.PrioLangSetup) || prio > int(types.MaxLangSetup) {
		t.Errorf("Task %d priority %d out of LangSetup band", index, prio)
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
