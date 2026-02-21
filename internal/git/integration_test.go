// internal/git/integration_test.go
package git

import (
	"os"
	"testing"
)

func TestGitIntegration(t *testing.T) {
	// Prerequisite: git must be installed
	if err := CheckGitInstalled(); err != nil {
		t.Skipf("CheckGitInstalled failed (git not found?): %v", err)
	}

	// Setup: isolated workspace
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to change dir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	t.Run("Repo Initialization", testRepoInitialization)
	t.Run("Initial Commit & HEAD", testInitialCommitHead)
	t.Run("Savepoint Rollback", testSavepointRollback)
	t.Run("Savepoint Cleanup", testSavepointCleanup)
}

func testRepoInitialization(t *testing.T) {
	// CheckIsRepo must fail before init
	if err := CheckIsRepo(); err == nil {
		t.Error("CheckIsRepo should fail in empty dir")
	}

	// Initialize Repo
	if err := Init(); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	// CheckIsRepo must succeed after init
	if err := CheckIsRepo(); err != nil {
		t.Error("CheckIsRepo should succeed after Init")
	}
}

func testInitialCommitHead(t *testing.T) {
	// There is no HEAD initially
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
		t.Fatalf("CheckHasHEAD after commit failed: %v", err)
	}
	if !hasHeadAfter {
		t.Error("CheckHasHEAD should be true after initial commit")
	}
}

func testSavepointRollback(t *testing.T) {
	tag, err := CreateSavepoint()
	if err != nil {
		t.Fatalf("CreateSavepoint failed: %v", err)
	}

	// Dirty workspace (simulate a task polluting the workspace)
	if err := os.WriteFile("test.txt", []byte("dirty"), 0600); err != nil {
		t.Fatalf("failed to write dirty file: %v", err)
	}

	// Roll back
	if err := RollbackToSavepoint(tag); err != nil {
		t.Fatalf("RollbackToSavepoint failed: %v", err)
	}

	// Dirty file must be gone
	if _, err := os.Stat("test.txt"); !os.IsNotExist(err) {
		t.Error("Rollback failed: test.txt should have been removed")
	}
}

func testSavepointCleanup(t *testing.T) {
	tag, err := CreateSavepoint()
	if err != nil {
		t.Fatalf("CreateSavepoint failed: %v", err)
	}

	if err := DeleteSavepoint(tag); err != nil {
		t.Errorf("DeleteSavepoint failed: %v", err)
	}
}
