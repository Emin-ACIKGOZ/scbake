// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package core

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"testing"
)

// MockTask implements types.Task for testing core logic.
type MockTask struct {
	Name         string
	Prio         int
	PathToCreate string // If set, creates this file
	ShouldFail   bool   // If set, returns error
}

func (m *MockTask) Description() string { return m.Name }
func (m *MockTask) Priority() int       { return m.Prio }

func (m *MockTask) Execute(tc types.TaskContext) error {
	if m.PathToCreate != "" {
		// Verify tracking works from within the core engine
		if tc.Tx != nil {
			if err := tc.Tx.Track(m.PathToCreate); err != nil {
				return err
			}
		}
		// Create file
		// G306: Use PrivateFilePerms for secure test file creation
		if err := os.WriteFile(m.PathToCreate, []byte("test data"), fileutil.PrivateFilePerms); err != nil {
			return err
		}
	}

	if m.ShouldFail {
		return errors.New("mock failure")
	}
	return nil
}

// TestExecuteAndFinalize_Rollback verifies that if a task fails,
// previous changes are rolled back.
func TestExecuteAndFinalize_Rollback(t *testing.T) {
	// Setup
	rootDir := t.TempDir()

	// Create initial manifest
	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	// G306: Use PrivateFilePerms for secure test file creation
	if err := os.WriteFile(manifestPath, []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	// Init transaction manually (simulating RunApply)
	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}
	// We do NOT defer tx.Rollback() here because we want to test its effect explicitly
	// after the failure to ensure deterministic testing.

	// Define paths
	file1 := filepath.Join(rootDir, "step1.txt")

	// Build Plan: Task 1 succeeds (creates file), Task 2 fails.
	plan := &types.Plan{
		Tasks: []types.Task{
			&MockTask{Name: "Step 1", PathToCreate: file1},
			&MockTask{Name: "Step 2", ShouldFail: true},
		},
	}

	// Setup Context
	// Correctly capture rootPath from Load
	m, rootPath, err := manifest.Load(rootDir)
	if err != nil {
		t.Fatal(err)
	}
	changes := &manifestChanges{}
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   m,
		Tx:         tx,
	}

	// Run logic
	logger := NewStepLogger(2, false)
	// Execute should fail
	err = executeAndFinalize(logger, plan, tc, m, changes, rootPath, tx)

	// Assert Failure
	if err == nil {
		t.Fatal("Expected execution to fail, but it succeeded")
	}

	// Assert Rollback
	// executeAndFinalize returned error, so RunApply's defer would trigger Rollback.
	// We manually trigger rollback here to simulate that behavior and verify result.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// Check if file1 (created by Step 1) is gone.
	if _, err := os.Stat(file1); !os.IsNotExist(err) {
		t.Errorf("Rollback failed. File %s should have been deleted.", file1)
	}
}

// TestExecuteAndFinalize_Success verifies that a successful run commits changes
// and cleans up the temp directory.
func TestExecuteAndFinalize_Success(t *testing.T) {
	rootDir := t.TempDir()

	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	// G306: Use PrivateFilePerms for secure test file creation
	if err := os.WriteFile(manifestPath, []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	// Explicitly check initialization error
	tx, err := transaction.New(rootDir)
	if err != nil {
		t.Fatalf("Failed to create transaction: %v", err)
	}

	// In success case, Commit() invalidates the transaction, so defer Rollback is harmless.
	defer func() { _ = tx.Rollback() }()

	file1 := filepath.Join(rootDir, "success.txt")

	plan := &types.Plan{
		Tasks: []types.Task{
			&MockTask{Name: "Success", PathToCreate: file1},
		},
	}

	// Correctly capture rootPath from Load
	m, rootPath, err := manifest.Load(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	// Propose a change to manifest to verify it saves
	changes := &manifestChanges{
		Projects: []types.Project{{Name: "NewProj", Path: "."}},
	}

	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: rootDir,
		Manifest:   m,
		Tx:         tx,
	}

	logger := NewStepLogger(1, false)
	if err := executeAndFinalize(logger, plan, tc, m, changes, rootPath, tx); err != nil {
		t.Fatalf("Execution failed: %v", err)
	}

	// Assert Persistence
	if _, err := os.Stat(file1); os.IsNotExist(err) {
		t.Error("File was not created")
	}

	// Assert Manifest Update
	updatedM, _, _ := manifest.Load(rootDir)
	if len(updatedM.Projects) != 1 {
		t.Error("Manifest was not updated")
	}

	// Assert Cleanup (Temp dir gone)
	tmpDir := filepath.Join(rootDir, fileutil.InternalDir, fileutil.TmpDir)

	// We read the directory.
	// 1. If entries exist > 0: FAIL (cleanup didn't happen)
	// 2. If err is nil (dir exists) but empty: PASS (transaction committed)
	// 3. If err is IsNotExist: PASS (parent folder removed, highly clean)
	entries, err := os.ReadDir(tmpDir)
	if err == nil && len(entries) > 0 {
		t.Errorf("Temp directory %s is not empty after commit: %v", tmpDir, entries)
	}
	// Note: We don't fail on IsNotExist, as that is a valid clean state.
}
