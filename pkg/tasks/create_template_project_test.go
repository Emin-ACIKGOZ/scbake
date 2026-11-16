// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"strings"
	"testing"
)

// Issue #168: templates that name the owning project need render data that is
// scoped to the task's target directory, not merely the first manifest project.
//
//go:embed testdata/project_owner.tpl testdata/legacy_render_data.tpl
var projectRenderTemplates embed.FS

// monorepoManifest is the two-project fixture used by the render tests. Its
// first project is deliberately "api" so a template that falls back to
// Projects[0] cannot accidentally pass a "web" assertion.
func monorepoManifest() *types.Manifest {
	return &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects: []types.Project{
			{Name: "api", Path: "services/api", Language: "go"},
			{Name: "web", Path: "apps/web", Language: "svelte"},
		},
	}
}

// renderProjectOwner executes the project_owner.tpl fixture for one target and
// returns its trimmed output.
func renderProjectOwner(t *testing.T, tc types.TaskContext) string {
	t.Helper()

	task := &CreateTemplateTask{
		TemplateFS:   projectRenderTemplates,
		TemplatePath: "testdata/project_owner.tpl",
		OutputPath:   "owner.txt",
		Desc:         "Render owning project",
		TaskPrio:     100,
	}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tc.TargetPath, "owner.txt"))
	if err != nil {
		t.Fatalf("reading rendered output: %v", err)
	}
	return strings.TrimSpace(string(content))
}

// TestCreateTemplateTask_RendersOwningProject verifies that a template can name
// the manifest project that owns the target directory, on both a first render
// and a manifest-driven update re-render.
func TestCreateTemplateTask_RendersOwningProject(t *testing.T) {
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
		{
			name:   "api",
			target: filepath.Join(root, "services", "api"),
			want:   "owner=api first=api",
		},
		{
			name:   "web",
			target: filepath.Join(root, "apps", "web"),
			want:   "owner=web first=api",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := types.TaskContext{
				Ctx:        context.Background(),
				TargetPath: tt.target,
				Manifest:   manifest,
				Tx:         tx,
			}

			if got := renderProjectOwner(t, tc); got != tt.want {
				t.Errorf("render = %q, want %q", got, tt.want)
			}

			// `update` replays the same task with Update set. The owning
			// project must remain stable rather than falling back to the first
			// manifest project.
			tc.Update = true
			if got := renderProjectOwner(t, tc); got != tt.want {
				t.Errorf("update render = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCreateTemplateTask_NoTransactionFallsBackToFirstProject pins the
// documented fallback when TaskContext.Tx is nil: without a transaction root,
// currentProject cannot resolve the manifest-root-relative project path, so
// .Project stays nil and templates keep their legacy Projects[0] behavior.
//
// Direct CreateTemplateTask execution (unit/embedding callers) has no CLI
// transaction, so this is the supported degraded mode. Templates must guard
// .Project, as project_owner.tpl does.
func TestCreateTemplateTask_NoTransactionFallsBackToFirstProject(t *testing.T) {
	root := t.TempDir()
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: filepath.Join(root, "apps", "web"),
		Manifest:   monorepoManifest(),
		// Tx is deliberately unset.
	}

	// project_owner.tpl renders "owner=none" when .Project is nil, and its
	// "first=" field proves the legacy Projects[0] fallback still yields api.
	got := renderProjectOwner(t, tc)
	want := "owner=none first=api"
	if got != want {
		t.Errorf("render without Tx = %q, want %q (.Project must be nil and fall back to Projects[0])", got, want)
	}
}

// TestCreateTemplateTask_LegacyManifestDataStillAvailable is the compatibility
// guard for issue #168: the project-scoped wrapper must keep every legacy
// template field (Projects, Metadata, SbakeVersion, ...) available. A custom
// TemplateData override must also still take precedence over the wrapper.
func TestCreateTemplateTask_LegacyManifestDataStillAvailable(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	manifest := monorepoManifest()
	manifest.Metadata = map[string]string{"license": "Apache-2.0"}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: filepath.Join(root, "services", "api"),
		Manifest:   manifest,
		Tx:         tx,
	}

	task := &CreateTemplateTask{
		TemplateFS:   projectRenderTemplates,
		TemplatePath: "testdata/legacy_render_data.tpl",
		OutputPath:   "legacy.txt",
		Desc:         "Render legacy data",
		TaskPrio:     100,
	}
	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(tc.TargetPath, "legacy.txt"))
	if err != nil {
		t.Fatalf("reading rendered output: %v", err)
	}
	got := strings.TrimSpace(string(content))
	want := "projects=api;web; license=Apache-2.0 version=v1.0.0"
	if got != want {
		t.Errorf("legacy render = %q, want %q", got, want)
	}

	// A custom TemplateData override remains authoritative; adding the render
	// wrapper must not silently replace it.
	override := &CreateTemplateTask{
		TemplateFS:   projectRenderTemplates,
		TemplatePath: "testdata/legacy_render_data.tpl",
		OutputPath:   "override.txt",
		TemplateData: struct {
			SbakeVersion string
			Projects     []types.Project
			Metadata     map[string]string
		}{
			SbakeVersion: "v9.9.9",
			Projects:     []types.Project{{Name: "override"}},
			Metadata:     map[string]string{"license": "MIT"},
		},
		Desc:     "Render override data",
		TaskPrio: 100,
	}
	if err := override.Execute(tc); err != nil {
		t.Fatalf("override Execute() failed: %v", err)
	}
	overrideContent, err := os.ReadFile(filepath.Join(tc.TargetPath, "override.txt"))
	if err != nil {
		t.Fatalf("reading override output: %v", err)
	}
	got = strings.TrimSpace(string(overrideContent))
	want = "projects=override; license=MIT version=v9.9.9"
	if got != want {
		t.Errorf("override render = %q, want %q", got, want)
	}
}
