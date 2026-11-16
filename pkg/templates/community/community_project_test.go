// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package community

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"strings"
	"testing"
)

// Issue #168: the community CONTRIBUTING template must name the project that
// owns the target directory, not simply the first project in the manifest.

// contributingTask returns the CONTRIBUTING.md task from the community plan.
func contributingTask(t *testing.T) types.Task {
	t.Helper()

	plan, err := (&Handler{}).GetTasks(".", "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	for _, task := range plan {
		if task.Description() == "Create CONTRIBUTING.md" {
			return task
		}
	}
	t.Fatal("community plan has no CONTRIBUTING.md task")
	return nil
}

// contributingHeading renders CONTRIBUTING.md into tc.TargetPath and returns
// its first line. The first line carries the project name the template chose.
func contributingHeading(t *testing.T, task types.Task, tc types.TaskContext) string {
	t.Helper()

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute(CONTRIBUTING.md) failed: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(tc.TargetPath, "CONTRIBUTING.md"))
	if err != nil {
		t.Fatalf("reading CONTRIBUTING.md: %v", err)
	}
	lines := strings.SplitN(string(content), "\n", 2)
	return lines[0]
}

// monorepoManifest is the two-project fixture. "api" is first on purpose, so a
// template that falls back to Projects[0] cannot pass a "web" assertion.
func monorepoManifest() *types.Manifest {
	return &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "api", Path: "services/api", Language: "go"},
			{Name: "web", Path: "apps/web", Language: "svelte"},
		},
	}
}

// TestCommunityContributingUsesOwningProjectName applies community to each
// subproject of a monorepo and verifies each CONTRIBUTING.md names its own
// project.
func TestCommunityContributingUsesOwningProjectName(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	manifest := monorepoManifest()

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "api", target: filepath.Join(root, "services", "api"), want: "# Contributing to api"},
		{name: "web", target: filepath.Join(root, "apps", "web"), want: "# Contributing to web"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := types.TaskContext{
				Ctx:        context.Background(),
				TargetPath: tt.target,
				Manifest:   manifest,
				Tx:         tx,
			}
			if got := contributingHeading(t, contributingTask(t), tc); got != tt.want {
				t.Errorf("CONTRIBUTING heading = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCommunityContributingUpdateUsesOwningProjectName verifies that a
// manifest-driven update of each subproject recreates CONTRIBUTING.md with the
// correct project name rather than the first manifest project's name.
func TestCommunityContributingUpdateUsesOwningProjectName(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	manifest := monorepoManifest()
	task := contributingTask(t)

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "api", target: filepath.Join(root, "services", "api"), want: "# Contributing to api"},
		{name: "web", target: filepath.Join(root, "apps", "web"), want: "# Contributing to web"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := types.TaskContext{
				Ctx:        context.Background(),
				TargetPath: tt.target,
				Manifest:   manifest,
				Tx:         tx,
			}
			if got := contributingHeading(t, task, tc); got != tt.want {
				t.Fatalf("initial CONTRIBUTING heading = %q, want %q", got, tt.want)
			}

			// Remove the managed output so update has to render it again. This
			// is the path that used to re-derive the name from Projects[0].
			if err := os.Remove(filepath.Join(tt.target, "CONTRIBUTING.md")); err != nil {
				t.Fatalf("removing CONTRIBUTING.md: %v", err)
			}

			tc.Update = true
			if got := contributingHeading(t, task, tc); got != tt.want {
				t.Errorf("updated CONTRIBUTING heading = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCommunityContributingProjectFallbacks pins the documented fallback
// behavior of the template: when the target has no owning manifest project,
// CONTRIBUTING.md keeps the legacy Projects[0] name, and an empty manifest
// yields the generic "this project" heading.
func TestCommunityContributingProjectFallbacks(t *testing.T) {
	tests := []struct {
		name     string
		projects []types.Project
		target   func(root string) string
		want     string
	}{
		{
			name: "root-level apply falls back to first project",
			projects: []types.Project{
				{Name: "api", Path: "services/api", Language: "go"},
				{Name: "web", Path: "apps/web", Language: "svelte"},
			},
			target: func(root string) string { return root },
			want:   "# Contributing to api",
		},
		{
			name: "target without a manifest project entry falls back to first project",
			projects: []types.Project{
				{Name: "api", Path: "services/api", Language: "go"},
				{Name: "web", Path: "apps/web", Language: "svelte"},
			},
			target: func(root string) string { return filepath.Join(root, "tools", "releaser") },
			want:   "# Contributing to api",
		},
		{
			name:     "manifest without projects uses generic heading",
			projects: nil,
			target:   func(root string) string { return root },
			want:     "# Contributing to this project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tx, err := transaction.New(root)
			if err != nil {
				t.Fatalf("transaction.New() failed: %v", err)
			}
			manifest := &types.Manifest{SbakeVersion: "v1.0.0", Projects: tt.projects}
			tc := types.TaskContext{
				Ctx:        context.Background(),
				TargetPath: tt.target(root),
				Manifest:   manifest,
				Tx:         tx,
			}
			if got := contributingHeading(t, contributingTask(t), tc); got != tt.want {
				t.Errorf("CONTRIBUTING heading = %q, want %q", got, tt.want)
			}
		})
	}
}
