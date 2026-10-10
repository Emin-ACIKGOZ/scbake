// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/templateregistry"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/templates"
	"strings"
	"testing"
)

const windowsOS = "windows"

// Resets global flag variables to their default state to prevent test-to-test pollution.
func resetFlags() {
	newLangFlag = ""
	newWithFlag = []string{}
	langFlag = ""
	withFlag = []string{}
	dryRun = false
	force = false
}

// executeCLI simulates command invocation by setting args on the root Cobra command.
func executeCLI(args ...string) error {
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

// verifyTransactionCleanup ensures the hidden transaction directory is deleted after execution.
func verifyTransactionCleanup(t *testing.T, dir string) {
	t.Helper()
	scbakeDir := filepath.Join(dir, fileutil.InternalDir)
	if _, err := os.Stat(scbakeDir); !os.IsNotExist(err) {
		t.Errorf("transaction residue found at %s", scbakeDir)
	}
}

// createMockGitScript creates a robust mock git binary that behaves based on the failCommit flag.
func createMockGitScript(t *testing.T, dir string, failCommit bool) {
	t.Helper()

	var content, name string
	failStr := "false"
	if failCommit {
		failStr = "true"
	}

	if runtime.GOOS == windowsOS {
		name = "git.bat"
		// Use findstr to catch the 'commit' subcommand regardless of flag positions.
		content = fmt.Sprintf(`@echo off
echo %%* | findstr "init" >nul && ( mkdir %s & exit /b 0 )
echo %%* | findstr "commit" >nul && ( if "%s"=="true" exit /b 1 )
exit /b 0`, fileutil.GitDir, failStr)
	} else {
		name = "git"
		// Use case matching to catch 'commit' or 'init' anywhere in the argument string.
		content = fmt.Sprintf(`#!/bin/sh
case "$*" in
  *init*) mkdir %s; exit 0 ;;
  *commit*) [ "%s" = "true" ] && exit 1 ;;
esac
exit 0`, fileutil.GitDir, failStr)
	}

	path := filepath.Join(dir, name)
	// Fix: G306 resolve by using 0700 for execution bit and adding nosec directive.
	_ = os.WriteFile(path, []byte(content), 0700) // #nosec G306
}

// MockFailTask defines a task that always returns an error for rollback testing.
type MockFailTask struct{ TargetFile string }

func (m *MockFailTask) Description() string { return "Fail Task" }
func (m *MockFailTask) Priority() int       { return 100 }
func (m *MockFailTask) Execute(tc types.TaskContext) error {
	if tc.Tx != nil {
		_ = tc.Tx.Track(m.TargetFile)
	}
	_ = os.WriteFile(m.TargetFile, []byte("CORRUPTED"), fileutil.PrivateFilePerms)
	return errors.New("fail")
}

// MockCreateTask defines a task that successfully creates a file.
type MockCreateTask struct{ TargetFile string }

func (m *MockCreateTask) Description() string { return "Create Task" }
func (m *MockCreateTask) Priority() int       { return 50 }
func (m *MockCreateTask) Execute(tc types.TaskContext) error {
	if tc.Tx != nil {
		_ = tc.Tx.Track(m.TargetFile)
	}
	return os.WriteFile(m.TargetFile, []byte("CREATED"), fileutil.PrivateFilePerms)
}

// MockHandlerGeneric allows for the injection of custom task sets into the template registry.
type MockHandlerGeneric struct{ Tasks []types.Task }

func (h *MockHandlerGeneric) GetTasks(_ string, _, _ string) ([]types.Task, error) {
	return h.Tasks, nil
}

// isolateXDGDirs points the registry config/cache at a temp dir so registry
// commands do not touch the real user config.
//
// os.UserConfigDir/os.UserCacheDir resolve differently per platform: Linux
// honours XDG_CONFIG_HOME/XDG_CACHE_HOME, macOS uses $HOME/Library/..., and
// Windows uses %AppData%/%LocalAppData%. Setting all of them keeps these tests
// isolated on every OS (relying on XDG alone made them share the real user
// config on macOS/Windows).
func isolateXDGDirs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
	t.Setenv("LocalAppData", filepath.Join(dir, "LocalAppData"))
}

// captureStdout redirects os.Stdout for the duration of fn and returns what
// was written.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer failed: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stdout failed: %v", err)
	}
	_ = r.Close()
	return string(data)
}

// TestRegistryAdd_RedactsCredentials is a regression test for SCB-034: the
// registry URL printed on 'registry add' must not leak embedded credentials.
func TestRegistryAdd_RedactsCredentials(t *testing.T) {
	isolateXDGDirs(t)

	out := captureStdout(t, func() {
		if err := executeCLI("template", "registry", "add", "acme", "https://user:pass@host/x"); err != nil {
			t.Errorf("registry add failed: %v", err)
		}
	})

	if strings.Contains(out, "pass") {
		t.Errorf("registry add output leaks credentials: %q", out)
	}
	if !strings.Contains(out, "https://host/x") {
		t.Errorf("registry add output should show the redacted URL, got %q", out)
	}
}

// TestRegistryList_RedactsCredentials is a regression test for SCB-034: the
// registry URL printed on 'registry list' must not leak embedded credentials.
func TestRegistryList_RedactsCredentials(t *testing.T) {
	isolateXDGDirs(t)

	m, err := templateregistry.NewManager()
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	if err := m.Add("acme", "https://user:pass@host/x", "", "", ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	out := captureStdout(t, func() {
		if err := executeCLI("template", "registry", "list"); err != nil {
			t.Errorf("registry list failed: %v", err)
		}
	})

	if strings.Contains(out, "pass") {
		t.Errorf("registry list output leaks credentials: %q", out)
	}
	if !strings.Contains(out, "https://host/x") {
		t.Errorf("registry list output should show the redacted URL, got %q", out)
	}
}

// --- Registration Tests ---

// Verifies that every subcommand is registered exactly once on rootCmd.
// Cobra's AddCommand does not de-duplicate, so a duplicate registration
// surfaces as a repeated name in rootCmd.Commands().
func TestRootCmd_NoDuplicateSubcommands(t *testing.T) {
	seen := make(map[string]int)
	for _, c := range rootCmd.Commands() {
		seen[c.Name()]++
	}

	for name, count := range seen {
		if count != 1 {
			t.Errorf("command %q registered %d times, want exactly 1", name, count)
		}
	}

	for _, want := range []string{"apply", "list", "new"} {
		if seen[want] != 1 {
			t.Errorf("command %q registered %d times, want exactly 1", want, seen[want])
		}
	}
}

// --- New Command Tests ---

// Verifies that 'new' fails if no language or templates are requested.
func TestNew_FailsWithoutFlags(t *testing.T) {
	resetFlags()
	tmpDir := t.TempDir()

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	err := executeCLI("new", "empty-app")
	if err == nil {
		t.Fatal("expected error when no flags are provided")
	}

	if !strings.Contains(err.Error(), "no language or templates specified") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// Verifies 'new' successfully creates a project when valid templates are provided.
func TestNew_EndToEnd_Success(t *testing.T) {
	resetFlags()

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	_ = os.Mkdir(binDir, fileutil.DirPerms)
	createMockGitScript(t, binDir, false)

	// Prepend absolute bin path to PATH to override system git.
	absBin, _ := filepath.Abs(binDir)
	t.Setenv("PATH", absBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	if err := executeCLI("new", "my-app", "--with", "git"); err != nil {
		t.Fatalf("command failed: %v", err)
	}

	projectPath := filepath.Join(tmpDir, "my-app")
	if _, err := os.Stat(filepath.Join(projectPath, fileutil.GitDir)); os.IsNotExist(err) {
		t.Error("git directory missing")
	}

	verifyTransactionCleanup(t, projectPath)
}

// Verifies that failing post-creation tasks trigger a full directory cleanup.
func TestNew_CommitFailure_CleansDirectory(t *testing.T) {
	resetFlags()

	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	_ = os.Mkdir(binDir, fileutil.DirPerms)
	createMockGitScript(t, binDir, true)

	absBin, _ := filepath.Abs(binDir)
	t.Setenv("PATH", absBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	err := executeCLI("new", "broken-app", "--with", "git")
	if err == nil {
		t.Fatal("expected commit failure")
	}

	// The project directory should be deleted if RunApply fails.
	if _, err := os.Stat(filepath.Join(tmpDir, "broken-app")); !os.IsNotExist(err) {
		t.Error("project directory should be cleaned on commit failure")
	}
}

// Verifies safety check against overwriting existing project names.
func TestNew_DirectoryAlreadyExists(t *testing.T) {
	resetFlags()

	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, "app")
	_ = os.Mkdir(existing, fileutil.DirPerms)

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	err := executeCLI("new", "app")
	if err == nil {
		t.Fatal("expected error when directory exists")
	}
}

// --- Apply Command Tests ---

// Verifies atomic rollback restores original file content on execution failure.
func TestApply_PermissionError_Rollback(t *testing.T) {
	if runtime.GOOS == windowsOS {
		t.Skip() // Permission modes (0400) behave differently on Windows.
	}

	resetFlags()

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms)

	targetFile := filepath.Join(tmpDir, "readonly.txt")
	_ = os.WriteFile(targetFile, []byte("ORIGINAL"), 0400)

	templates.Register("fail", &MockHandlerGeneric{
		Tasks: []types.Task{&MockFailTask{TargetFile: targetFile}},
	})

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	_ = executeCLI("apply", "--with", "fail", ".")

	// Validate content restoration via cleaned path to satisfy G304.
	content, _ := os.ReadFile(filepath.Clean(targetFile)) // #nosec G304
	if string(content) != "ORIGINAL" {
		t.Error("rollback failed to restore original content")
	}

	_ = os.Chmod(targetFile, fileutil.PrivateFilePerms)
	verifyTransactionCleanup(t, tmpDir)
}

// Verifies that the dry-run flag prevents any filesystem writes.
func TestApply_DryRun_NoChanges(t *testing.T) {
	resetFlags()

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms)

	targetFile := filepath.Join(tmpDir, "file.txt")

	templates.Register("create", &MockHandlerGeneric{
		Tasks: []types.Task{&MockCreateTask{TargetFile: targetFile}},
	})

	dryRun = true

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	_ = executeCLI("apply", "--with", "create", ".")

	if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
		t.Error("file should not be created in dry-run mode")
	}
}

// Verifies proper error handling for non-registered template names.
func TestApply_UnknownTemplate(t *testing.T) {
	resetFlags()

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms)

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	err := executeCLI("apply", "--with", "does-not-exist", ".")
	if err == nil {
		t.Fatal("expected error for unknown template")
	}
}

// Verifies that re-running templates does not cause errors or transaction artifacts.
func TestApply_IdempotentRun(t *testing.T) {
	resetFlags()

	tmpDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(tmpDir, fileutil.ManifestFileName), []byte(""), fileutil.PrivateFilePerms)

	targetFile := filepath.Join(tmpDir, "file.txt")

	templates.Register("create-idem", &MockHandlerGeneric{
		Tasks: []types.Task{&MockCreateTask{TargetFile: targetFile}},
	})

	oldWD, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	_ = os.Chdir(tmpDir)

	if err := executeCLI("apply", "--with", "create-idem", "."); err != nil {
		t.Fatalf("first apply failed: %v", err)
	}

	if err := executeCLI("apply", "--with", "create-idem", "."); err != nil {
		t.Fatalf("second apply failed: %v", err)
	}

	verifyTransactionCleanup(t, tmpDir)
}
