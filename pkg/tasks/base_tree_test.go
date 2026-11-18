// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/util/fileutil"
)

// readTestFile returns the contents of a test-controlled path.
func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	//nolint:gosec // test-controlled temp path
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

// requireBaseSymlinkError asserts the fail-closed diagnostic names the base
// path and explains why a symlinked base directory is refused.
func requireBaseSymlinkError(t *testing.T, err error, wantPath string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected a symlinked base tree to be rejected")
	}
	if !strings.Contains(err.Error(), wantPath) {
		t.Errorf("error %q does not name the offending path %q", err, wantPath)
	}
	if !strings.Contains(err.Error(), "symlinked base directory is not allowed") {
		t.Errorf("error %q does not explain that a symlinked base directory is not allowed", err)
	}
}

// TestWriteManagedFileRejectsSymlinkedBaseRoot is the regression guard for
// #222: <root>/.scbake-base -> src resolves inside the project root, so SafePath
// accepts it, but the committed base write must not follow it into src and
// overwrite a user file.
func TestWriteManagedFileRejectsSymlinkedBaseRoot(t *testing.T) {
	requireSymlinks(t)

	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	local := []byte("LOCAL WORK: keep me\n")
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, fileutil.DirPerms); err != nil {
		t.Fatalf("create src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "Makefile"), local, 0o600); err != nil {
		t.Fatalf("write local src/Makefile: %v", err)
	}

	existing := []byte("tracked target\n")
	if err := os.WriteFile(filepath.Join(root, "Makefile"), existing, 0o600); err != nil {
		t.Fatalf("write target Makefile: %v", err)
	}
	tc.Manifest.ManagedFiles = map[string]string{"Makefile": HashContent(existing)}

	baseRoot := filepath.Join(root, fileutil.CommittedBaseDir)
	if err := os.Symlink("src", baseRoot); err != nil {
		t.Fatalf("symlink base root: %v", err)
	}

	err := WriteManagedFile(tc, "Makefile", []byte("all:\n"), 0o600)
	requireBaseSymlinkError(t, err, baseRoot)

	if got := readTestFile(t, filepath.Join(srcDir, "Makefile")); string(got) != string(local) {
		t.Fatalf("redirected base write changed src/Makefile: got %q, want %q", got, local)
	}
	if got := readTestFile(t, filepath.Join(root, "Makefile")); string(got) != string(existing) {
		t.Fatalf("rejected write changed the target Makefile: got %q, want %q", got, existing)
	}

	if _, err := ReadBaseShadow(root, "Makefile"); err == nil {
		t.Fatal("ReadBaseShadow accepted a symlinked base root")
	}

	info, err := os.Lstat(baseRoot)
	if err != nil {
		t.Fatalf("lstat base root: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("test setup: base root is no longer a symlink")
	}
}

// TestWriteManagedFileRejectsSymlinkInsideBaseTree covers a redirected parent
// directory below a real base root: .scbake-base/sub -> ../../src must not let a
// write for sub/Makefile escape into src/Makefile.
func TestWriteManagedFileRejectsSymlinkInsideBaseTree(t *testing.T) {
	requireSymlinks(t)

	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	local := []byte("LOCAL WORK: keep me\n")
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, fileutil.DirPerms); err != nil {
		t.Fatalf("create src: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "Makefile"), local, 0o600); err != nil {
		t.Fatalf("write local src/Makefile: %v", err)
	}

	baseRoot := filepath.Join(root, fileutil.CommittedBaseDir)
	if err := os.MkdirAll(baseRoot, fileutil.DirPerms); err != nil {
		t.Fatalf("create base root: %v", err)
	}
	redirect := filepath.Join(baseRoot, "sub")
	if err := os.Symlink(filepath.Join("..", "src"), redirect); err != nil {
		t.Fatalf("symlink base subdirectory: %v", err)
	}

	err := WriteManagedFile(tc, filepath.Join("sub", "Makefile"), []byte("all:\n"), 0o600)
	requireBaseSymlinkError(t, err, redirect)

	if got := readTestFile(t, filepath.Join(srcDir, "Makefile")); string(got) != string(local) {
		t.Fatalf("redirected base write changed src/Makefile: got %q, want %q", got, local)
	}
	if _, err := os.Stat(filepath.Join(root, "sub", "Makefile")); !os.IsNotExist(err) {
		t.Fatalf("rejected write created the target file (stat err: %v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, "sub")); !os.IsNotExist(err) {
		t.Fatalf("rejected write created the target parent directory (stat err: %v)", err)
	}
}

// TestWriteManagedFileAllowsRealBaseTree is the negative control: an ordinary
// base tree is still created and read back byte-for-byte.
func TestWriteManagedFileAllowsRealBaseTree(t *testing.T) {
	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	content := []byte("all:\n\t@echo hi\n")
	if err := WriteManagedFile(tc, "Makefile", content, 0o600); err != nil {
		t.Fatalf("WriteManagedFile: %v", err)
	}

	baseRoot := filepath.Join(root, fileutil.CommittedBaseDir)
	info, err := os.Lstat(baseRoot)
	if err != nil {
		t.Fatalf("lstat base root: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		t.Fatalf("base root is not a real directory: mode %v", info.Mode())
	}

	base, err := ReadBaseShadow(root, "Makefile")
	if err != nil {
		t.Fatalf("ReadBaseShadow: %v", err)
	}
	if string(base) != string(content) {
		t.Fatalf("base = %q, want %q", base, content)
	}
	if got := readTestFile(t, filepath.Join(root, "Makefile")); string(got) != string(content) {
		t.Fatalf("target = %q, want %q", got, content)
	}
}
