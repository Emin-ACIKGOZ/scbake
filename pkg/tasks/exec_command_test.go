// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"testing"
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
	// We run 'pwd' (or create a file) inside the temp dir and check location.
	// Since 'pwd' output is hard to capture without modifying the task,
	// we will run a command that has a side effect: 'touch file.txt'.

	// Create a dummy file named "created_in_target" inside tmpDir
	touchTask := &ExecCommandTask{
		Cmd:         "touch", // Assumes unix-like environment or git bash
		Args:        []string{"created_in_target"},
		Desc:        "Touch file in target",
		TaskPrio:    100,
		RunInTarget: true,
	}

	if err := touchTask.Execute(tc); err != nil {
		t.Fatalf("Touch task failed: %v", err)
	}

	// Verify the file exists in the CORRECT place
	expectedPath := filepath.Join(tmpDir, "created_in_target")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("RunInTarget=true failed. File not found at %s", expectedPath)
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
