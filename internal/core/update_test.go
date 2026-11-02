// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"scbake/internal/types"
	"scbake/internal/ui"
	"scbake/pkg/tasks"
	"scbake/pkg/templates/compliance"
	"testing"
)

// TestResolveUpdateSelection verifies I6: the manifest reproduces exactly the
// selection that `update` re-applies, scoped by normalized path so monorepo
// subprojects do not alias each other.
func TestResolveUpdateSelection(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{
		Projects: []types.Project{
			{Name: "api", Path: "services/api", Language: "go"},
			{Name: "web", Path: "services/web", Language: "svelte"},
		},
		Templates: []types.Template{
			{Name: "makefile", Path: "services/api"},
			{Name: "makefile", Path: "services/api"}, // duplicate is deduplicated
			{Name: "editorconfig", Path: "."},
			{Name: "ci_github", Path: "services/api"},
		},
	}

	t.Run("subproject", func(t *testing.T) {
		lang, with, rel, err := resolveUpdateSelection(m, root, filepath.Join(root, "services", "api"))
		if err != nil {
			t.Fatalf("resolveUpdateSelection: %v", err)
		}
		if lang != "go" {
			t.Errorf("lang = %q, want go", lang)
		}
		if rel != "services/api" {
			t.Errorf("rel = %q, want services/api", rel)
		}
		if want := []string{"makefile", "ci_github"}; !reflect.DeepEqual(with, want) {
			t.Errorf("with = %v, want %v", with, want)
		}
	})

	t.Run("root", func(t *testing.T) {
		lang, with, rel, err := resolveUpdateSelection(m, root, root)
		if err != nil {
			t.Fatalf("resolveUpdateSelection: %v", err)
		}
		if lang != "" {
			t.Errorf("lang = %q, want empty", lang)
		}
		if rel != "." {
			t.Errorf("rel = %q, want .", rel)
		}
		if want := []string{"editorconfig"}; !reflect.DeepEqual(with, want) {
			t.Errorf("with = %v, want %v", with, want)
		}
	})
}

// TestManagedWriteTasks verifies that `update` executes only managed file
// writes and never re-runs side-effecting commands (F5).
func TestManagedWriteTasks(t *testing.T) {
	write := &tasks.CreateTemplateTask{OutputPath: "Makefile"}
	exec := &tasks.ExecCommandTask{Cmd: "go"}

	got := managedWriteTasks([]types.Task{exec, write})
	if len(got) != 1 {
		t.Fatalf("managedWriteTasks returned %d tasks, want 1", len(got))
	}
	if got[0] != types.Task(write) {
		t.Fatalf("managedWriteTasks kept the wrong task: %T", got[0])
	}
}

// TestManagedWriteTasks_KeepsLicenseTask is a regression test for #142: update
// and drift --repair dropped LicenseTask, so a deleted LICENSE was never restored.
func TestManagedWriteTasks_KeepsLicenseTask(t *testing.T) {
	license := &compliance.LicenseTask{TaskPrio: 100}
	exec := &tasks.ExecCommandTask{Cmd: "go"}

	got := managedWriteTasks([]types.Task{exec, license})
	if len(got) != 1 {
		t.Fatalf("managedWriteTasks returned %d tasks, want 1 (LicenseTask kept)", len(got))
	}
	if got[0] != types.Task(license) {
		t.Fatalf("managedWriteTasks kept the wrong task: %T", got[0])
	}
}

// runManagedWrites executes the tasks that `update` keeps, the same way
// `update` does, against root with the given conflict strategy.
func runManagedWrites(t *testing.T, root string, m *types.Manifest, strategy string, plan []types.Task) error {
	t.Helper()
	tc := types.TaskContext{
		Ctx:              context.Background(),
		TargetPath:       root,
		Manifest:         m,
		ConflictStrategy: strategy,
	}
	reporter := ui.NewPlainReporter(1, false)
	return Execute(&types.Plan{Tasks: managedWriteTasks(plan)}, tc, reporter)
}

// TestUpdateRestoresDeletedLicense is the end-to-end regression for #142: after
// LICENSE is deleted, the update/repair path must select LicenseTask and recreate it.
func TestUpdateRestoresDeletedLicense(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{Metadata: map[string]string{"license": "MIT", "copyright_holder": "Acme"}}
	plan := []types.Task{
		&tasks.ExecCommandTask{Cmd: "go"},
		&compliance.LicenseTask{TaskPrio: 100},
	}
	// Create LICENSE directly, as a prior `apply` would, then delete it.
	if err := (&compliance.LicenseTask{TaskPrio: 100}).Execute(types.TaskContext{
		Ctx: context.Background(), TargetPath: root, Manifest: m,
	}); err != nil {
		t.Fatalf("initial LICENSE write failed: %v", err)
	}
	licensePath := filepath.Join(root, "LICENSE")
	if err := os.Remove(licensePath); err != nil {
		t.Fatal(err)
	}
	if err := runManagedWrites(t, root, m, types.ConflictFail, plan); err != nil {
		t.Fatalf("repair write failed: %v", err)
	}
	if _, err := os.Stat(licensePath); err != nil {
		t.Fatalf("LICENSE was not restored: %v", err)
	}
}

// TestUpdateLicenseHandEditFollowsConflictStrategy verifies that a hand-edited
// LICENSE is handled by the same conflict strategies as other managed files.
func TestUpdateLicenseHandEditFollowsConflictStrategy(t *testing.T) {
	const edited = "my own license text\n"
	plan := []types.Task{&compliance.LicenseTask{TaskPrio: 100}}

	tests := []struct {
		strategy string
		wantErr  bool
		want     string // expected LICENSE content; empty means "not the edit"
	}{
		{types.ConflictFail, true, edited},
		{types.ConflictKeepLocal, false, edited},
		{types.ConflictOverwrite, false, ""},
	}
	for _, tt := range tests {
		t.Run(tt.strategy, func(t *testing.T) {
			root := t.TempDir()
			m := &types.Manifest{Metadata: map[string]string{"license": "MIT", "copyright_holder": "Acme"}}
			if err := runManagedWrites(t, root, m, types.ConflictFail, plan); err != nil {
				t.Fatalf("initial write failed: %v", err)
			}
			licensePath := filepath.Join(root, "LICENSE")
			if err := os.WriteFile(licensePath, []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}

			err := runManagedWrites(t, root, m, tt.strategy, plan)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			got, readErr := os.ReadFile(licensePath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if tt.want != "" && string(got) != tt.want {
				t.Fatalf("LICENSE = %q, want %q", got, tt.want)
			}
			if tt.want == "" && string(got) == edited {
				t.Fatalf("LICENSE still holds the hand edit, want overwritten")
			}
		})
	}
}

// TestApplyManifestChangesDeduplicates is a regression guard: the render-time
// manifest must not re-add a project or template that already exists, or
// templates ranging .Projects render every block twice after update.
func TestApplyManifestChangesDeduplicates(t *testing.T) {
	m := &types.Manifest{
		Projects:  []types.Project{{Name: "api", Path: "services/api", Language: "go"}},
		Templates: []types.Template{{Name: "makefile", Path: "."}},
	}
	changes := &manifestChanges{
		Projects: []types.Project{
			{Name: "api", Path: "services/api", Language: "go"}, // duplicate
			{Name: "web", Path: "services/web", Language: "svelte"},
		},
		Templates: []types.Template{
			{Name: "makefile", Path: "."}, // duplicate
			{Name: "ci_github", Path: "."},
		},
	}

	applyManifestChanges(m, changes)
	applyManifestChanges(m, changes) // a second run must not grow the manifest

	if len(m.Projects) != 2 {
		t.Errorf("projects = %d, want 2: %+v", len(m.Projects), m.Projects)
	}
	if len(m.Templates) != 2 {
		t.Errorf("templates = %d, want 2: %+v", len(m.Templates), m.Templates)
	}
}
