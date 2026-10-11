// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"strings"
	"testing"
	"time"
)

// TestGitHandler_Structure validates the deterministic structural plan and task ordering
// for a fresh target (no pre-existing .git).
// Logic: Git operations MUST be strictly sequential (Init -> Add -> Commit) with no priority gaps.
func TestGitHandler_Structure(t *testing.T) {
	tmpDir := t.TempDir()

	h := &Handler{}
	plan, err := h.GetTasks(tmpDir, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	if len(plan) != 3 {
		t.Fatalf("Expected exactly 3 tasks, got %d", len(plan))
	}

	noHooks := "core.hooksPath=" + os.DevNull
	expected := []struct {
		cmd  string
		args []string
	}{
		{"git", []string{"init"}},
		{"git", []string{"-c", noHooks, "add", "."}},
		{"git", []string{"-c", noHooks, "commit", "--allow-empty", "-m", "scbake: Apply templates"}},
	}

	for i, task := range plan {
		execTask := assertExecTask(t, task, i)
		assertCommandIdentity(t, execTask, expected[i], i)
		assertSequentialPriority(t, plan, execTask, i)
		assertPredictedCreated(t, execTask, i)
		assertBandBoundary(t, execTask, i)
	}
}

// TestGitHandler_ExistingRepo_Structure validates that when the target already
// contains a .git directory the plan is fully fail-closed: NO Git tasks at all
// are scheduled, so no `git init`, `git add`, or `git commit` can execute
// attacker-controlled repository config (hooks, filters, fsmonitor, gpg).
func TestGitHandler_ExistingRepo_Structure(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, fileutil.GitDir), 0700); err != nil {
		t.Fatalf("failed to create fake .git: %v", err)
	}

	h := &Handler{}
	plan, err := h.GetTasks(tmpDir, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	if len(plan) != 0 {
		t.Fatalf("Expected no tasks for an existing repo, got %d: %v", len(plan), plan)
	}

	for i, task := range plan {
		if execTask, ok := task.(*tasks.ExecCommandTask); ok && execTask.Cmd == "git" {
			t.Errorf("Task %d: must not schedule any git command on an existing repo: %v", i, execTask.Args)
		}
	}
}

// TestGitHandler_SymlinkGit_NoTasks is the regression test for a `.git` entry
// that exists only as a symlink. os.Stat follows symlinks, so a dangling `.git`
// symlink looks absent and the handler would schedule `git init`; git would
// then follow the link and create the repository outside the target. The guard
// must use os.Lstat so any `.git` entry — dangling symlink, symlink to an
// outside directory, or symlink to a file — is treated as a pre-existing repo
// and no git tasks are scheduled (fail closed).
func TestGitHandler_SymlinkGit_NoTasks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	cases := []struct {
		name  string
		setup func(t *testing.T, target string)
	}{
		{"dangling symlink", setupDanglingGitSymlink},
		{"symlink to outside directory", setupOutsideDirGitSymlink},
		{"symlink to regular file", setupFileGitSymlink},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			tc.setup(t, tmpDir)
			assertSymlinkedGitSchedulesNoTasks(t, tmpDir)
		})
	}
}

func setupDanglingGitSymlink(t *testing.T, target string) {
	t.Helper()
	if err := os.Symlink(filepath.Join(target, "does-not-exist"), filepath.Join(target, fileutil.GitDir)); err != nil {
		t.Fatalf("failed to create dangling .git symlink: %v", err)
	}
}

func setupOutsideDirGitSymlink(t *testing.T, target string) {
	t.Helper()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(target, fileutil.GitDir)); err != nil {
		t.Fatalf("failed to create .git symlink: %v", err)
	}
}

func setupFileGitSymlink(t *testing.T, target string) {
	t.Helper()
	outside := filepath.Join(t.TempDir(), "gitfile")
	if err := os.WriteFile(outside, []byte("gitdir: elsewhere\n"), 0600); err != nil {
		t.Fatalf("failed to create symlink target file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(target, fileutil.GitDir)); err != nil {
		t.Fatalf("failed to create .git symlink: %v", err)
	}
}

// assertSymlinkedGitSchedulesNoTasks verifies that a target whose `.git` entry
// is a symlink yields no git tasks and that executing the returned (empty) plan
// neither creates a repository outside the target nor replaces the symlink.
func assertSymlinkedGitSchedulesNoTasks(t *testing.T, target string) {
	t.Helper()

	h := &Handler{}
	plan, err := h.GetTasks(target, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(plan) != 0 {
		t.Fatalf("Expected no git tasks for a symlinked .git, got %d: %v", len(plan), plan)
	}

	// Executing the returned plan must not create a repository anywhere
	// outside the target (nor turn the symlink target into a repo).
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: target,
	}
	for _, task := range plan {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("plan execution failed at task '%s': %v", task.Description(), err)
		}
	}

	// The `.git` entry must still be the symlink we planted, not a
	// directory git init would have created by following it.
	info, err := os.Lstat(filepath.Join(target, fileutil.GitDir))
	if err != nil {
		t.Fatalf("planted .git entry disappeared: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("SECURITY: .git symlink was replaced by a real %v entry", info.Mode())
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

	// Scoped environment setup. t.Setenv ensures these are cleaned up automatically.
	isolateGitEnv(t)

	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "scbake.toml"), []byte(""), 0600); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	h := &Handler{}
	plan, err := h.GetTasks(tmpDir, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
	}

	for _, task := range plan {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Fresh lifecycle failed at task '%s': %v", task.Description(), err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	logCmd := exec.CommandContext(ctx, "git", "log", "-1", "--pretty=%B")
	logCmd.Dir = tmpDir
	out, err := logCmd.Output()
	if err != nil {
		t.Fatalf("Failed to read git log: %v", err)
	}

	if !strings.Contains(string(out), "scbake: Apply templates") {
		t.Errorf("Commit message mismatch. Got: %s", string(out))
	}
}

// TestGitTemplate_ExistingRepo_RunsNoGitTasks is the SCB-037 regression test.
// A target that already contains a .git directory is untrusted input. Disabling
// hooks via `-c core.hooksPath=<devnull>` is insufficient because `git add` and
// `git status` still execute attacker-controlled `filter.*.clean` and
// `core.fsmonitor` programs from the untrusted `.git/config`. The handler must
// therefore schedule NO git tasks at all for a pre-existing repository, so none
// of these vectors can run.
func TestGitTemplate_ExistingRepo_RunsNoGitTasks(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	isolateGitEnv(t)

	tmpDir := t.TempDir()
	markers := plantUntrustedRepoVectors(t, tmpDir)

	if err := os.WriteFile(filepath.Join(tmpDir, "scbake.toml"), []byte(""), 0600); err != nil {
		t.Fatalf("Failed to create config file: %v", err)
	}

	h := &Handler{}
	plan, err := h.GetTasks(tmpDir, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	assertNoGitTasks(t, plan)

	// Execute the (empty) plan end-to-end. Nothing must run.
	executeTaskPlan(t, plan, tmpDir)

	assertMarkersAbsent(t, markers)
}

// plantUntrustedRepoVectors initializes an attacker-controlled repository in
// tmpDir and installs a malicious pre-commit hook, clean filter, and fsmonitor.
// It returns the marker file each vector would touch if it ever executed.
func plantUntrustedRepoVectors(t *testing.T, tmpDir string) map[string]string {
	t.Helper()

	runInDir(t, tmpDir, "git", "init")
	runInDir(t, tmpDir, "git", "config", "user.email", "scbake-test@scbake.dev")
	runInDir(t, tmpDir, "git", "config", "user.name", "scbake-test-user")

	hookMarker := filepath.Join(tmpDir, "hook-executed.marker")
	filterMarker := filepath.Join(tmpDir, "filter-executed.marker")
	fsmonitorMarker := filepath.Join(tmpDir, "fsmonitor-executed.marker")

	// Vector 1: a malicious pre-commit hook.
	hooksDir := filepath.Join(tmpDir, fileutil.GitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0700); err != nil {
		t.Fatalf("Failed to create hooks dir: %v", err)
	}
	writeExecutableScript(t, filepath.Join(hooksDir, "pre-commit"), "touch "+hookMarker)

	// Vector 2: a malicious clean filter that `git add` would execute.
	filterScript := filepath.Join(tmpDir, "evil-clean.sh")
	writeExecutableScript(t, filterScript, "touch "+filterMarker+"\ncat")
	runInDir(t, tmpDir, "git", "config", "filter.evil.clean", filterScript)
	runInDir(t, tmpDir, "git", "config", "filter.evil.required", "true")
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitattributes"), []byte("*.txt filter=evil\n"), 0600); err != nil {
		t.Fatalf("Failed to write .gitattributes: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "evil.txt"), []byte("payload"), 0600); err != nil {
		t.Fatalf("Failed to write evil.txt: %v", err)
	}

	// Vector 3: a malicious fsmonitor that `git add`/`git status` would execute.
	fsmonitorScript := filepath.Join(tmpDir, "evil-fsmonitor.sh")
	writeExecutableScript(t, fsmonitorScript, "touch "+fsmonitorMarker)
	runInDir(t, tmpDir, "git", "config", "core.fsmonitor", fsmonitorScript)

	return map[string]string{
		"pre-commit hook": hookMarker,
		"clean filter":    filterMarker,
		"fsmonitor":       fsmonitorMarker,
	}
}

// assertNoGitTasks fails if the plan schedules any git command at all.
func assertNoGitTasks(t *testing.T, plan []types.Task) {
	t.Helper()

	if len(plan) != 0 {
		t.Fatalf("Expected no git tasks for an existing repo, got %d", len(plan))
	}
	for i, task := range plan {
		if execTask, ok := task.(*tasks.ExecCommandTask); ok && execTask.Cmd == "git" {
			t.Errorf("Task %d: git command scheduled on existing repo: %v", i, execTask.Args)
		}
	}
}

// executeTaskPlan runs every task in the plan against target.
func executeTaskPlan(t *testing.T, plan []types.Task, target string) {
	t.Helper()

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: target,
	}
	for _, task := range plan {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Existing-repo lifecycle failed at task '%s': %v", task.Description(), err)
		}
	}
}

// assertMarkersAbsent fails if any planted marker file exists, proving that the
// corresponding untrusted program executed during scaffolding.
func assertMarkersAbsent(t *testing.T, markers map[string]string) {
	t.Helper()

	for name, marker := range markers {
		if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
			t.Fatalf("SECURITY: planted %s executed during scaffolding (marker present)", name)
		}
	}
}

// writeExecutableScript writes a #!/bin/sh script with the given body and marks
// it executable, so Git would be able to run it if it were ever invoked.
func writeExecutableScript(t *testing.T, path, body string) {
	t.Helper()
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatalf("Failed to write script %s: %v", path, err)
	}
	//nolint:gosec // G302: test fixture must stay executable so git would run it if invoked
	if err := os.Chmod(path, 0700); err != nil {
		t.Fatalf("Failed to mark script %s executable: %v", path, err)
	}
}

// TestGitTemplate_Idempotent ensures re-running the handler against an existing
// repository is safe: no Git tasks are scheduled, so the repository is left
// untouched (fail-closed).
func TestGitTemplate_Idempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	// Apply identity isolation for idempotent tests.
	t.Setenv("GIT_AUTHOR_NAME", "scbake-test-user")
	t.Setenv("GIT_AUTHOR_EMAIL", "scbake-test@scbake.dev")
	t.Setenv("GIT_COMMITTER_NAME", "scbake-test-user")
	t.Setenv("GIT_COMMITTER_EMAIL", "scbake-test@scbake.dev")

	tmpDir := t.TempDir()

	runInDir(t, tmpDir, "git", "init")
	runInDir(t, tmpDir, "git", "config", "user.email", "scbake-test@scbake.dev")
	runInDir(t, tmpDir, "git", "config", "user.name", "scbake-test-user")

	if err := os.WriteFile(filepath.Join(tmpDir, "init.txt"), []byte("initial"), 0600); err != nil {
		t.Fatalf("Failed to create init file: %v", err)
	}

	runInDir(t, tmpDir, "git", "add", ".")
	runInDir(t, tmpDir, "git", "commit", "-m", "Initial")

	h := &Handler{}
	plan, err := h.GetTasks(tmpDir, "", "")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(plan) != 0 {
		t.Fatalf("Expected no git tasks for an existing repo, got %d", len(plan))
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusCmd.Dir = tmpDir
	status, err := statusCmd.Output()
	if err != nil {
		t.Fatalf("Failed to get git status: %v", err)
	}

	if len(status) > 0 {
		t.Errorf("Repository is dirty after idempotent run:\n%s", string(status))
	}
}

// isolateGitEnv pins Git identity and config to test-local values so commits
// succeed regardless of the host configuration.
func isolateGitEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "scbake-test-user")
	t.Setenv("GIT_AUTHOR_EMAIL", "scbake-test@scbake.dev")
	t.Setenv("GIT_COMMITTER_NAME", "scbake-test-user")
	t.Setenv("GIT_COMMITTER_EMAIL", "scbake-test@scbake.dev")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "true")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
}

func runInDir(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	//nolint:gosec // Test helper intentionally accepts variable command name for flexibility
	cmd := exec.CommandContext(ctx, name, args...)
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
