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
