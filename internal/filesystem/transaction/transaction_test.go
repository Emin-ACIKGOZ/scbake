// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package transaction

import (
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"testing"
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
