// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"path/filepath"
	"reflect"
	"scbake/internal/types"
	"scbake/pkg/tasks"
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
