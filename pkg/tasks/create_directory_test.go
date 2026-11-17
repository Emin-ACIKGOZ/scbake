// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"testing"
)

func TestCreateDirTask(t *testing.T) {
	tmpDir := t.TempDir()
	targetPath := filepath.Join(tmpDir, "nested", "dir")

	task := &CreateDirTask{
		Path:     targetPath,
		Desc:     "Create nested dir",
		TaskPrio: 50,
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: tmpDir,
		DryRun:     false,
	}

	// 1. Run first time
	if err := task.Execute(tc); err != nil {
		t.Fatalf("First execution failed: %v", err)
	}

	// Verify existence
	info, err := os.Stat(targetPath)
	if err != nil {
		t.Fatalf("Directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("Created path is not a directory")
	}

	// 2. Run second time (Idempotency check)
	if err := task.Execute(tc); err != nil {
		t.Errorf("Second execution (idempotency) failed: %v", err)
	}
}

func TestCreateDirTask_Transaction(t *testing.T) {
	// Verify that the task correctly registers with the transaction manager
	rootDir := t.TempDir()
	tx, _ := transaction.New(rootDir)

	targetDir := filepath.Join(rootDir, "tracked_dir")
	absTargetDir, _ := filepath.Abs(targetDir)

	task := &CreateDirTask{
		Path:     targetDir,
		Desc:     "Tracked Dir",
		TaskPrio: 50,
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Tx:         tx,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Task execution failed: %v", err)
	}

	// Rollback should remove the directory
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if _, err := os.Stat(absTargetDir); !os.IsNotExist(err) {
		t.Error("Directory was not removed by rollback, likely wasn't tracked via absolute path")
	}
}

func TestCreateDirTask_RollbackRemovesIntermediateDirs(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}

	task := &CreateDirTask{
		Path:     "services/api",
		Desc:     "Nested services dir",
		TaskPrio: 50,
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Tx:         tx,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(rootDir, "services", "api")); err != nil {
		t.Fatalf("nested directory was not created: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(rootDir, "services", "api")); !os.IsNotExist(err) {
		t.Errorf("nested directory was not removed by rollback")
	}

	// Regression (SCB-055): the implicitly-created intermediate directory must
	// also be removed, not just the leaf directory that was explicitly tracked.
	if _, err := os.Stat(filepath.Join(rootDir, "services")); !os.IsNotExist(err) {
		t.Errorf("intermediate directory %q was left behind by rollback", "services")
	}
}

func TestCreateDirTask_PathTraversalRejected(t *testing.T) {
	tmpDir := t.TempDir()
	traversalPayloads := []string{
		"../outside",
		"../../escaped",
		"sub/../../outside",
	}

	for _, payload := range traversalPayloads {
		t.Run(payload, func(t *testing.T) {
			task := &CreateDirTask{
				Path:     payload,
				Desc:     "Path traversal dir safety test",
				TaskPrio: 50,
			}

			tc := types.TaskContext{
				Ctx:        context.Background(),
				TargetPath: tmpDir,
				DryRun:     true, // DryRun prevents directory creation outside test area
			}

			err := task.Execute(tc)
			if err == nil {
				t.Errorf("Path traversal contract violation: CreateDirTask unexpectedly accepted directory traversal payload %q", payload)
			}
		})
	}
}

func TestCreateDirTask_DryRunDoesNotCreateNestedDirectory(t *testing.T) {
	root := t.TempDir()
	// The target itself does not exist yet, so the nested path has a missing
	// parent at every level.
	target := filepath.Join(root, "missing-target")

	task := &CreateDirTask{
		Path:     filepath.Join("generated", "api"),
		Desc:     "Nested dry-run dir",
		TaskPrio: 50,
	}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: target,
		DryRun:     true,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() with DryRun failed: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("dry-run created target directory %q", target)
	}
	if _, err := os.Stat(filepath.Join(target, "generated", "api")); !os.IsNotExist(err) {
		t.Fatalf("dry-run created nested directory under %q", target)
	}
}

func TestCreateDirTask_DryRunPreservesTraversalErrors(t *testing.T) {
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: t.TempDir(),
		DryRun:     true,
	}
	task := &CreateDirTask{Path: "../outside/dir", Desc: "Traversal dry run", TaskPrio: 50}

	if err := task.Execute(tc); err == nil {
		t.Fatal("dry-run accepted a directory path outside the target")
	}
}

// TestCreateDirTask_RollbackRemovesMissingTargetSubdir is a regression test for
// SCB-055: when the target directory itself does not exist yet (e.g.
// `apply <subproject>` in a monorepo, where the transaction root is the existing
// manifest root), the implicitly-created target and its intermediates must be
// removed by rollback.
func TestCreateDirTask_RollbackRemovesMissingTargetSubdir(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}

	// The subproject target does not exist yet.
	target := filepath.Join(root, "services")
	task := &CreateDirTask{Path: "api", Desc: "d"}
	tc := types.TaskContext{TargetPath: target, Manifest: &types.Manifest{}, Tx: tx}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "services")); !os.IsNotExist(err) {
		t.Errorf("rollback left the implicitly-created target tree %q behind", filepath.Join(root, "services"))
	}
}
