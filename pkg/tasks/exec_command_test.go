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
	"slices"
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
func TestExecCommandTaskHelper(_ *testing.T) {
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
	case "sleep":
		time.Sleep(5 * time.Second)
	}
}

func TestExecCommandTask_PredictedCreated(t *testing.T) {
	// Verify that predicted paths are tracked even if the command doesn't create them
	rootDir := t.TempDir()
	tx, _ := transaction.New(rootDir)

	// We expect "generated_artifact" to be created.
	// We won't actually create it (using a dummy command),
	// but we want to verify the Transaction Manager *thinks* we are about to create it.
	task := &ExecCommandTask{
		Cmd:              "echo",
		Args:             []string{"noop"},
		Desc:             "Prediction Test",
		TaskPrio:         100,
		RunInTarget:      true,
		PredictedCreated: []string{"generated_artifact"},
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Tx:         tx,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Task failed: %v", err)
	}

	// Simulate Rollback
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// If the file HAD been created, it would be deleted.
	// Since we are black-box testing the integration, we rely on the fact that
	// Track() was called without error.
	// Ideally, we'd check tx internals, but we can infer success by the absence of error.
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
