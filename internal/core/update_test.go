// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"path/filepath"
	"reflect"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"strings"
	"testing"
)

// stepRecordingReporter records high-level steps so a test can assert a run's
// exact progress sequence without depending on the concrete UI reporter.
type stepRecordingReporter struct {
	steps []string
}

func (r *stepRecordingReporter) Step(_, message string) { r.steps = append(r.steps, message) }

func (r *stepRecordingReporter) SetTotalSteps(int) {}

func (r *stepRecordingReporter) TaskStart(string, int, int) {}

func (r *stepRecordingReporter) TaskEnd(error) {}

// TestRunUpdateEmitsManifestLoadOnce is a regression guard for #271: update
// loaded the manifest twice and emitted six steps for a five-step denominator.
func TestRunUpdateEmitsManifestLoadOnce(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{
		SbakeVersion: manifest.DefaultSbakeVersion,
		Templates:    []types.Template{{Name: "editorconfig", Path: "."}},
	}
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("write fixture manifest: %v", err)
	}

	reporter := &stepRecordingReporter{}
	rc := RunContext{
		TargetPath:       root,
		ManifestPathArg:  ".",
		ConflictStrategy: types.ConflictMerge,
	}
	if err := RunUpdate(rc, reporter); err != nil {
		t.Fatalf("RunUpdate failed: %v", err)
	}

	want := []string{
		"Loading manifest (" + fileutil.ManifestFileName + ")...",
		"Building execution plan...",
		"Executing plan...",
		"Updating manifest...",
		"Committing transaction...",
	}
	if !reflect.DeepEqual(reporter.steps, want) {
		t.Fatalf("steps = %#v, want %#v", reporter.steps, want)
	}

	loads := 0
	for _, step := range reporter.steps {
		if strings.Contains(step, "Loading manifest") {
			loads++
		}
	}
	if loads != 1 {
		t.Errorf("RunUpdate emitted %d manifest-load steps, want 1", loads)
	}
}

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
