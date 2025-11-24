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
	"strings"
	"testing"
)

// binaryPath holds the absolute path to the compiled scbake binary.
var binaryPath string

// TestMain manages the test lifecycle: Build -> Run Tests -> Cleanup.
func TestMain(m *testing.M) {
	// 1. Setup: Compile the binary
	fmt.Println("[Setup] Building scbake binary for integration testing...")

	// Create a temp directory for the build artifact
	tmpDir, err := os.MkdirTemp("", "scbake-integration-build")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create temp dir: %v\n", err)
		os.Exit(1)
	}

	// Handle Windows executable extension
	binName := "scbake-test"
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}
	binaryPath = filepath.Join(tmpDir, binName)

	// Build the project (targeting the root directory "../")
	// -tags test ensures we don't accidentally pull in dev dependencies if you had any
	//nolint:gosec // Test runner needs to build the binary using variable paths
	buildCmd := exec.CommandContext(context.Background(), "go", "build", "-o", binaryPath, "../")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "Build failed: %v\nOutput:\n%s\n", err, out)
		os.Exit(1)
	}

	// 2. Execution: Run the tests
	exitCode := m.Run()

	// 3. Teardown: Cleanup binary
	_ = os.RemoveAll(tmpDir)

	// Exit with the correct code
	os.Exit(exitCode)
}

// runCLI executes the compiled binary with the provided arguments.
// It returns the combined stdout/stderr and any execution error.
func runCLI(args ...string) (string, error) {
	//nolint:gosec // Test runner must execute the compiled binary to verify functionality
	cmd := exec.CommandContext(context.Background(), binaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String() + stderr.String()
	return output, err
}

// TestListCommand verifies the 'list' subcommand.
func TestListCommand(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantContain string
		wantError   bool
	}{
		{
			name:        "List Languages",
			args:        []string{"list", "langs"},
			wantContain: "go", // We know 'go' is a supported language
			wantError:   false,
		},
		{
			name:        "List Templates",
			args:        []string{"list", "templates"},
			wantContain: "makefile", // We know 'makefile' is a template
			wantError:   false,
		},
		{
			name:        "Unknown Resource",
			args:        []string{"list", "not-exist"},
			wantContain: "Unknown resource type",
			wantError:   true, // Should fail with exit code 1
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runCLI(tt.args...)

			// Check exit code expectation
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error/exit-code, but got nil")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v. Output: %s", err, output)
				}
			}

			// Check output content
			if !strings.Contains(output, tt.wantContain) {
				t.Errorf("output missing expected string '%s'. Got:\n%s", tt.wantContain, output)
			}
		})
	}
}

// TestNewCommand verifies the 'new' subcommand.
// It simulates a user creating a new project from scratch.
func TestNewCommand(t *testing.T) {
	// Setup a clean workspace for this test run
	tmpDir := t.TempDir()

	// Switch to tmpDir so 'scbake new' creates the project there.
	// We defer switching back to the original directory.
	originalWd, _ := os.Getwd()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	projectName := "my-test-app"

	// Execute: scbake new my-test-app --lang go
	// This should create the dir, init git, and run 'go mod init'
	output, err := runCLI("new", projectName, "--lang", "go")
	if err != nil {
		t.Fatalf("scbake new failed: %v\nOutput:\n%s", err, output)
	}

	// --- Verification ---

	// 1. Directory created?
	projectPath := filepath.Join(tmpDir, projectName)
	if _, err := os.Stat(projectPath); os.IsNotExist(err) {
		t.Fatalf("Project directory was not created at %s", projectPath)
	}

	// 2. Git initialized?
	if _, err := os.Stat(filepath.Join(projectPath, ".git")); os.IsNotExist(err) {
		t.Error("Git repository not initialized (.git missing)")
	}

	// 3. Go language pack applied? (go.mod existence)
	if _, err := os.Stat(filepath.Join(projectPath, "go.mod")); os.IsNotExist(err) {
		t.Error("Go language pack failed: go.mod missing")
	}

	// 4. Manifest created? (scbake.toml existence)
	if _, err := os.Stat(filepath.Join(projectPath, "scbake.toml")); os.IsNotExist(err) {
		t.Error("Manifest file (scbake.toml) missing")
	}

	// 5. Idempotency / Safety check
	// Running 'new' again on an existing directory should fail.
	_, err2 := runCLI("new", projectName, "--lang", "go")
	if err2 == nil {
		t.Error("scbake new should fail if directory exists, but it succeeded")
	}
}
