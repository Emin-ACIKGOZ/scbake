// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package manifest

import (
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"testing"
)

func TestFindProjectRoot(t *testing.T) {
	// Setup:
	// /root/ (scbake.toml)
	// /root/src/cmd/

	baseDir := t.TempDir()
	rootDir := filepath.Join(baseDir, "root")
	srcDir := filepath.Join(rootDir, "src")
	cmdDir := filepath.Join(srcDir, "cmd")

	if err := os.MkdirAll(cmdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	// Create manifest at root
	manifestPath := filepath.Join(rootDir, fileutil.ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	// Test 1: Run from deep inside
	found, err := FindProjectRoot(cmdDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != rootDir {
		t.Errorf("Deep traversal failed. Want %s, Got %s", rootDir, found)
	}

	// Test 2: Run from root itself
	found, err = FindProjectRoot(rootDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != rootDir {
		t.Errorf("Root traversal failed. Want %s, Got %s", rootDir, found)
	}
}

func TestFindProjectRoot_NestedOverride(t *testing.T) {
	// Setup:
	// /repo/scbake.toml
	// /repo/sub/scbake.toml (Should override root)
	// /repo/sub/cmd/

	baseDir := t.TempDir()
	repoDir := filepath.Join(baseDir, "repo")
	subDir := filepath.Join(repoDir, "sub")
	cmdDir := filepath.Join(subDir, "cmd")

	if err := os.MkdirAll(cmdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	// Create BOTH manifests
	if err := os.WriteFile(filepath.Join(repoDir, fileutil.ManifestFileName), []byte("root=true"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, fileutil.ManifestFileName), []byte("sub=true"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	// Run from /repo/sub/cmd -> Should stop at /repo/sub
	found, err := FindProjectRoot(cmdDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != subDir {
		t.Errorf("Nested override failed. Want %s, Got %s", subDir, found)
	}

	// Run from /repo -> Should see /repo
	found, err = FindProjectRoot(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != repoDir {
		t.Errorf("Root lookup failed. Want %s, Got %s", repoDir, found)
	}
}

func TestFindProjectRoot_FromFilePath(t *testing.T) {
	// Setup: /tmp/root/scbake.toml and /tmp/root/main.go
	// User runs Load("/tmp/root/main.go")

	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	mainGo := filepath.Join(rootDir, "main.go")
	if err := os.WriteFile(mainGo, []byte("package main"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectRoot(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	if found != rootDir {
		t.Errorf("File path input traversal failed. Want %s, Got %s", rootDir, found)
	}
}

func TestFindProjectRoot_GitFallback(t *testing.T) {
	baseDir := t.TempDir()
	gitRoot := filepath.Join(baseDir, "gitroot")
	childDir := filepath.Join(gitRoot, "child")

	if err := os.MkdirAll(childDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(gitRoot, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	found, err := FindProjectRoot(childDir)
	if err != nil {
		t.Fatal(err)
	}
	if found != gitRoot {
		t.Errorf("Git fallback failed. Want %s, Got %s", gitRoot, found)
	}
}

func TestFindProjectRoot_FallbackToStart(t *testing.T) {
	emptyDir := t.TempDir()

	found, err := FindProjectRoot(emptyDir)
	if err != nil {
		t.Fatal(err)
	}
	// Fallback should normalize to the directory itself
	if found != emptyDir {
		t.Errorf("Fallback failed. Want %s, Got %s", emptyDir, found)
	}
}

func TestLoadAndSave(t *testing.T) {
	tmpDir := t.TempDir()

	// 1. Test Load on empty dir (should default)
	m, root, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if len(m.Projects) != 0 {
		t.Error("expected empty projects")
	}
	if root != tmpDir {
		t.Errorf("Root mismatch. Want %s, Got %s", tmpDir, root)
	}

	// 2. Test Save (Atomic)
	m.Projects = append(m.Projects, types.Project{Name: "test"})
	if err := Save(m, tmpDir); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	// 3. Verify file exists
	mfPath := filepath.Join(tmpDir, fileutil.ManifestFileName)
	if _, err := os.Stat(mfPath); err != nil {
		t.Error("Manifest file was not created")
	}

	// Verify temp file is gone
	if _, err := os.Stat(mfPath + ".tmp"); !os.IsNotExist(err) {
		t.Error("Temp manifest file was not cleaned up")
	}

	// 4. Reload
	m2, _, err := Load(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m2.Projects) != 1 {
		t.Error("Persistence failed")
	}
}
