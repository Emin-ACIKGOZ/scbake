// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package golang

import (
	"os"
	"path/filepath"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// TestGetTasks_ExecPredictedPaths asserts that every Go module command declares
// the files it may create or rewrite, so rollback can restore an existing
// go.mod/go.sum and remove newly created ones.
func TestGetTasks_ExecPredictedPaths(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		count   int
		initSet []string
		tidySet []string
	}{
		{
			name:    "new project",
			count:   4,
			initSet: []string{"go.mod"},
			tidySet: []string{"go.mod", "go.sum"},
		},
		{
			name: "existing project",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/existing\n"), 0o600); err != nil {
					t.Fatalf("seed go.mod: %v", err)
				}
			},
			count:   3,
			tidySet: []string{"go.mod", "go.sum"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.setup != nil {
				tt.setup(t, dir)
			}
			plan := getPlanOrFail(t, &Handler{}, dir)
			assertPlanLength(t, plan, tt.count)

			initTask, tidyTask := findGoModuleTasks(plan)
			if tt.initSet == nil {
				if initTask != nil {
					t.Fatalf("unexpected go mod init task: %v", initTask.Args)
				}
			} else {
				if initTask == nil {
					t.Fatal("missing go mod init task")
				}
				assertStringSet(t, "go mod init", initTask.PredictedCreated, tt.initSet)
			}

			if tidyTask == nil {
				t.Fatal("missing go mod tidy task")
			}
			assertStringSet(t, "go mod tidy", tidyTask.PredictedCreated, tt.tidySet)
		})
	}
}

func findGoModuleTasks(plan []types.Task) (initTask, tidyTask *tasks.ExecCommandTask) {
	for _, task := range plan {
		execTask, ok := task.(*tasks.ExecCommandTask)
		if !ok || len(execTask.Args) < 2 || execTask.Args[0] != "mod" {
			continue
		}
		switch execTask.Args[1] {
		case "init":
			initTask = execTask
		case "tidy":
			tidyTask = execTask
		}
	}
	return initTask, tidyTask
}

// assertStringSet compares two path lists as sets, so declaration order is not
// part of the contract.
func assertStringSet(t *testing.T, what string, got, want []string) {
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
