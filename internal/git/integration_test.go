package git

import (
	"os"
	"testing"
)

func TestGitIntegration(t *testing.T) {
	// 1. Prerequisite: Verify git is installed
	if err := CheckGitInstalled(); err != nil {
		t.Skipf("CheckGitInstalled failed (git not found?): %v", err)
	}

	// 2. Setup: Create a clean temp workspace
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	// --- Phase 1: Repo Initialization ---

	// CheckIsRepo should fail in empty dir
	if err := CheckIsRepo(); err == nil {
		t.Error("CheckIsRepo should fail in empty dir, but succeeded")
	}

	// Initialize Repo
	if err := Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	// CheckIsRepo should now succeed
	if err := CheckIsRepo(); err != nil {
		t.Error("CheckIsRepo should succeed after Init, but failed")
	}

	// --- Phase 2: Initial Commit & HEAD ---

	// CheckHasHEAD should be false in empty repo
	// (This previously failed because of the error wrapping bug)
	hasHead, err := CheckHasHEAD()
	if err != nil {
		t.Fatalf("CheckHasHEAD failed: %v", err)
	}
	if hasHead {
		t.Error("CheckHasHEAD should be false in empty repo")
	}

	// Perform Initial Commit
	if err := InitialCommit("initial structure"); err != nil {
		t.Fatalf("InitialCommit failed: %v", err)
	}

	// Verify HEAD exists now
	hasHeadAfter, err := CheckHasHEAD()
	if err != nil {
		t.Fatalf("CheckHasHEAD (after commit) failed: %v", err)
	}
	if !hasHeadAfter {
		t.Error("CheckHasHEAD should be true after InitialCommit")
	}

	// --- Phase 3: Savepoint Rollback (Failure Scenario) ---
	tag, err := CreateSavepoint()
	if err != nil {
		t.Fatalf("CreateSavepoint failed: %v", err)
	}

	// Make a change (simulate a task polluting the workspace)
	if err := os.WriteFile("test.txt", []byte("dirty"), 0644); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Rollback
	if err := RollbackToSavepoint(tag); err != nil {
		t.Fatalf("RollbackToSavepoint failed: %v", err)
	}

	// Verify rollback (file should be gone)
	if _, err := os.Stat("test.txt"); !os.IsNotExist(err) {
		t.Error("Rollback failed: test.txt should have been deleted")
	}

	// --- Phase 4: Savepoint Cleanup (Success Scenario) ---
	// Test the "Happy Path" where we delete the tag manually
	tagSuccess, err := CreateSavepoint()
	if err != nil {
		t.Fatalf("CreateSavepoint (2) failed: %v", err)
	}

	if err := DeleteSavepoint(tagSuccess); err != nil {
		t.Errorf("DeleteSavepoint failed: %v", err)
	}
}
