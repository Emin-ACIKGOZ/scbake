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
