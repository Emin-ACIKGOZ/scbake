// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

func (h *MockHandlerGeneric) GetTasks(_ string) ([]types.Task, error) {
	return h.Tasks, nil
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
