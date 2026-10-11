// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package makefile

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/pkg/tasks"
	"strings"
	"testing"
)

// TestGetTasks_Makefile validates the generation of the smart Makefile.
// Logical Validation: The file must be named "Makefile" and occupy the
// base priority of the Build System band for deterministic execution.
func TestGetTasks_Makefile(t *testing.T) {
	handler := &Handler{}

	plan, err := handler.GetTasks("", "", "")
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
	plan, _ := handler.GetTasks("", "", "")
	task := plan[0].(*tasks.CreateTemplateTask)

	_, err := templates.ReadFile(task.TemplatePath)
	if err != nil {
		t.Errorf("Embedded template '%s' is missing from binary: %v", task.TemplatePath, err)
	}
}

// renderMakefile renders the embedded Makefile template for the given manifest
// and returns its contents.
func renderMakefile(t *testing.T, m *types.Manifest) string {
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

	out, err := os.ReadFile(filepath.Join(dir, "Makefile")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read generated Makefile: %v", err)
	}
	return string(out)
}

// TestMakefile_BenignProjectRendersUnchanged pins the benign rendering so the
// injection hardening does not silently change ordinary output.
func TestMakefile_BenignProjectRendersUnchanged(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{Name: "backend", Path: "services/backend", Language: "go"}},
		Metadata: map[string]string{"test_command": "go test ./..."},
	}
	out := renderMakefile(t, m)

	for _, want := range []string{
		`@echo "Building backend (go)..."`,
		"(cd services/backend && $(MAKE_BUILD_COMMAND_go))",
		"go test ./...",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated Makefile missing %q:\n%s", want, out)
		}
	}
}

// TestMakefile_ProjectNameCannotInject is a regression guard for SCB-007: a
// project name containing a newline must not break out of an `echo` recipe and
// inject extra make lines.
func TestMakefile_ProjectNameCannotInject(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{
			Name:     "evil\ntouch /tmp/pwned",
			Path:     "backend",
			Language: "go",
		}},
	}
	out := renderMakefile(t, m)

	if strings.Contains(out, "\ntouch /tmp/pwned") {
		t.Errorf("project name injected a new make recipe line:\n%s", out)
	}
	if !strings.Contains(out, `evil\ntouch /tmp/pwned`) {
		t.Errorf("expected the name to be rendered as an escaped scalar:\n%s", out)
	}
}
