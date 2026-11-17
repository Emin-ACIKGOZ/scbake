// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

func getPlanWithVarsOrFail(t *testing.T, h *Handler, path string, vars map[string]string) []types.Task {
	t.Helper()
	plan, err := h.GetTasks(path, "", "", vars)
	if err != nil {
		t.Fatalf("GetTasks(%q, vars=%v) failed: %v", path, vars, err)
	}
	return plan
}

func execTaskAt(t *testing.T, plan []types.Task, idx int) *tasks.ExecCommandTask {
	t.Helper()
	if idx < 0 || idx >= len(plan) {
		t.Fatalf("task index %d out of range for plan of length %d", idx, len(plan))
	}
	task, ok := plan[idx].(*tasks.ExecCommandTask)
	if !ok {
		t.Fatalf("task %d is %T, want *tasks.ExecCommandTask", idx, plan[idx])
	}
	return task
}

// findGoModVerb returns the index of the single `go mod <verb>` task, or -1.
func findGoModVerb(t *testing.T, plan []types.Task, verb string) int {
	t.Helper()
	found := -1
	for i, task := range plan {
		exec, ok := task.(*tasks.ExecCommandTask)
		if !ok || len(exec.Args) < 2 || exec.Args[0] != "mod" || exec.Args[1] != verb {
			continue
		}
		if found != -1 {
			t.Fatalf("multiple 'go mod %s' tasks at %d and %d", verb, found, i)
		}
		found = i
	}
	return found
}

func assertNoGoModVerb(t *testing.T, plan []types.Task, verb string) {
	t.Helper()
	if idx := findGoModVerb(t, plan, verb); idx != -1 {
		t.Fatalf("unexpected 'go mod %s' task at index %d: %v", verb, idx, execTaskAt(t, plan, idx).Args)
	}
}

func TestGetTasks_ModulePathOverride(t *testing.T) {
	plan := getPlanWithVarsOrFail(t, &Handler{}, t.TempDir(), map[string]string{
		"module_path": "github.com/acme/gm",
	})

	assertPlanLength(t, plan, 4)
	initTask := execTaskAt(t, plan, 2)
	want := []string{"mod", "init", "github.com/acme/gm"}
	if strings.Join(initTask.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("init args = %v, want %v", initTask.Args, want)
	}
	if !strings.Contains(initTask.Desc, "github.com/acme/gm") {
		t.Errorf("init description %q should contain the explicit module path", initTask.Desc)
	}
}

func TestGetTasks_ModulePathPreservedVerbatim(t *testing.T) {
	for _, module := range []string{
		"Example.COM/Acme/GM",
		"github.com/acme/gm/v2",
		"github.com/acme/gm.git",
	} {
		t.Run(module, func(t *testing.T) {
			plan := getPlanWithVarsOrFail(t, &Handler{}, t.TempDir(), map[string]string{
				"module_path": module,
			})
			initTask := execTaskAt(t, plan, 2)
			if got := initTask.Args[len(initTask.Args)-1]; got != module {
				t.Fatalf("module path = %q, want verbatim %q", got, module)
			}
		})
	}
}

func TestGetTasks_InvalidModulePathRejectedAtPlanTime(t *testing.T) {
	cases := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "dash", value: "-x", wantErr: "leading dash"},
		{name: "double_dash", value: "--", wantErr: "leading dash"},
		{name: "flag", value: "-C", wantErr: "leading dash"},
		{name: "leading_space", value: " github.com/a", wantErr: "whitespace"},
		{name: "trailing_space", value: "github.com/a ", wantErr: "whitespace"},
		{name: "internal_space", value: "a b", wantErr: "whitespace"},
		{name: "tab", value: "a\tb", wantErr: "whitespace"},
		{name: "newline", value: "a\nb", wantErr: "whitespace"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := (&Handler{}).GetTasks(t.TempDir(), "", "", map[string]string{
				"module_path": tc.value,
			})
			if err == nil {
				t.Fatalf("expected error for module_path=%q, got plan %v", tc.value, plan)
			}
			if plan != nil {
				t.Errorf("expected no tasks on error, got %v", plan)
			}
			if !strings.Contains(err.Error(), "module_path") || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want mention of module_path and %q", err, tc.wantErr)
			}
		})
	}
}

func TestGetTasks_OverrideWinsOverAwkwardTargetDir(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "-x")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}

	plan := getPlanWithVarsOrFail(t, &Handler{}, target, map[string]string{
		"module_path": "github.com/acme/valid",
	})
	initTask := execTaskAt(t, plan, 2)
	if got := initTask.Args[2]; got != "github.com/acme/valid" {
		t.Fatalf("module path = %q, want explicit override", got)
	}
}

func TestGetTasks_GoVersionOrderedBeforeTidy(t *testing.T) {
	plan := getPlanWithVarsOrFail(t, &Handler{}, t.TempDir(), map[string]string{
		"module_path": "github.com/acme/gm",
		"go_version":  "1.23",
	})

	assertPlanLength(t, plan, 5)
	initIdx := findGoModVerb(t, plan, "init")
	editIdx := findGoModVerb(t, plan, "edit")
	tidyIdx := findGoModVerb(t, plan, "tidy")
	if initIdx >= editIdx || editIdx >= tidyIdx {
		t.Fatalf("order init=%d edit=%d tidy=%d, want init < edit < tidy", initIdx, editIdx, tidyIdx)
	}

	editTask := execTaskAt(t, plan, editIdx)
	want := []string{"mod", "edit", "-go=1.23"}
	if strings.Join(editTask.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("edit args = %v, want %v", editTask.Args, want)
	}
	if !strings.Contains(editTask.Desc, "1.23") {
		t.Errorf("edit description %q should contain the version", editTask.Desc)
	}
	assertStringSet(t, "go mod edit", editTask.PredictedCreated, []string{"go.mod"})
}

func TestGetTasks_NoEditTaskWithoutExplicitVersion(t *testing.T) {
	cases := []struct {
		name string
		vars map[string]string
	}{
		{name: "nil", vars: nil},
		{name: "empty_map", vars: map[string]string{}},
		{name: "empty_version", vars: map[string]string{"go_version": ""}},
		{name: "module_only", vars: map[string]string{"module_path": "github.com/acme/gm"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := getPlanWithVarsOrFail(t, &Handler{}, t.TempDir(), tc.vars)
			assertNoGoModVerb(t, plan, "edit")
			assertPlanLength(t, plan, 4)
		})
	}
}

func TestGetTasks_ExistingProjectWithExplicitVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := getPlanWithVarsOrFail(t, &Handler{}, dir, map[string]string{"go_version": "1.23"})

	assertPlanLength(t, plan, 4)
	assertNoGoModVerb(t, plan, "init")
	editIdx := findGoModVerb(t, plan, "edit")
	tidyIdx := findGoModVerb(t, plan, "tidy")
	if editIdx == -1 || tidyIdx == -1 || editIdx >= tidyIdx {
		t.Fatalf("edit index=%d tidy index=%d, want edit before tidy", editIdx, tidyIdx)
	}
	assertStringSet(t, "go mod edit", execTaskAt(t, plan, editIdx).PredictedCreated, []string{"go.mod"})
}

func TestGetTasks_ExistingModulePathEqualIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/acme/gm\n\ngo 1.23\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan := getPlanWithVarsOrFail(t, &Handler{}, dir, map[string]string{
		"module_path": "github.com/acme/gm",
	})
	assertPlanLength(t, plan, 3)
	assertNoGoModVerb(t, plan, "init")
	assertNoGoModVerb(t, plan, "edit")
}

// TestGetTasks_QuotedExistingModulePathIsNoOp covers go.mod files that spell
// the module path as a double-quoted interpreted string: Go accepts that form,
// so equality must be compared after unquoting.
func TestGetTasks_QuotedExistingModulePathIsNoOp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module \"example.com/quoted\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Assert the parsed directive directly; checking for a missing init task
	// alone is weak because init is never planned when go.mod already exists.
	got, err := moduleDirective(dir)
	if err != nil {
		t.Fatalf("moduleDirective failed for a double-quoted path: %v", err)
	}
	if got != "example.com/quoted" {
		t.Fatalf("moduleDirective = %q, want %q", got, "example.com/quoted")
	}

	plan := getPlanWithVarsOrFail(t, &Handler{}, dir, map[string]string{
		"module_path": "example.com/quoted",
	})
	assertNoGoModVerb(t, plan, "init")
	if findGoModVerb(t, plan, "tidy") == -1 {
		t.Fatal("maintenance plan is missing the go mod tidy task")
	}
}

// TestGetTasks_BacktickModulePathErrors covers go.mod files that spell the
// module path with backticks. Go's modfile parser rejects raw-string quoting,
// so scbake rejects it at plan time instead of planning a tidy that would fail.
func TestGetTasks_BacktickModulePathErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module `example.com/backtick`\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := (&Handler{}).GetTasks(dir, "", "", map[string]string{
		"module_path": "example.com/backtick",
	})
	if err == nil {
		t.Fatalf("expected backtick-quoted module path to fail, got plan %v", plan)
	}
	for _, want := range []string{"malformed module directive", "backtick quoting"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want mention of %q", err, want)
		}
	}
}

func TestGetTasks_MalformedQuotedModuleDirectiveErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module \"example.com/unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := (&Handler{}).GetTasks(dir, "", "", map[string]string{
		"module_path": "example.com/unterminated",
	})
	if err == nil {
		t.Fatalf("expected malformed quoting to fail, got plan %v", plan)
	}
	if !strings.Contains(err.Error(), "malformed module directive") {
		t.Errorf("error = %v, want mention of a malformed module directive", err)
	}
}

func TestGetTasks_ExistingModulePathDifferentErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := (&Handler{}).GetTasks(dir, "", "", map[string]string{
		"module_path": "github.com/acme/other",
	})
	if err == nil {
		t.Fatalf("expected error when module_path differs from the existing module, got plan %v", plan)
	}
	if plan != nil {
		t.Errorf("expected no tasks on error, got %v", plan)
	}
	for _, want := range []string{"example.com/existing", "will not rewrite", "go mod edit -module="} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want mention of %q", err, want)
		}
	}
}

func TestGetTasks_UnparsableGoModWithOverrideErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("// only a comment\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	plan, err := (&Handler{}).GetTasks(dir, "", "", map[string]string{
		"module_path": "github.com/acme/gm",
	})
	if err == nil {
		t.Fatalf("expected error for go.mod without a module directive, got plan %v", plan)
	}
	if !strings.Contains(err.Error(), "module directive") {
		t.Errorf("error = %v, want mention of a missing module directive", err)
	}
}

func TestGetTasks_PriorityBandWithVars(t *testing.T) {
	plan := getPlanWithVarsOrFail(t, &Handler{}, t.TempDir(), map[string]string{
		"module_path": "github.com/acme/gm",
		"go_version":  "1.23",
	})
	for _, task := range plan {
		p := task.Priority()
		if p < int(types.PrioLangSetup) || p > int(types.MaxLangSetup) {
			t.Errorf("task %q has priority %d outside band [%d, %d]",
				task.Description(), p, types.PrioLangSetup, types.MaxLangSetup)
		}
	}
}
