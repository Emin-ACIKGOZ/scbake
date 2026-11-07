// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/templateregistry"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestExecCommandTask verifies that external commands run correctly.
func TestExecCommandTask(t *testing.T) {
	// Setup a temporary directory to act as the target
	tmpDir := t.TempDir()

	// Case 1: Simple Success (echo)
	task := &ExecCommandTask{
		Cmd:      "echo",
		Args:     []string{"hello"},
		Desc:     "Echo test",
		TaskPrio: 100,
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		DryRun:     false,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Simple echo task failed: %v", err)
	}

	// Case 2: RunInTarget (Verify working directory)
	// Create a dummy file named "created_in_target" inside tmpDir
	// Note: using 'touch' requires unix environment, for robust testing across OS
	// we rely on the command working, but if running on windows without touch, this might fail.
	// For this specific test logic, let's assume valid command environment or mock.
	// We'll skip if touch is missing? No, let's assume standard environment or use go run.

	// Better: Use "go" command itself to do something trivial if available, or just skip if simple command fails.
	// Since this is a unit test for the Task wrapper logic, we can use "echo" writing to a file if shell supported,
	// but ExecCommand doesn't support shell redirection natively.
	// Keeping "touch" as per original test, assuming dev environment has it.

	touchTask := &ExecCommandTask{
		Cmd:         "touch", // Assumes unix-like environment or git bash
		Args:        []string{"created_in_target"},
		Desc:        "Touch file in target",
		TaskPrio:    100,
		RunInTarget: true,
	}

	// Allow failure if 'touch' isn't found (e.g. minimal Windows), but if it runs, check result.
	if err := touchTask.Execute(tc); err == nil {
		// Verify the file exists in the CORRECT place
		expectedPath := filepath.Join(tmpDir, "created_in_target")
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("RunInTarget=true failed. File not found at %s", expectedPath)
		}
	}

	// Case 3: Dry Run (Should do nothing)
	dryTask := &ExecCommandTask{
		Cmd:         "touch",
		Args:        []string{"should_not_exist"},
		Desc:        "Dry run test",
		TaskPrio:    100,
		RunInTarget: true,
	}
	dryTC := tc
	dryTC.DryRun = true

	if err := dryTask.Execute(dryTC); err != nil {
		t.Fatalf("Dry run execution failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(tmpDir, "should_not_exist")); !os.IsNotExist(err) {
		t.Error("Dry run failed: Command was actually executed!")
	}
}

func TestScrubRegistryTokens(t *testing.T) {
	tests := []struct {
		name string
		env  []string
		want []string
	}{
		{"uppercase", []string{"PATH=/bin", "SCBAKE_REGISTRY_TOKEN_PROD=secret", "KEEP=1"}, []string{"PATH=/bin", "KEEP=1"}},
		{"lowercase", []string{"scbake_registry_token_prod=secret"}, []string{}},
		{"duplicate", []string{"SCBAKE_REGISTRY_TOKEN_A=one", "SCBAKE_REGISTRY_TOKEN_A=two"}, []string{}},
		{"no separator", []string{"SCBAKE_REGISTRY_TOKEN_PROD"}, []string{}},
		{"value has separator", []string{"SCBAKE_REGISTRY_TOKEN_PROD=a=b"}, []string{}},
		{"unrelated", []string{"SCBAKE_TEMPLATE_DIR=/tmp"}, []string{"SCBAKE_TEMPLATE_DIR=/tmp"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := scrubRegistryTokens(tc.env); !slices.Equal(got, tc.want) {
				t.Errorf("scrubRegistryTokens() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestScrubRegistryTokensNeverReturnsNil(t *testing.T) {
	for _, env := range [][]string{
		nil,
		{"SCBAKE_REGISTRY_TOKEN_A=secret"},
	} {
		if got := scrubRegistryTokens(env); got == nil {
			t.Fatalf("scrubRegistryTokens(%v) returned nil", env)
		}
	}
}

func TestScrubRegistryTokensMatchesRegistryKey(t *testing.T) {
	env := []string{templateregistry.RegistryTokenEnvKey("prod") + "=secret"}
	if got := scrubRegistryTokens(env); len(got) != 0 {
		t.Errorf("scrubRegistryTokens() = %v, want empty", got)
	}
}

func TestExecCommandTask_ScrubsRegistryTokens(t *testing.T) {
	t.Setenv("SCBAKE_REGISTRY_TOKEN_PROD", "super-secret")
	t.Setenv("GO_WANT_EXEC_HELPER", "env")

	task := &ExecCommandTask{
		Cmd:  os.Args[0],
		Args: []string{"-test.run=^TestExecCommandTaskHelper$"},
		Desc: "Check child environment",
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: t.TempDir()}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("child inherited a registry token: %v", err)
	}
}

func TestExecCommandTask_BoundsStderr(t *testing.T) {
	t.Setenv("GO_WANT_EXEC_HELPER", "stderr")

	task := &ExecCommandTask{
		Cmd:  os.Args[0],
		Args: []string{"-test.run=^TestExecCommandTaskHelper$"},
		Desc: "Overflow stderr",
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: t.TempDir()}

	err := task.Execute(tc)
	if err == nil {
		t.Fatal("expected the helper to fail")
	}
	if !strings.Contains(err.Error(), "[stderr truncated]") {
		t.Error("error does not report truncated stderr")
	}
	if len(err.Error()) > maxStderrBytes+200 {
		t.Errorf("captured stderr is not bounded: %d bytes", len(err.Error()))
	}
}

// TestExecCommandTaskHelper is a child process for the tests above.
func TestExecCommandTaskHelper(_ *testing.T) { //nolint:cyclop // one branch per helper mode
	switch os.Getenv("GO_WANT_EXEC_HELPER") {
	case "env":
		if token := os.Getenv("SCBAKE_REGISTRY_TOKEN_PROD"); token != "" {
			fmt.Fprintf(os.Stderr, "registry token leaked: %s\n", token)
			os.Exit(1)
		}
	case "stderr":
		fmt.Fprint(os.Stderr, strings.Repeat("x", maxStderrBytes*2))
		os.Exit(1)
	case "descendant":
		// Keep a descendant alive with the inherited stderr pipe open, to
		// exercise Cmd.WaitDelay after this process is killed.
		child := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestExecCommandTaskHelper$") //nolint:gosec // test helper
		child.Env = append(os.Environ(), "GO_WANT_EXEC_HELPER=sleep")
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		_ = child.Wait()
	case "orphan-success":
		child := exec.CommandContext(context.Background(), os.Args[0], "-test.run=^TestExecCommandTaskHelper$") //nolint:gosec // test helper
		child.Env = append(os.Environ(), "GO_WANT_EXEC_HELPER=sleep")
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "mutate":
		// Write or overwrite files as a stand-in for an opaque external
		// command, then optionally fail so the caller can exercise rollback.
		mutateHelperFile(os.Getenv("GO_WANT_EXEC_HELPER_PATH"), os.Getenv("GO_WANT_EXEC_HELPER_CONTENT"), os.Getenv("GO_WANT_EXEC_HELPER_MODE"))
		mutateHelperFile(os.Getenv("GO_WANT_EXEC_HELPER_PATH2"), os.Getenv("GO_WANT_EXEC_HELPER_CONTENT2"), os.Getenv("GO_WANT_EXEC_HELPER_MODE2"))
		if os.Getenv("GO_WANT_EXEC_HELPER_FAIL") != "" {
			fmt.Fprintln(os.Stderr, "intentional helper failure")
			os.Exit(1)
		}
	case "sentinel":
		writeHelperFile(os.Getenv("GO_WANT_EXEC_HELPER_PATH"), "helper ran\n")
	case "sleep":
		time.Sleep(5 * time.Second)
	}
}

// mutateHelperFile writes content to path and optionally sets its permission
// bits. It is used by the child-process helper to simulate an external command
// touching transaction-tracked paths.
func mutateHelperFile(path, content, mode string) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), fileutil.DirPerms); err != nil { //nolint:gosec // path is injected by the test environment
		fmt.Fprintf(os.Stderr, "helper mkdir %s: %v\n", path, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // path is injected by the test environment
		fmt.Fprintf(os.Stderr, "helper write %s: %v\n", path, err)
		os.Exit(1)
	}
	if mode == "" {
		return
	}
	perm, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		fmt.Fprintf(os.Stderr, "helper parse mode %q: %v\n", mode, err)
		os.Exit(1)
	}
	if err := os.Chmod(path, os.FileMode(perm)); err != nil { //nolint:gosec // path is injected by the test environment
		fmt.Fprintf(os.Stderr, "helper chmod %s: %v\n", path, err)
		os.Exit(1)
	}
}

// writeHelperFile writes a sentinel proving the child command ran.
func writeHelperFile(path, content string) {
	if path == "" {
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil { //nolint:gosec // path is injected by the test environment
		fmt.Fprintf(os.Stderr, "helper write sentinel %s: %v\n", path, err)
		os.Exit(1)
	}
}

func TestExecCommandTask_PredictedCreated(t *testing.T) { //nolint:cyclop // three behavioral subtests with explicit setup
	t.Run("existing file is backed up and restored", func(t *testing.T) {
		root := t.TempDir()
		const original = "module example.com/original\n\ngo 1.21\n"
		const modified = "module example.com/modified\n"

		path := filepath.Join(root, "go.mod")
		if err := os.WriteFile(path, []byte(original), 0o640); err != nil { //nolint:gosec // mode restoration under test
			t.Fatalf("seed go.mod: %v", err)
		}
		if err := os.Chmod(path, 0o640); err != nil { //nolint:gosec // mode restoration under test
			t.Fatalf("set go.mod mode: %v", err)
		}

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New: %v", err)
		}
		t.Setenv("GO_WANT_EXEC_HELPER", "mutate")
		t.Setenv("GO_WANT_EXEC_HELPER_PATH", path)
		t.Setenv("GO_WANT_EXEC_HELPER_CONTENT", modified)
		t.Setenv("GO_WANT_EXEC_HELPER_MODE", "0600")

		task := &ExecCommandTask{
			Cmd:              os.Args[0],
			Args:             []string{"-test.run=^TestExecCommandTaskHelper$"},
			Desc:             "Rewrite go.mod",
			RunInTarget:      true,
			PredictedCreated: []string{"go.mod"},
		}
		tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Tx: tx}

		if err := task.Execute(tc); err != nil {
			t.Fatalf("task failed: %v", err)
		}
		if got := mustReadTestFile(t, path); got != modified {
			t.Fatalf("command did not modify go.mod: got %q, want %q", got, modified)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if got := mustReadTestFile(t, path); got != original {
			t.Fatalf("go.mod content after rollback = %q, want %q", got, original)
		}
		assertTestFileMode(t, path, 0o640)
	})

	t.Run("missing file created by command is removed", func(t *testing.T) {
		root := t.TempDir()
		path := filepath.Join(root, "go.sum")

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New: %v", err)
		}
		t.Setenv("GO_WANT_EXEC_HELPER", "mutate")
		t.Setenv("GO_WANT_EXEC_HELPER_PATH", path)
		t.Setenv("GO_WANT_EXEC_HELPER_CONTENT", "example.com/dep v1.0.0 h1:abc=\n")

		task := &ExecCommandTask{
			Cmd:              os.Args[0],
			Args:             []string{"-test.run=^TestExecCommandTaskHelper$"},
			Desc:             "Create go.sum",
			RunInTarget:      true,
			PredictedCreated: []string{"go.sum"},
		}
		tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Tx: tx}

		if err := task.Execute(tc); err != nil {
			t.Fatalf("task failed: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("command did not create go.sum: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("go.sum survived rollback; stat err = %v", err)
		}
	})

	t.Run("failed command still rolls back tracked mutations", func(t *testing.T) {
		root := t.TempDir()
		const original = "module example.com/original\n\ngo 1.21\n"
		const modified = "module example.com/modified\n"

		existing := filepath.Join(root, "go.mod")
		created := filepath.Join(root, "go.sum")
		if err := os.WriteFile(existing, []byte(original), 0o640); err != nil { //nolint:gosec // mode restoration under test
			t.Fatalf("seed go.mod: %v", err)
		}
		if err := os.Chmod(existing, 0o640); err != nil { //nolint:gosec // mode restoration under test
			t.Fatalf("set go.mod mode: %v", err)
		}

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New: %v", err)
		}
		t.Setenv("GO_WANT_EXEC_HELPER", "mutate")
		t.Setenv("GO_WANT_EXEC_HELPER_PATH", existing)
		t.Setenv("GO_WANT_EXEC_HELPER_CONTENT", modified)
		t.Setenv("GO_WANT_EXEC_HELPER_MODE", "0600")
		t.Setenv("GO_WANT_EXEC_HELPER_PATH2", created)
		t.Setenv("GO_WANT_EXEC_HELPER_CONTENT2", "example.com/dep v1.0.0 h1:abc=\n")
		t.Setenv("GO_WANT_EXEC_HELPER_FAIL", "1")

		task := &ExecCommandTask{
			Cmd:              os.Args[0],
			Args:             []string{"-test.run=^TestExecCommandTaskHelper$"},
			Desc:             "Mutate then fail",
			RunInTarget:      true,
			PredictedCreated: []string{"go.mod", "go.sum"},
		}
		tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Tx: tx}

		err = task.Execute(tc)
		if err == nil {
			t.Fatal("expected the helper command to fail")
		}
		if !strings.Contains(err.Error(), "intentional helper failure") {
			t.Errorf("error does not include helper stderr: %v", err)
		}
		if got := mustReadTestFile(t, existing); got != modified {
			t.Fatalf("precondition failed: command did not modify go.mod: %q", got)
		}
		if _, err := os.Stat(created); err != nil {
			t.Fatalf("precondition failed: command did not create go.sum: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		if got := mustReadTestFile(t, existing); got != original {
			t.Fatalf("go.mod content after rollback = %q, want %q", got, original)
		}
		assertTestFileMode(t, existing, 0o640)
		if _, err := os.Stat(created); !os.IsNotExist(err) {
			t.Fatalf("go.sum survived rollback; stat err = %v", err)
		}
	})
}

// TestExecCommandTask_DryRunSkipsTrackingAndExecution verifies that a dry run
// neither registers predicted paths with the transaction nor executes the
// command.
func TestExecCommandTask_DryRunSkipsTrackingAndExecution(t *testing.T) {
	root := t.TempDir()
	const original = "module example.com/original\n"
	existing := filepath.Join(root, "go.mod")
	if err := os.WriteFile(existing, []byte(original), 0o600); err != nil {
		t.Fatalf("seed go.mod: %v", err)
	}
	sentinel := filepath.Join(root, "command-ran")

	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	t.Setenv("GO_WANT_EXEC_HELPER", "sentinel")
	t.Setenv("GO_WANT_EXEC_HELPER_PATH", sentinel)

	task := &ExecCommandTask{
		Cmd:              os.Args[0],
		Args:             []string{"-test.run=^TestExecCommandTaskHelper$"},
		Desc:             "Must not run",
		RunInTarget:      true,
		PredictedCreated: []string{"go.mod", "go.sum"},
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: root, Tx: tx, DryRun: true}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("dry-run task failed: %v", err)
	}
	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatalf("dry run executed the command; stat err = %v", err)
	}
	if got := mustReadTestFile(t, existing); got != original {
		t.Fatalf("dry run modified go.mod: got %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, "go.sum")); !os.IsNotExist(err) {
		t.Fatalf("dry run created go.sum; stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir)); !os.IsNotExist(err) {
		t.Fatalf("dry run created transaction state; stat err = %v", err)
	}
}

// TestExecCommandTask_ContextCanceled verifies that a canceled task context is
// reported as an interruption rather than as an ordinary child failure.
func TestExecCommandTask_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	task := &ExecCommandTask{
		Cmd:  "echo",
		Args: []string{"should not run"},
		Desc: "Canceled command",
	}
	tc := types.TaskContext{Ctx: ctx, TargetPath: t.TempDir()}

	err := task.Execute(tc)
	if err == nil {
		t.Fatal("expected a canceled command to fail")
	}
	if !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("error = %q, want an interruption message", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error does not wrap context.Canceled: %v", err)
	}
}

// TestExecCommandTask_WaitDelayClosesInheritedPipes verifies that a canceled
// command does not block on stderr pipes held open by orphaned descendants.
func TestExecCommandTask_WaitDelayClosesInheritedPipes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper uses a POSIX test binary process tree")
	}
	t.Setenv("GO_WANT_EXEC_HELPER", "descendant")

	ctx, cancel := context.WithCancel(context.Background())
	task := &ExecCommandTask{
		Cmd:  os.Args[0],
		Args: []string{"-test.run=^TestExecCommandTaskHelper$"},
		Desc: "Inherited pipe",
	}
	tc := types.TaskContext{Ctx: ctx, TargetPath: t.TempDir()}

	start := time.Now()
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := task.Execute(tc)
	if err == nil {
		t.Fatal("expected cancellation to fail the task")
	}
	if elapsed := time.Since(start); elapsed > commandWaitDelay+2*time.Second {
		t.Errorf("task waited %s for an orphaned descendant; WaitDelay did not bound it", elapsed)
	}
}

// TestExecCommandTask_WaitDelayIgnoresSuccessfulExit verifies that a command
// which exits 0 while leaving an orphan holding stderr is still a success.
func TestExecCommandTask_WaitDelayIgnoresSuccessfulExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper uses a POSIX test binary process tree")
	}
	t.Setenv("GO_WANT_EXEC_HELPER", "orphan-success")

	task := &ExecCommandTask{
		Cmd:  os.Args[0],
		Args: []string{"-test.run=^TestExecCommandTaskHelper$"},
		Desc: "Orphaned pipe after success",
	}
	tc := types.TaskContext{Ctx: context.Background(), TargetPath: t.TempDir()}

	start := time.Now()
	if err := task.Execute(tc); err != nil {
		t.Fatalf("successful command with an orphaned pipe failed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > commandWaitDelay+2*time.Second {
		t.Errorf("task waited %s for an orphaned descendant", elapsed)
	}
}
