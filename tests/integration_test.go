// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package integration

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// binaryPath holds the absolute path to the compiled scbake binary.
var binaryPath string

// TestMain manages the test lifecycle: Build -> Run Tests -> Cleanup.
func TestMain(m *testing.M) {
	fmt.Println("[Setup] Building scbake binary for integration testing...")

	// Create a temp directory for the build artifact to ensure isolation
	tmpDir, err := os.MkdirTemp("", "scbake-integration-build")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp dir: %v\n", err)
		os.Exit(fileutil.ExitError)
	}

	// Handle Windows executable extension
	binName := "scbake-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binaryPath = filepath.Join(tmpDir, binName)

	// G204: We use nolint because compiling the project binary for testing
	// requires variable paths which gosec flags as unsafe.
	//nolint:gosec // Intended build of test binary
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, "../")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Build failed: %v\nOutput:\n%s\n", err, out)
		os.Exit(fileutil.ExitError)
	}

	exitCode := m.Run()

	_ = os.RemoveAll(tmpDir)
	os.Exit(exitCode)
}

// runCLI executes the compiled binary with a forced Git identity via environment variables.
// This prevents the tests from ever touching the user's global ~/.gitconfig.
func runCLI(args ...string) (string, error) {
	// G204: The binaryPath is internally managed by the test suite.
	//nolint:gosec // Intended execution of test binary
	cmd := exec.CommandContext(context.Background(), binaryPath, args...)

	// ISOLATION: Inject Git identity directly into the process environment.
	// This overrides ~/.gitconfig without modifying the user's machine state.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test Runner",
		"GIT_AUTHOR_EMAIL=runner@test.com",
		"GIT_COMMITTER_NAME=Test Runner",
		"GIT_COMMITTER_EMAIL=runner@test.com",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String() + stderr.String(), err
}

// --- Basic Command Tests ---

// TestListCommand verifies the 'list' subcommand.
func TestListCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantContain string
		wantError   bool
	}{
		{"List Languages", []string{"list", "langs"}, "go", false},
		{"List Templates", []string{"list", "templates"}, "makefile", false},
		{"Unknown Resource", []string{"list", "not-exist"}, "Unknown resource type", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runCLI(tt.args...)

			if tt.wantError {
				if err == nil {
					t.Errorf("expected error, but got nil")
				}
				// Assert non-zero exit code for failures
				if exitErr, ok := err.(*exec.ExitError); ok {
					if exitErr.ExitCode() == 0 {
						t.Error("expected non-zero exit code for failure")
					}
				}
			} else if err != nil {
				t.Errorf("unexpected error: %v", err)
			}

			if !strings.Contains(output, tt.wantContain) {
				t.Errorf("output missing expected string '%s'", tt.wantContain)
			}
		})
	}
}

// --- Monorepo & Polyglot Tests ---

func TestMonorepoScenarios(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	repoName := "main-repo"
	repoPath := filepath.Join(tmpDir, repoName)

	t.Run("Full Polyglot Lifecycle", func(t *testing.T) {
		// 1. Root init
		_, err := runCLI("new", repoName, "--with", "git,makefile")
		if err != nil {
			t.Fatalf("Root init failed: %v", err)
		}

		// 2. Subprojects
		_, err = runCLI("apply", "--lang", "go", filepath.Join(repoPath, "backend"))
		if err != nil {
			t.Fatalf("Backend failed: %v", err)
		}
		_, err = runCLI("apply", "--lang", "svelte", filepath.Join(repoPath, "frontend"))
		if err != nil {
			t.Fatalf("Frontend failed: %v", err)
		}

		// 3. Final verification of orchestrated targets
		_, _ = runCLI("apply", "--with", "makefile", repoPath)

		// G304: repoPath is a test-controlled temporary directory.
		//nolint:gosec
		makefile, _ := os.ReadFile(filepath.Join(repoPath, "Makefile"))
		content := string(makefile)
		if !strings.Contains(content, "MAKE_BUILD_COMMAND_go") || !strings.Contains(content, "MAKE_BUILD_COMMAND_svelte") {
			t.Error("Makefile failed to unify subprojects")
		}
	})
}

// --- Safety & Rollback Tests ---

func TestRollbackIntegrity(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	// TIGHTENING: Ensure WD is restored even if test panics or fails
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	t.Run("Directory Cleanup on Invalid Input", func(t *testing.T) {
		projectName := "ghost-project"
		output, err := runCLI("new", projectName, "--lang", "invalid-lang")

		// Assert failure logic
		if err == nil {
			t.Error("Command should have failed due to invalid language")
		}

		// LOGICAL VALIDATION: Directory must not exist after rollback
		if _, err := os.Stat(filepath.Join(tmpDir, projectName)); !os.IsNotExist(err) {
			t.Errorf("Rollback failed to remove directory. Output: %s", output)
		}
	})
}

// --- State Verification Helpers ---

type projectExpectations struct {
	projectName string
	expectGit   bool
	expectGoMod bool
}

// TestNewCommandBasics verifies the 'new' subcommand with various combinations.
func TestNewCommandBasics(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })

	tests := []struct {
		name string
		args []string
		exp  projectExpectations
	}{
		{"Pure Go", []string{"new", "only-go", "--lang", "go"}, projectExpectations{"only-go", false, true}},
		{"Pure Git", []string{"new", "only-git", "--with", "git"}, projectExpectations{"only-git", true, false}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Chdir(tmpDir)

			output, err := runCLI(tt.args...)
			if err != nil {
				t.Fatalf("scbake new failed: %v\nOutput: %s", err, output)
			}

			verifyProjectState(t, tmpDir, tt.exp)

			// Idempotency check: creating the same project name should fail.
			if _, err2 := runCLI(tt.args...); err2 == nil {
				t.Errorf("expected failure when re-running 'scbake new' for '%s'", tt.exp.projectName)
			}
		})
	}
}

// verifyProjectState validates project structure while keeping cyclomatic complexity low.
func verifyProjectState(t *testing.T, tmpDir string, exp projectExpectations) {
	t.Helper()

	projectPath := filepath.Join(tmpDir, exp.projectName)
	mustExist(t, projectPath, "project directory")
	mustExist(t, filepath.Join(projectPath, fileutil.ManifestFileName), "manifest file")

	checkOptional(t, filepath.Join(projectPath, fileutil.GitDir), exp.expectGit, ".git folder")
	checkOptional(t, filepath.Join(projectPath, "go.mod"), exp.expectGoMod, "go.mod file")
}

func mustExist(t *testing.T, path, label string) {
	t.Helper()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("%s missing at %s", label, path)
	}
}

func checkOptional(t *testing.T, path string, shouldExist bool, label string) {
	t.Helper()

	_, err := os.Stat(path)
	exists := !os.IsNotExist(err)

	if shouldExist && !exists {
		t.Errorf("expected %s, but missing", label)
	}
	if !shouldExist && exists {
		t.Errorf("unexpected %s found", label)
	}
}
