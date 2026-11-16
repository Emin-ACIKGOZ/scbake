// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGitHandler_InsideWorkTree_NoTasks pins the handler-level half of #151:
// a target below an existing work tree must not schedule any git task, even
// though the target itself has no `.git` entry.
func TestGitHandler_InsideWorkTree_NoTasks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitEnv(t)

	repo := t.TempDir()
	runInDir(t, repo, "git", "init")
	child := filepath.Join(repo, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	h := &Handler{}
	plan, err := h.GetTasks(child, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	assertNoGitTasks(t, plan)

	executeTaskPlan(t, plan, child)
	if _, statErr := os.Lstat(filepath.Join(child, ".git")); !os.IsNotExist(statErr) {
		t.Errorf("handler created a nested .git (stat error: %v)", statErr)
	}
}

// TestGitHandler_SubdirOfUntrustedRepo is the security regression for the new
// ancestor probe. The repository at the parent contains a malicious pre-commit
// hook, clean filter, and fsmonitor. `git rev-parse --is-inside-work-tree` as
// used by preflight.GitSkipReason is read-only and must not execute any of
// them; the handler must therefore schedule no tasks and leave no marker.
func TestGitHandler_SubdirOfUntrustedRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	isolateGitEnv(t)

	repo := t.TempDir()
	markers := plantUntrustedRepoVectors(t, repo)
	child := filepath.Join(repo, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}

	h := &Handler{}
	plan, err := h.GetTasks(child, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	assertNoGitTasks(t, plan)

	executeTaskPlan(t, plan, child)
	assertMarkersAbsent(t, markers)
}
