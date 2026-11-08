// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"testing"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
)

// TestGitHandler_PredictedPathsUnchanged pins the git predictions: the handler
// keeps predicting .git (and the repository-local exclude file after init) and
// must not start tracking the working tree.
func TestGitHandler_PredictedPathsUnchanged(t *testing.T) {
	h := &Handler{}
	plan, err := h.GetTasks(t.TempDir(), "", "")
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}

	cases := []struct {
		name   string
		marker string
		want   []string
	}{
		{
			name:   "init",
			marker: "init",
			want:   []string{fileutil.GitDir, internalStateIgnorePath},
		},
		{
			// The pre-existing behavior is unchanged: init already tracked
			// .git before `git add` runs, so add declares no paths of its own.
			name:   "add",
			marker: "add",
			want:   nil,
		},
		{
			name:   "commit",
			marker: "commit",
			want:   []string{fileutil.GitDir},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			task := findGitExecTask(t, plan, tt.marker)
			if task.Cmd != "git" {
				t.Fatalf("command = %q, want git", task.Cmd)
			}
			assertGitStringSet(t, tt.name, task.PredictedCreated, tt.want)
		})
	}
}

func findGitExecTask(t *testing.T, plan []types.Task, marker string) *tasks.ExecCommandTask {
	t.Helper()
	var found *tasks.ExecCommandTask
	count := 0
	for _, task := range plan {
		execTask, ok := task.(*tasks.ExecCommandTask)
		if !ok || execTask.Cmd != "git" {
			continue
		}
		for _, arg := range execTask.Args {
			if arg == marker {
				found = execTask
				count++
				break
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one git task containing %q, found %d", marker, count)
	}
	return found
}

func assertGitStringSet(t *testing.T, what string, got, want []string) {
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
