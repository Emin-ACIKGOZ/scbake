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

func findGoDirectiveEditTask(plan []types.Task) *tasks.ExecCommandTask {
	for _, task := range plan {
		execTask, ok := task.(*tasks.ExecCommandTask)
		if !ok || len(execTask.Args) < 2 || execTask.Args[0] != "mod" {
			continue
		}
		if execTask.Args[1] == "edit" {
			return execTask
		}
	}
	return nil
}

// TestGetTasks_VarsPredictedPaths pins task counts and declared outputs when
// explicit variables add a `go mod edit` task.
func TestGetTasks_VarsPredictedPaths(t *testing.T) {
	cases := []struct {
		name      string
		vars      map[string]string
		seedGoMod string
		count     int
		initSet   []string
		editSet   []string
		tidySet   []string
	}{
		{
			name:    "new project no vars",
			count:   4,
			initSet: []string{"go.mod"},
			tidySet: []string{"go.mod", "go.sum"},
		},
		{
			name:    "new project with version",
			vars:    map[string]string{"module_path": "github.com/acme/gm", "go_version": "1.23"},
			count:   5,
			initSet: []string{"go.mod"},
			editSet: []string{"go.mod"},
			tidySet: []string{"go.mod", "go.sum"},
		},
		{
			name:      "existing project no vars",
			seedGoMod: "module example.com/existing\n",
			count:     3,
			tidySet:   []string{"go.mod", "go.sum"},
		},
		{
			name:      "existing project with version",
			seedGoMod: "module example.com/existing\n",
			vars:      map[string]string{"go_version": "1.23"},
			count:     4,
			editSet:   []string{"go.mod"},
			tidySet:   []string{"go.mod", "go.sum"},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.seedGoMod != "" {
				if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tt.seedGoMod), 0o600); err != nil {
					t.Fatalf("seed go.mod: %v", err)
				}
			}

			plan := getPlanWithVarsOrFail(t, &Handler{}, dir, tt.vars)
			assertPlanLength(t, plan, tt.count)

			initTask, tidyTask := findGoModuleTasks(plan)
			editTask := findGoDirectiveEditTask(plan)

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

			if tt.editSet == nil {
				if editTask != nil {
					t.Fatalf("unexpected go mod edit task: %v", editTask.Args)
				}
			} else {
				if editTask == nil {
					t.Fatal("missing go mod edit task")
				}
				assertStringSet(t, "go mod edit", editTask.PredictedCreated, tt.editSet)
			}

			if tidyTask == nil {
				t.Fatal("missing go mod tidy task")
			}
			assertStringSet(t, "go mod tidy", tidyTask.PredictedCreated, tt.tidySet)
		})
	}
}
