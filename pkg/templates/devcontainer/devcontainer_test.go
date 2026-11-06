// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package devcontainer

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"testing"
)

const (
	commonUtilsFeature = "ghcr.io/devcontainers/features/common-utils:2"
	goFeature          = "ghcr.io/devcontainers/features/go:1"
	nodeFeature        = "ghcr.io/devcontainers/features/node:1"
	javaFeature        = "ghcr.io/devcontainers/features/java:1"
)

// TestGetTasks_DevContainer validates exact template mapping, ordering, and band safety.
// Logic: Dev Container initialization requires a Dockerfile to exist before or
// alongside the devcontainer.json that references it.
func TestGetTasks_DevContainer(t *testing.T) {
	handler := &Handler{}

	plan, err := handler.GetTasks("", "", "")
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
	plan, _ := handler.GetTasks("", "", "")

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

// renderDevcontainerJSON renders .devcontainer/devcontainer.json for the given
// manifest and returns its exact bytes.
func renderDevcontainerJSON(t *testing.T, m *types.Manifest) []byte {
	t.Helper()

	handler := &Handler{}
	plan, err := handler.GetTasks("", "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	var task *tasks.CreateTemplateTask
	for _, tk := range plan {
		ct, ok := tk.(*tasks.CreateTemplateTask)
		if ok && ct.OutputPath == ".devcontainer/devcontainer.json" {
			task = ct
			break
		}
	}
	if task == nil {
		t.Fatal("devcontainer.json task not found in plan")
	}

	dir := t.TempDir()
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: dir, Manifest: m}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(dir, ".devcontainer", "devcontainer.json")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read generated devcontainer.json: %v", err)
	}
	return out
}

// TestDevcontainer_RenderedJSONIsValid is a regression test for issue #153: the
// rendered file must never contain U+00A0 and must always be a valid JSON object.
// Feature keys must track the languages declared in the manifest.
func TestDevcontainer_RenderedJSONIsValid(t *testing.T) {
	tests := []struct {
		name     string
		projects []types.Project
		features map[string]bool
	}{
		{
			name: "no projects",
			features: map[string]bool{
				goFeature:   false,
				nodeFeature: false,
				javaFeature: false,
			},
		},
		{
			name:     "go",
			projects: []types.Project{{Name: "api", Path: "services/api", Language: "go"}},
			features: map[string]bool{
				goFeature:   true,
				nodeFeature: false,
				javaFeature: false,
			},
		},
		{
			name:     "svelte",
			projects: []types.Project{{Name: "web", Path: "web", Language: "svelte"}},
			features: map[string]bool{
				goFeature:   false,
				nodeFeature: true,
				javaFeature: false,
			},
		},
		{
			name:     "spring",
			projects: []types.Project{{Name: "service", Path: "service", Language: "spring"}},
			features: map[string]bool{
				goFeature:   false,
				nodeFeature: false,
				javaFeature: true,
			},
		},
		{
			name: "mixed",
			projects: []types.Project{
				{Name: "api", Path: "services/api", Language: "go"},
				{Name: "web", Path: "web", Language: "svelte"},
				{Name: "service", Path: "service", Language: "spring"},
			},
			features: map[string]bool{
				goFeature:   true,
				nodeFeature: true,
				javaFeature: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := renderDevcontainerJSON(t, &types.Manifest{Projects: tt.projects})

			if bytes.Contains(out, []byte("\u00a0")) {
				t.Fatalf("rendered devcontainer.json contains U+00A0 non-breaking space:\n%s", out)
			}

			if !json.Valid(out) {
				t.Fatalf("rendered devcontainer.json is not valid JSON:\n%s", out)
			}

			var doc map[string]any
			if err := json.Unmarshal(out, &doc); err != nil {
				t.Fatalf("unmarshal devcontainer.json: %v\n%s", err, out)
			}
			if doc == nil {
				t.Fatal("rendered devcontainer.json is not a JSON object")
			}

			features, ok := doc["features"].(map[string]any)
			if !ok {
				t.Fatalf("rendered devcontainer.json has no features object:\n%s", out)
			}
			if _, ok := features[commonUtilsFeature]; !ok {
				t.Errorf("missing base feature %q:\n%s", commonUtilsFeature, out)
			}

			for key, want := range tt.features {
				_, got := features[key]
				if got != want {
					t.Errorf("feature %q presence = %v, want %v:\n%s", key, got, want, out)
				}
			}
		})
	}
}
