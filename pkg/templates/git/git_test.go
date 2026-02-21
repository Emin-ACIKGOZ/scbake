// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"strings"
	"testing"
)

// TestGitHandler_Structure validates the deterministic structural plan and task ordering.
// Logic: Git operations MUST be strictly sequential (Init -> Add -> Commit) with no priority gaps.
func TestGitHandler_Structure(t *testing.T) {
	h := &Handler{}
	plan, err := h.GetTasks("")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	if len(plan) != 3 {
		t.Fatalf("Expected exactly 3 tasks, got %d", len(plan))
	}

	expected := []struct {
		cmd  string
		args []string
	}{
		{"git", []string{"init"}},
		{"git", []string{"add", "."}},
		{"git", []string{"commit", "--allow-empty", "-m", "scbake: Apply templates"}},
	}

	for i, task := range plan {
		execTask := assertExecTask(t, task, i)
		assertCommandIdentity(t, execTask, expected[i], i)
		assertSequentialPriority(t, plan, execTask, i)
		assertPredictedCreated(t, execTask, i)
		assertBandBoundary(t, execTask, i)
	}
}

func assertExecTask(t *testing.T, task types.Task, index int) *tasks.ExecCommandTask {
	t.Helper()
	execTask, ok := task.(*tasks.ExecCommandTask)
	if !ok {
		t.Fatalf("Task index %d is not an *tasks.ExecCommandTask", index)
	}
	return execTask
}

func assertCommandIdentity(t *testing.T, task *tasks.ExecCommandTask, expected struct {
	cmd  string
	args []string
}, index int) {
	t.Helper()
	if task.Cmd != expected.cmd {
		t.Errorf("Task %d: expected command '%s', got '%s'", index, expected.cmd, task.Cmd)
	}
	if !compareSlices(task.Args, expected.args) {
		t.Errorf("Task %d: expected args %v, got %v", index, expected.args, task.Args)
	}
}

func assertSequentialPriority(t *testing.T, plan []types.Task, task *tasks.ExecCommandTask, index int) {
	t.Helper()
	if index == 0 {
		return
	}
	prev := plan[index-1].Priority()
	if task.TaskPrio != prev+1 {
		t.Errorf("Task %d: priority %d is not strictly consecutive to Task %d (%d)",
			index, task.TaskPrio, index-1, prev)
	}
}

func assertPredictedCreated(t *testing.T, task *tasks.ExecCommandTask, index int) {
	t.Helper()
	if index != 0 && index != 2 {
		return
	}
	if len(task.PredictedCreated) == 0 || task.PredictedCreated[0] != fileutil.GitDir {
		t.Errorf("Task %d: missing predicted directory %s", index, fileutil.GitDir)
	}
}

func assertBandBoundary(t *testing.T, task *tasks.ExecCommandTask, index int) {
	t.Helper()
	if task.TaskPrio < int(types.PrioVersionControl) ||
		task.TaskPrio > int(types.MaxVersionControl) {
		t.Errorf("Task %d: priority %d outside VersionControl band [%d, %d]",
			index,
			task.TaskPrio,
			types.PrioVersionControl,
			types.MaxVersionControl,
		)
	}
}

// TestGitTemplate_Fresh verifies the actual interaction with the Git binary from scratch.
// Logic: The handler is responsible for 'git init'. The test only provides the environment.
func TestGitTemplate_Fresh(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "scbake.toml"), []byte(""), 0600); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	h := &Handler{}
	plan, err := h.GetTasks(tmpDir)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
	}

	// Configure identity for test runner
	cmd := exec.Command("git", "config", "--global", "user.email", "test@scbake.dev")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to set git user.email: %v", err)
	}

	cmd = exec.Command("git", "config", "--global", "user.name", "Test User")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to set git user.name: %v", err)
	}

	for _, task := range plan {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Fresh lifecycle failed at task '%s': %v", task.Description(), err)
		}
	}

	logCmd := exec.Command("git", "log", "-1", "--pretty=%B")
	logCmd.Dir = tmpDir
	out, err := logCmd.Output()
	if err != nil {
		t.Fatalf("Failed to read git log: %v", err)
	}

	if !strings.Contains(string(out), "scbake: Apply templates") {
		t.Errorf("Commit message mismatch. Got: %s", string(out))
	}
}

// TestGitTemplate_Idempotent ensures re-running Git tasks on a clean repo is safe.
// Validates that --allow-empty prevents crashes during maintenance runs.
func TestGitTemplate_Idempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpDir := t.TempDir()

	runInDir(t, tmpDir, "git", "init")
	runInDir(t, tmpDir, "git", "config", "user.email", "test@scbake.dev")
	runInDir(t, tmpDir, "git", "config", "user.name", "Test User")

	if err := os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("initial"), 0600); err != nil {
		t.Fatalf("Failed to create init file: %v", err)
	}

	runInDir(t, tmpDir, "git", "add", ".")
	runInDir(t, tmpDir, "git", "commit", "-m", "Initial")

	h := &Handler{}
	plan, err := h.GetTasks(tmpDir)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
	}

	for _, task := range plan {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Idempotent run failed: %v", err)
		}
	}

	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = tmpDir
	status, err := statusCmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git status: %v", err)
	}

	if len(status) > 0 {
		t.Errorf("Repository is dirty after idempotent run:\n%s", string(status))
	}
}

func runInDir(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("Command failed (%s %v): %v", name, args, err)
	}
}

func compareSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
