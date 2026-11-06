// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package makefile

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
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

// makeTestCommandRef matches a Makefile reference to a per-language test
// command, e.g. $(MAKE_TEST_COMMAND_go).
var makeTestCommandRef = regexp.MustCompile(`\$\(MAKE_TEST_COMMAND_([A-Za-z0-9_.-]+)\)`)

// emptySubshellCommand matches a subshell whose command is empty, e.g.
// `(cd . && )`. It appears when an undefined MAKE_TEST_COMMAND_<lang> variable
// is expanded by make.
var emptySubshellCommand = regexp.MustCompile(`\(cd\s+[^)]*&&\s*\)`)

// makefileTestTarget extracts the rendered `test` target section so assertions
// about the executed test recipe do not match the inert command definitions at
// the bottom of the Makefile.
func makefileTestTarget(t *testing.T, out string) string {
	t.Helper()

	const startMarker = "# --- Test Target ---"
	const endMarker = "# --- Main Clean Target ---"

	start := strings.Index(out, startMarker)
	end := strings.Index(out, endMarker)
	if start < 0 || end <= start {
		t.Fatalf("generated Makefile is missing a complete test target section:\n%s", out)
	}
	return out[start:end]
}

// assertTestCommandsDefined checks that every MAKE_TEST_COMMAND_<lang>
// referenced by the test target has a non-empty definition. An undefined
// variable expands to nothing, leaving make to run an empty command such as
// `(cd . && )`, which the shell rejects.
func assertTestCommandsDefined(t *testing.T, out string) {
	t.Helper()

	target := makefileTestTarget(t, out)
	for _, match := range makeTestCommandRef.FindAllStringSubmatch(target, -1) {
		lang := match[1]
		defPrefix := "MAKE_TEST_COMMAND_" + lang + " :="
		idx := strings.Index(out, defPrefix)
		if idx < 0 {
			t.Errorf("test target references $(MAKE_TEST_COMMAND_%s) but the Makefile never defines %s; make expands it to an empty command such as `(cd . && )`", lang, defPrefix)
			continue
		}
		value := out[idx+len(defPrefix):]
		if newline := strings.IndexByte(value, '\n'); newline >= 0 {
			value = value[:newline]
		}
		if strings.TrimSpace(value) == "" {
			t.Errorf("%s is defined with an empty command", defPrefix)
		}
	}
}

// TestMakefile_NilMetadataNoProjectsIsNeutral is a regression guard for #148:
// a project created without a language must not render a Go test default when
// there is no test_command metadata and no projects.
func TestMakefile_NilMetadataNoProjectsIsNeutral(t *testing.T) {
	out := renderMakefile(t, &types.Manifest{})

	if strings.Contains(out, "go test ./...") {
		t.Errorf("no-project Makefile must not contain a Go test default:\n%s", out)
	}
	if !strings.Contains(out, "No projects configured; nothing to test.") {
		t.Errorf("no-project Makefile must explain that there is nothing to test:\n%s", out)
	}
}

// TestMakefile_PerLanguageTestCommands verifies that an unset test_command
// falls back to the project's language-specific test command.
func TestMakefile_PerLanguageTestCommands(t *testing.T) {
	tests := []struct {
		name       string
		language   string
		path       string
		wantDef    string
		wantRecipe string
	}{
		{
			name:       "go",
			language:   "go",
			path:       "services/backend",
			wantDef:    "MAKE_TEST_COMMAND_go := go test ./...",
			wantRecipe: "(cd services/backend && $(MAKE_TEST_COMMAND_go))",
		},
		{
			name:       "svelte",
			language:   "svelte",
			path:       "web",
			wantDef:    "MAKE_TEST_COMMAND_svelte := npm test",
			wantRecipe: "(cd web && $(MAKE_TEST_COMMAND_svelte))",
		},
		{
			name:       "spring",
			language:   "spring",
			path:       "api",
			wantDef:    "MAKE_TEST_COMMAND_spring := ./mvnw test",
			wantRecipe: "(cd api && $(MAKE_TEST_COMMAND_spring))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &types.Manifest{
				Projects: []types.Project{{
					Name:     "app",
					Path:     tt.path,
					Language: tt.language,
				}},
			}
			out := renderMakefile(t, m)
			target := makefileTestTarget(t, out)

			if !strings.Contains(out, tt.wantDef) {
				t.Errorf("generated Makefile missing language command %q:\n%s", tt.wantDef, out)
			}
			if !strings.Contains(target, tt.wantRecipe) {
				t.Errorf("test target missing per-language recipe %q:\n%s", tt.wantRecipe, target)
			}
			if !strings.Contains(target, `@echo "Testing app (`+tt.language+`)..."`) {
				t.Errorf("test target missing per-project announcement for %s:\n%s", tt.language, target)
			}
			if tt.language != "go" && (strings.Contains(target, "go test") || strings.Contains(target, "MAKE_TEST_COMMAND_go")) {
				t.Errorf("%s test target claimed a Go test:\n%s", tt.language, target)
			}

			assertTestCommandsDefined(t, out)
		})
	}
}

// TestMakefile_ExplicitTestCommandOverridesPerLanguage verifies that an
// explicit test_command wins over the per-language loop for every project.
func TestMakefile_ExplicitTestCommandOverridesPerLanguage(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{Name: "backend", Path: "services/backend", Language: "go"}},
		Metadata: map[string]string{"test_command": "custom test"},
	}
	out := renderMakefile(t, m)
	target := makefileTestTarget(t, out)

	if !strings.Contains(target, "custom test") {
		t.Errorf("test target did not use the explicit test_command:\n%s", target)
	}
	if strings.Contains(target, "Testing ") {
		t.Errorf("per-language loop was emitted despite an explicit test_command:\n%s", target)
	}
	if strings.Contains(target, "$(MAKE_TEST_COMMAND_") {
		t.Errorf("per-language command was emitted despite an explicit test_command:\n%s", target)
	}
}

// TestMakefile_CustomLanguageWithoutTestCommandIsSafe is a regression guard for
// #148 after custom language handlers (for example rust) were supported. A
// project whose language has no MAKE_TEST_COMMAND_<language> definition must
// not render an empty shell command or a Go test claim.
func TestMakefile_CustomLanguageWithoutTestCommandIsSafe(t *testing.T) {
	m := &types.Manifest{
		Projects: []types.Project{{Name: "cli", Path: ".", Language: "rust"}},
	}
	out := renderMakefile(t, m)
	target := makefileTestTarget(t, out)

	if emptySubshellCommand.MatchString(target) {
		t.Errorf("test target rendered an empty subshell command:\n%s", target)
	}
	if strings.Contains(target, "go test") || strings.Contains(target, "MAKE_TEST_COMMAND_go") {
		t.Errorf("rust project test target claimed a Go test:\n%s", target)
	}

	assertTestCommandsDefined(t, out)
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
