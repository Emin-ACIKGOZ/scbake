// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cighub

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"strings"
	"testing"
)

// TestGetTasks_GitHubCI validates the workflow file creation task.
// Logical Validation: GitHub Actions files MUST be in .github/workflows/ to be functional.
func TestGetTasks_GitHubCI(t *testing.T) {
	handler := &Handler{}

	// targetPath is ignored by this handler as CI is usually root-level
	plan, err := handler.GetTasks("", "", "")
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
	plan, _ := handler.GetTasks("", "", "")
	task := plan[0].(*tasks.CreateTemplateTask)

	// Ensure the embedded file path matches the go:embed directive
	_, err := templates.ReadFile(task.TemplatePath)
	if err != nil {
		t.Errorf("Embedded template file '%s' not found: %v", task.TemplatePath, err)
	}
}

// renderWorkflow renders the embedded CI workflow for the given manifest and
// returns its contents.
func renderWorkflow(t *testing.T, m *types.Manifest) string {
	t.Helper()

	handler := &Handler{}
	plan, err := handler.GetTasks("", "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	task := plan[0].(*tasks.CreateTemplateTask)

	dir := t.TempDir()
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: dir, Manifest: m}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	out, err := os.ReadFile(filepath.Join(dir, ".github", "workflows", "main.yml"))
	if err != nil {
		t.Fatalf("read generated workflow: %v", err)
	}
	return string(out)
}

// TestWorkflow_BenignProjectRendersUnchanged pins the benign rendering so the
// injection hardening does not silently change ordinary output.
func TestWorkflow_BenignProjectRendersUnchanged(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{Name: "backend", Path: "services/backend", Language: "go"}},
	}
	out := renderWorkflow(t, m)

	for _, want := range []string{
		`echo "--- Building backend (go) ---"`,
		"cd services/backend",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated workflow missing %q:\n%s", want, out)
		}
	}
}

// TestWorkflow_ProjectNameCannotInject is a regression guard for SCB-006: a
// project name containing a newline or quote must not break out of the `echo`
// line in the generated `run:` block.
func TestWorkflow_ProjectNameCannotInject(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{
			Name:     "evil\ntouch /tmp/pwned",
			Path:     "backend",
			Language: "go",
		}},
	}
	out := renderWorkflow(t, m)

	if strings.Contains(out, "\ntouch /tmp/pwned") {
		t.Errorf("project name injected a new shell line:\n%s", out)
	}
	// The escaped form keeps the payload inert on the echo line.
	if !strings.Contains(out, `evil\ntouch /tmp/pwned`) {
		t.Errorf("expected the name to be rendered as an escaped scalar:\n%s", out)
	}
}
