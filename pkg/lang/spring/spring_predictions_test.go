// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package spring

import (
	"path/filepath"
	"testing"

	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// TestGetTasks_ExecPredictedPaths asserts that the download, extraction, and
// chmod steps declare the files and archive they act on.
func TestGetTasks_ExecPredictedPaths(t *testing.T) {
	target := filepath.Join(t.TempDir(), "demo-app")
	plan := getPlanOrFail(t, &Handler{}, target)

	curlTask := findSpringExecTask(t, plan, "curl")
	assertSpringStringSet(t, "curl", curlTask.PredictedCreated, []string{"spring-init.zip"})
	if curlTask.ArchivePath != "" {
		t.Errorf("curl task unexpectedly declares ArchivePath %q", curlTask.ArchivePath)
	}

	unzipTask := findSpringExecTask(t, plan, "unzip")
	if unzipTask.ArchivePath != "spring-init.zip" {
		t.Errorf("unzip ArchivePath = %q, want %q", unzipTask.ArchivePath, "spring-init.zip")
	}
	if len(unzipTask.PredictedCreated) != 0 {
		t.Errorf("unzip should rely on ArchivePath, got predictions %v", unzipTask.PredictedCreated)
	}

	chmodTask := findSpringExecTask(t, plan, "chmod")
	assertSpringStringSet(t, "chmod", chmodTask.PredictedCreated, []string{"mvnw"})
}

func findSpringExecTask(t *testing.T, plan []types.Task, cmd string) *tasks.ExecCommandTask {
	t.Helper()
	for _, task := range plan {
		execTask, ok := task.(*tasks.ExecCommandTask)
		if ok && execTask.Cmd == cmd {
			return execTask
		}
	}
	t.Fatalf("no exec task for command %q", cmd)
	return nil
}

func assertSpringStringSet(t *testing.T, what string, got, want []string) {
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
