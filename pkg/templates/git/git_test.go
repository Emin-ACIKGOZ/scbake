// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/types"
	"testing"
)

func TestGitTemplate_Fresh(t *testing.T) {
	// Requires git installed
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpDir := t.TempDir()

	// Create a file to commit
	if err := os.WriteFile(filepath.Join(tmpDir, "scbake.toml"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	tasks, err := h.GetTasks(tmpDir)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		DryRun:     false,
	}

	// 1. Init
	if err := tasks[0].Execute(tc); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Configure user for CI environments.
	// We must check errors here to ensure the environment is valid for the commit step.
	//nolint:gosec // Trusted test input (tmpDir)
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@scbake.dev").Run(); err != nil {
		t.Fatalf("failed to configure git user.email: %v", err)
	}
	//nolint:gosec // Trusted test input (tmpDir)
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User").Run(); err != nil {
		t.Fatalf("failed to configure git user.name: %v", err)
	}

	// 2. Add
	if err := tasks[1].Execute(tc); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// 3. Commit
	if err := tasks[2].Execute(tc); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Verify
	if _, err := os.Stat(filepath.Join(tmpDir, ".git")); os.IsNotExist(err) {
		t.Error(".git directory not found")
	}
}

func TestGitTemplate_Idempotent(t *testing.T) {
	// Requires git installed
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}

	tmpDir := t.TempDir()

	// Initialize manually first
	//nolint:gosec // Trusted test input (tmpDir)
	if err := exec.Command("git", "-C", tmpDir, "init").Run(); err != nil {
		t.Fatalf("manual git init failed: %v", err)
	}
	//nolint:gosec // Trusted test input (tmpDir)
	if err := exec.Command("git", "-C", tmpDir, "config", "user.email", "test@scbake.dev").Run(); err != nil {
		t.Fatalf("manual git config email failed: %v", err)
	}
	//nolint:gosec // Trusted test input (tmpDir)
	if err := exec.Command("git", "-C", tmpDir, "config", "user.name", "Test User").Run(); err != nil {
		t.Fatalf("manual git config name failed: %v", err)
	}

	// Create file
	if err := os.WriteFile(filepath.Join(tmpDir, "new.txt"), []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}

	h := NewHandler()
	tasks, err := h.GetTasks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		DryRun:     false,
	}

	// Execute all (Init should be safe, Add should work, Commit should work)
	for _, task := range tasks {
		if err := task.Execute(tc); err != nil {
			t.Fatalf("Task %s failed: %v", task.Description(), err)
		}
	}
}
