// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package svelte

import (
	"path/filepath"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// TestGetTasks_ExecPredictedPaths asserts the paths declared for the opaque
// npm install and npm pkg set commands. create-vite runs as a ScaffoldTask,
// which tracks its own output instead of declaring predictions.
func TestGetTasks_ExecPredictedPaths(t *testing.T) {
	plan := getPlanOrFail(t, &Handler{}, filepath.Join(t.TempDir(), "app"))
	assertNewProjectPlanLength(t, plan)

	cases := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "npm install",
			args: []string{"install"},
			want: []string{"package.json", "package-lock.json", "node_modules"},
		},
		{
			name: "npm pkg set",
			args: []string{"pkg", "set"},
			want: []string{"package.json"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			task := findSvelteExecTask(t, plan, tt.args)
			assertSvelteStringSet(t, tt.name, task.PredictedCreated, tt.want)
		})
	}
}

func findSvelteExecTask(t *testing.T, plan []types.Task, prefix []string) *tasks.ExecCommandTask {
	t.Helper()
	for _, task := range plan {
		execTask, ok := task.(*tasks.ExecCommandTask)
		if !ok || len(execTask.Args) < len(prefix) {
			continue
		}
		matches := true
		for i, want := range prefix {
			if execTask.Args[i] != want {
				matches = false
				break
			}
		}
		if matches {
			return execTask
		}
	}
	t.Fatalf("no exec task with args prefix %v", prefix)
	return nil
}

func assertSvelteStringSet(t *testing.T, what string, got, want []string) {
	t.Helper()
	gotSet := make(map[string]struct{}, len(got))
	for _, path := range got {
		gotSet[path] = struct{}{}
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, path := range want {
		wantSet[path] = struct{}{}
	}
	if len(gotSet) != len(wantSet) {
		t.Fatalf("%s predictions = %v, want set %v", what, got, want)
	}
	for path := range wantSet {
		if _, ok := gotSet[path]; !ok {
			t.Fatalf("%s predictions = %v, missing %q", what, got, path)
		}
	}
	for path := range gotSet {
		if _, ok := wantSet[path]; !ok {
			t.Fatalf("%s predictions = %v, unexpected %q", what, got, path)
		}
	}
}
