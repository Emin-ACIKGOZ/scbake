// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"testing"
	"time"
)

const osWindows = "windows"

// Helper to create a file with specific content and permissions
func createFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()

	// Ensure parent dir exists (restricted perms per gosec G301)
	if err := os.MkdirAll(filepath.Dir(path), fileutil.DirPerms); err != nil {
		t.Fatalf("failed to create parent dir for %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("failed to create test file %s: %v", path, err)
	}

	// Explicit chmod because WriteFile may be affected by umask
	if runtime.GOOS != osWindows {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatalf("failed to chmod test file %s: %v", path, err)
		}
	}
}

func TestNew(t *testing.T) {
	rootDir := t.TempDir()

	tx, err := New(rootDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}
	if tx == nil {
		t.Fatal("New() returned nil manager")
	}
	if !filepath.IsAbs(tx.rootPath) {
		t.Errorf("expected absolute root path, got %s", tx.rootPath)
	}
}

// TestNew_SweepsStaleTransactionDirs is a regression test for issue #35
// (SCB-032): a crash during an apply skips Commit/Rollback and leaves a
// transaction backup directory behind. New() must reclaim it on the next run.
func TestNew_SweepsStaleTransactionDirs(t *testing.T) {
	rootDir := t.TempDir()

	staleDir := filepath.Join(rootDir, fileutil.InternalDir, fileutil.TmpDir, "tx-old")
	createFile(t, filepath.Join(staleDir, "backup.bak"), "secret", fileutil.PrivateFilePerms)

	// Age the directory well past the sweep threshold.
	old := time.Now().Add(-2 * staleTransactionThreshold)
	if err := os.Chtimes(staleDir, old, old); err != nil {
		t.Fatalf("failed to age stale transaction dir: %v", err)
	}

	if _, err := New(rootDir); err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if _, err := os.Stat(staleDir); !os.IsNotExist(err) {
		t.Errorf("New() did not sweep stale transaction dir %s", staleDir)
	}
}

// TestNew_KeepsRecentTransactionDirs ensures the sweep never reclaims a
// transaction that may still be active.
func TestNew_KeepsRecentTransactionDirs(t *testing.T) {
	rootDir := t.TempDir()

	recentDir := filepath.Join(rootDir, fileutil.InternalDir, fileutil.TmpDir, "tx-recent")
	if err := os.MkdirAll(recentDir, fileutil.DirPerms); err != nil {
		t.Fatalf("failed to create recent transaction dir: %v", err)
	}

	if _, err := New(rootDir); err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	if _, err := os.Stat(recentDir); err != nil {
		t.Errorf("New() removed a recent transaction dir %s: %v", recentDir, err)
	}
}

// TestNew_SweepNoTempDir ensures New() is a no-op when no temp directory exists.
func TestNew_SweepNoTempDir(t *testing.T) {
	rootDir := t.TempDir()

	if _, err := New(rootDir); err != nil {
		t.Fatalf("New() failed when .scbake/tmp is absent: %v", err)
	}
}

func TestRollback_CreatedFiles(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	newFile := filepath.Join(rootDir, "new_feature.go")

	if err := tx.Track(newFile); err != nil {
		t.Fatalf("Track failed: %v", err)
	}

	createFile(t, newFile, "package main", fileutil.FilePerms)

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if _, err := os.Stat(newFile); !os.IsNotExist(err) {
		t.Errorf("Rollback failed to delete created file %s", newFile)
	}
}

func TestRollback_ModifiedFiles(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	targetFile := filepath.Join(rootDir, "config.json")
	originalContent := `{"version": 1}`
	originalMode := fileutil.DirPerms

	createFile(t, targetFile, originalContent, originalMode)

	if err := tx.Track(targetFile); err != nil {
		t.Fatalf("Track failed: %v", err)
	}

	createFile(t, targetFile, `{"version": 2}`, fileutil.FilePerms)

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// #nosec G304 -- test-controlled path
	content, err := os.ReadFile(targetFile)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}
	if string(content) != originalContent {
		t.Errorf("Content mismatch.\nWant: %s\nGot:  %s", originalContent, string(content))
	}

	if runtime.GOOS != osWindows {
		info, err := os.Stat(targetFile)
		if err != nil {
			t.Fatalf("failed to stat restored file: %v", err)
		}
		if info.Mode() != originalMode {
			t.Errorf("Mode mismatch. Want: %v, Got: %v", originalMode, info.Mode())
		}
	}
}

func TestRollback_NestedStructures_LIFO(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	dirA := filepath.Join(rootDir, "a")
	dirB := filepath.Join(dirA, "b")
	fileC := filepath.Join(dirB, "c.txt")

	if err := tx.Track(dirA); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dirA, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	if err := tx.Track(dirB); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dirB, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	if err := tx.Track(fileC); err != nil {
		t.Fatal(err)
	}
	createFile(t, fileC, "content", fileutil.FilePerms)

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	if _, err := os.Stat(dirA); !os.IsNotExist(err) {
		t.Errorf("Rollback failed to clean up directory structure.")
	}
}

func TestRollback_SameBasenameFiles(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	fileA := filepath.Join(rootDir, "a", "config.json")
	fileB := filepath.Join(rootDir, "b", "config.json")

	createFile(t, fileA, "A", fileutil.FilePerms)
	createFile(t, fileB, "B", fileutil.FilePerms)

	if err := tx.Track(fileA); err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(fileB); err != nil {
		t.Fatal(err)
	}

	createFile(t, fileA, "A2", fileutil.FilePerms)
	createFile(t, fileB, "B2", fileutil.FilePerms)

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// #nosec G304 -- test-controlled path
	dataA, err := os.ReadFile(fileA)
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G304 -- test-controlled path
	dataB, err := os.ReadFile(fileB)
	if err != nil {
		t.Fatal(err)
	}

	if string(dataA) != "A" || string(dataB) != "B" {
		t.Fatal("Basename collision restore failed.")
	}
}

func TestCommit_Cleanup(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	file := filepath.Join(rootDir, "test.txt")
	createFile(t, file, "data", fileutil.FilePerms)

	if err := tx.Track(file); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(tx.tempDir); os.IsNotExist(err) {
		t.Fatal("Temp dir not created")
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(tx.tempDir); !os.IsNotExist(err) {
		t.Error("Commit failed to remove temp directory")
	}
}

func TestCommit_DisablesRollback_NoBackups(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	// Track a file that does not exist yet (the "created" case). No backup is
	// made, so tempDir stays empty — previously Commit() returned early
	// without finalizing the transaction and a subsequent Rollback() deleted
	// the created file, silently discarding successful work.
	created := filepath.Join(rootDir, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(created, []byte("data"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// The deferred rollback safety net runs after Commit even on success;
	// it must not undo the committed changes.
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(created); os.IsNotExist(err) {
		t.Error("Rollback after Commit deleted a committed file (atomicity violation)")
	}

	// Commit must be idempotent.
	if err := tx.Commit(); err != nil {
		t.Errorf("second Commit failed: %v", err)
	}
}

// TestTrack_RelativePathResolvedAgainstRoot is a regression test for issue #21
// (SCB-005): a relative path passed to Track must be resolved against the
// manager's root, not against the process working directory.
func TestTrack_RelativePathResolvedAgainstRoot(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}

	// Move the process CWD to an unrelated directory. With CWD-relative
	// resolution (the historical bug) "relfile.txt" resolves outside the root
	// and is rejected as a traversal.
	otherDir := t.TempDir()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", otherDir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Errorf("failed to restore working directory: %v", err)
		}
	})

	const relPath = "relfile.txt"
	if err := tx.Track(relPath); err != nil {
		t.Fatalf("Track(%q) failed: %v", relPath, err)
	}

	want := filepath.Join(rootDir, relPath)
	found := false
	for _, created := range tx.created {
		if created == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Track(%q) recorded %v; want the path resolved under root as %s", relPath, tx.created, want)
	}

	// A relative escape must still be rejected after joining to the root.
	if err := tx.Track(filepath.Join("..", "outside.txt")); err == nil {
		t.Error("Track(\"../outside.txt\") accepted a relative path escaping the root")
	}
}

// TestTrack_InRootNameBeginningWithDots is a regression test for issue #20
// (SCB-004): a legitimate in-root file whose name merely begins with ".."
// (e.g. "..hidden.xml") must not be mistaken for a parent-directory traversal.
func TestTrack_InRootNameBeginningWithDots(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	inRoot := filepath.Join(rootDir, "..hidden.xml")
	createFile(t, inRoot, "data", fileutil.FilePerms)

	if err := tx.Track(inRoot); err != nil {
		t.Fatalf("Track(%q) rejected an in-root name beginning with dots: %v", inRoot, err)
	}

	// A genuine parent-directory escape must still be rejected.
	outside := filepath.Join(filepath.Dir(rootDir), "outside.xml")
	if err := tx.Track(outside); err == nil {
		t.Errorf("Track(%q) accepted a path outside the root", outside)
	}
}

func TestTrack_ExistingDirectory_NoBackup(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(rootDir, "existing_dir")
	if err := os.Mkdir(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	if err := tx.Track(dir); err != nil {
		t.Fatal(err)
	}

	if len(tx.backups) != 0 {
		t.Errorf("directory should not be backed up as a file")
	}

	if len(tx.created) != 0 {
		t.Errorf("existing directory was marked as created")
	}
}
