// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

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
		Ctx:    context.Background(),
		DryRun: false,
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
