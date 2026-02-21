// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package devcontainer

import (
	"reflect"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

// TestGetTasks_DevContainer validates exact template mapping, ordering, and band safety.
// Logic: Dev Container initialization requires a Dockerfile to exist before or
// alongside the devcontainer.json that references it.
func TestGetTasks_DevContainer(t *testing.T) {
	handler := &Handler{}

	plan, err := handler.GetTasks("")
	if err != nil {
		t.Fatalf("Failed to get tasks: %v", err)
	}

	// 1. Assert exact task count
	if len(plan) != 2 {
		t.Fatalf("Expected 2 tasks, got %d", len(plan))
	}

	// 2. Validate structural integrity via sub-tests to reduce cyclomatic complexity
	t.Run("StructuralAssertions", func(t *testing.T) {
		assertDevContainerStructure(t, plan)
	})

	t.Run("PriorityAssertions", func(t *testing.T) {
		assertDevContainerPriorities(t, plan)
	})
}

// assertDevContainerStructure verifies types, paths, and shared filesystem instances.
func assertDevContainerStructure(t *testing.T, plan []types.Task) {
	t.Helper()

	dockerTask, ok1 := plan[0].(*tasks.CreateTemplateTask)
	jsonTask, ok2 := plan[1].(*tasks.CreateTemplateTask)
	if !ok1 || !ok2 {
		t.Fatal("Tasks are not of type *tasks.CreateTemplateTask")
	}

	// Verify exact template file mapping
	if dockerTask.TemplatePath != "Dockerfile.tpl" || jsonTask.TemplatePath != "devcontainer.json.tpl" {
		t.Errorf("TemplatePath mismatch. Docker: %s, JSON: %s", dockerTask.TemplatePath, jsonTask.TemplatePath)
	}

	// Identity Check: Verify both tasks share the same embedded filesystem instance
	if !reflect.DeepEqual(dockerTask.TemplateFS, jsonTask.TemplateFS) {
		t.Error("Tasks are using different embed.FS instances")
	}
}

// assertDevContainerPriorities enforces deterministic sequence and band boundaries.
func assertDevContainerPriorities(t *testing.T, plan []types.Task) {
	t.Helper()

	prioDocker := plan[0].Priority()
	prioJSON := plan[1].Priority()

	// Logic: Dockerfile (1500) -> JSON (1501). Must be strictly consecutive.
	if prioDocker != int(types.PrioDevEnv) {
		t.Errorf("Dockerfile priority should be %d, got %d", types.PrioDevEnv, prioDocker)
	}

	if prioDocker+1 != prioJSON {
		t.Errorf("Tasks are not strictly sequential. Docker: %d, JSON: %d", prioDocker, prioJSON)
	}

	// Upper bound check (Must not bleed into VersionControl band)
	upperBound := int(types.PrioVersionControl) - 1
	if prioJSON > upperBound {
		t.Errorf("Task priority %d exceeds DevEnv band limit %d", prioJSON, upperBound)
	}
}

// TestDevContainer_TemplateIntegrity ensures embedded files are physically present in the FS.
func TestDevContainer_TemplateIntegrity(t *testing.T) {
	handler := &Handler{}
	plan, _ := handler.GetTasks("")

	for _, task := range plan {
		tmplTask := task.(*tasks.CreateTemplateTask)
		data, err := templates.ReadFile(tmplTask.TemplatePath)
		if err != nil {
			t.Errorf("Could not find embedded template file: %s", tmplTask.TemplatePath)
		}
		if len(data) == 0 {
			t.Errorf("Embedded template file %s is empty", tmplTask.TemplatePath)
		}
	}
}
