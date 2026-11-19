// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cighub

import (
	"strings"
	"testing"

	"scbake/internal/types"
)

// TestWorkflow_GoVersionFileIsSingleSourced is the regression guard for #224:
// without an explicit go_version the workflow must read the version from the
// first Go project's go.mod instead of pinning a stale schema default.
func TestWorkflow_GoVersionFileIsSingleSourced(t *testing.T) {
	cases := []struct {
		name     string
		metadata map[string]string
		projects []types.Project
		want     string
	}{
		{
			name:     "nested_project",
			projects: []types.Project{{Name: "backend", Path: "services/backend", Language: "go"}},
			want:     "\n          go-version-file: \"services/backend/go.mod\"",
		},
		{
			name:     "root_project",
			projects: []types.Project{{Name: "demo", Path: ".", Language: "go"}},
			want:     "\n          go-version-file: \"go.mod\"",
		},
		{
			name: "first_go_project_after_non_go",
			projects: []types.Project{
				{Name: "web", Path: "web", Language: "svelte"},
				{Name: "backend", Path: "services/backend", Language: "go"},
				{Name: "worker", Path: "services/worker", Language: "go"},
			},
			want: "\n          go-version-file: \"services/backend/go.mod\"",
		},
		{
			name:     "empty_metadata_value",
			metadata: map[string]string{"go_version": ""},
			projects: []types.Project{{Name: "backend", Path: "backend", Language: "go"}},
			want:     "\n          go-version-file: \"backend/go.mod\"",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := renderWorkflow(t, &types.Manifest{Projects: tc.projects, Metadata: tc.metadata})

			if !strings.Contains(out, tc.want) {
				t.Errorf("workflow missing %q:\n%s", tc.want, out)
			}
			if strings.Contains(out, `go-version: "1.22"`) {
				t.Errorf("workflow still pins the removed schema default:\n%s", out)
			}
			if strings.Contains(out, "go-version:") {
				t.Errorf("workflow must not pin a version that was not set:\n%s", out)
			}
		})
	}
}

// TestWorkflow_ExplicitGoVersionPinsVersion verifies that an explicit --set
// value still wins over go.mod.
func TestWorkflow_ExplicitGoVersionPinsVersion(t *testing.T) {
	out := renderWorkflow(t, &types.Manifest{
		Projects: []types.Project{{Name: "backend", Path: "services/backend", Language: "go"}},
		Metadata: map[string]string{"go_version": "1.27.3"},
	})

	if !strings.Contains(out, "\n          go-version: \"1.27.3\"") {
		t.Errorf("workflow missing the explicit go-version:\n%s", out)
	}
	if strings.Contains(out, "go-version-file:") {
		t.Errorf("explicit go_version must not render go-version-file:\n%s", out)
	}
}

// TestWorkflow_NonGoProjectHasNoGoSetup pins that non-Go manifests keep their
// existing setup blocks and gain no Go setup step.
func TestWorkflow_NonGoProjectHasNoGoSetup(t *testing.T) {
	out := renderWorkflow(t, &types.Manifest{
		Projects: []types.Project{
			{Name: "web", Path: "web", Language: "svelte"},
			{Name: "api", Path: "api", Language: "spring"},
		},
	})

	for _, unwanted := range []string{"Setup Go", "actions/setup-go", "go-version"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("non-Go workflow unexpectedly contains %q:\n%s", unwanted, out)
		}
	}
	for _, want := range []string{"actions/setup-node@v4", "actions/setup-java@v4"} {
		if !strings.Contains(out, want) {
			t.Errorf("non-Go workflow missing %q:\n%s", want, out)
		}
	}
}
