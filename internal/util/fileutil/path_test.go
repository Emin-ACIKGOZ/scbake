// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package fileutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// skipIfNoSymlinks skips tests that require unprivileged symlink creation.
func skipIfNoSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}
}

func TestSafePath(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name       string
		baseDir    string
		targetPath string
		wantErr    bool
	}{
		{
			name:       "valid relative path",
			baseDir:    tmpDir,
			targetPath: "services/api/go.mod",
			wantErr:    false,
		},
		{
			name:       "valid same directory",
			baseDir:    tmpDir,
			targetPath: ".",
			wantErr:    false,
		},
		{
			name:       "valid nested relative path",
			baseDir:    tmpDir,
			targetPath: "./apps/web/package.json",
			wantErr:    false,
		},
		{
			name:       "path traversal parent directory",
			baseDir:    tmpDir,
			targetPath: "../secret.txt",
			wantErr:    true,
		},
		{
			name:       "path traversal multi level escape",
			baseDir:    tmpDir,
			targetPath: "../../etc/passwd",
			wantErr:    true,
		},
		{
			name:       "path traversal nested escape",
			baseDir:    tmpDir,
			targetPath: "sub/../../outside.txt",
			wantErr:    true,
		},
		{
			name:       "foreign unix absolute path",
			baseDir:    tmpDir,
			targetPath: "/tmp/escape.tpl",
			wantErr:    true,
		},
		{
			name:       "foreign windows absolute path",
			baseDir:    tmpDir,
			targetPath: `\tmp\escape.tpl`,
			wantErr:    true,
		},
		{
			name:       "empty base directory",
			baseDir:    "",
			targetPath: "file.txt",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SafePath(tt.baseDir, tt.targetPath)
			if (err != nil) != tt.wantErr {
				t.Errorf("SafePath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if !filepath.IsAbs(got) {
					t.Errorf("SafePath() returned non-absolute path: %s", got)
				}
			}
		})
	}
}

func TestSafeReadWriteFile(t *testing.T) {
	tmpDir := t.TempDir()
	fileName := "test.txt"
	content := []byte("hello world")

	// Test SafeWriteFile
	if err := SafeWriteFile(tmpDir, fileName, content, PrivateFilePerms); err != nil {
		t.Fatalf("SafeWriteFile failed: %v", err)
	}

	// Test SafeReadFile
	got, err := SafeReadFile(tmpDir, fileName)
	if err != nil {
		t.Fatalf("SafeReadFile failed: %v", err)
	}
	if string(got) != string(content) {
		t.Errorf("SafeReadFile content mismatch: got %q, want %q", string(got), string(content))
	}

	// Test SafeOpenFile
	f, err := SafeOpenFile(tmpDir, fileName, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("SafeOpenFile failed: %v", err)
	}
	_ = f.Close()

	// Test Traversal Rejection on Write
	if err := SafeWriteFile(tmpDir, "../escape.txt", content, PrivateFilePerms); err == nil {
		t.Errorf("SafeWriteFile should reject traversal path")
	}

	// Test Traversal Rejection on Read
	if _, err := SafeReadFile(tmpDir, "../escape.txt"); err == nil {
		t.Errorf("SafeReadFile should reject traversal path")
	}
}

// TestSafePath_SymlinkDirectoryEscapeRejected is a regression test for SCB-001:
// a lexical-only containment check accepts a symlinked directory inside the
// root that points outside it, letting later operations escape the root.
func TestSafePath_SymlinkDirectoryEscapeRejected(t *testing.T) {
	skipIfNoSymlinks(t)

	outside := t.TempDir()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), FilePerms); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	for _, target := range []string{"escape", "escape/secret.txt"} {
		if got, err := SafePath(root, target); err == nil {
			t.Errorf("SafePath(%q, %q) = %q, want rejection for escaping symlink", root, target, got)
		}
	}
}

// TestSafePath_SymlinkLeafEscapeRejected covers a symlink as the final component.
func TestSafePath_SymlinkLeafEscapeRejected(t *testing.T) {
	skipIfNoSymlinks(t)

	outside := t.TempDir()
	root := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), FilePerms); err != nil {
		t.Fatalf("seed outside file: %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if got, err := SafePath(root, "link.txt"); err == nil {
		t.Errorf("SafePath accepted an escaping symlinked leaf: %q", got)
	}
}

// TestSafePath_DanglingSymlinkEscapeRejected verifies that a symlink whose
// target does not exist yet is still resolved, rather than being mistaken for a
// non-existent in-root path.
func TestSafePath_DanglingSymlinkEscapeRejected(t *testing.T) {
	skipIfNoSymlinks(t)

	outside := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "newdir"), filepath.Join(root, "escape")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if got, err := SafePath(root, "escape/file.txt"); err == nil {
		t.Errorf("SafePath accepted a dangling escaping symlink: %q", got)
	}
}

// TestSafePath_InRootSymlinkAllowed verifies that a symlink resolving back
// inside the root is transparent and accepted.
func TestSafePath_InRootSymlinkAllowed(t *testing.T) {
	skipIfNoSymlinks(t)

	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, DirPerms); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	got, err := SafePath(root, "link/file.txt")
	if err != nil {
		t.Fatalf("in-root symlink was rejected: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("SafePath returned non-absolute path: %s", got)
	}
}

// TestSafeWriteFile_SymlinkDirectoryEscapeRejected verifies the write sink does
// not follow an escaping symlinked directory.
func TestSafeWriteFile_SymlinkDirectoryEscapeRejected(t *testing.T) {
	skipIfNoSymlinks(t)

	outside := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if err := SafeWriteFile(root, filepath.Join("escape", "pwned.txt"), []byte("x"), FilePerms); err == nil {
		t.Error("SafeWriteFile wrote through an escaping symlinked directory")
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); !os.IsNotExist(err) {
		t.Error("SafeWriteFile created a file outside the root")
	}
}

// TestSafeReadWriteFile_SymlinkLeafRejected verifies O_NOFOLLOW rejects a
// symlinked final component even when it resolves inside the root.
func TestSafeReadWriteFile_SymlinkLeafRejected(t *testing.T) {
	skipIfNoSymlinks(t)

	root := t.TempDir()
	real := filepath.Join(root, "real.txt")
	if err := os.WriteFile(real, []byte("data"), FilePerms); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(root, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	if _, err := SafeReadFile(root, "link.txt"); err == nil {
		t.Error("SafeReadFile followed a symlinked final component")
	}
	if err := SafeWriteFile(root, "link.txt", []byte("pwned"), FilePerms); err == nil {
		t.Error("SafeWriteFile followed a symlinked final component")
	}
	if f, err := SafeOpenFile(root, "link.txt", os.O_RDONLY, 0); err == nil {
		_ = f.Close()
		t.Error("SafeOpenFile followed a symlinked final component")
	}

	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("symlink target was modified: %q", got)
	}
}

// TestSafeWriteFile_HardlinkedTargetRejected is a regression test for SCB-003: a
// hard link inside the root shares its inode with a file outside it.
func TestSafeWriteFile_HardlinkedTargetRejected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard links are not portable in this test")
	}

	outside := t.TempDir()
	root := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("original"), FilePerms); err != nil {
		t.Fatalf("seed victim: %v", err)
	}
	if err := os.Link(victim, filepath.Join(root, "hardlink.txt")); err != nil {
		t.Skipf("hard links unsupported: %v", err)
	}

	if err := SafeWriteFile(root, "hardlink.txt", []byte("pwned"), FilePerms); err == nil {
		t.Error("SafeWriteFile wrote through a hard link")
	}

	got, err := os.ReadFile(victim)
	if err != nil {
		t.Fatalf("read victim: %v", err)
	}
	if string(got) != "original" {
		t.Errorf("hard-link victim was modified: %q", got)
	}
}
