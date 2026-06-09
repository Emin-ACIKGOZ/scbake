// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMonorepoGovernance verifies that scbake correctly manages multiple projects
// and templates across a monorepo structure using a single root manifest.
func TestMonorepoGovernance(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	repoName := "enterprise-monorepo"
	repoPath := filepath.Join(tmpDir, repoName)

	t.Run("1. Initialize Monorepo Root", func(t *testing.T) {
		output, err := runCLI("new", repoName, "--with", "git,makefile,editorconfig")
		if err != nil {
			t.Fatalf("Failed to init root: %v\nOutput: %s", err, output)
		}

		// Verify root manifest exists
		if _, err := os.Stat(filepath.Join(repoPath, "scbake.toml")); err != nil {
			t.Errorf("Root manifest missing: %v", err)
		}
	})

	t.Run("2. Add Subprojects from Root", func(t *testing.T) {
		_ = os.Chdir(repoPath)

		// Add a Go service in a subdirectory
		output, err := runCLI("apply", "--lang", "go", "services/api")
		if err != nil {
			t.Fatalf("Failed to add go service: %v\nOutput: %s", err, output)
		}

		// Add a Svelte app in another subdirectory
		output, err = runCLI("apply", "--lang", "svelte", "apps/web")
		if err != nil {
			t.Fatalf("Failed to add svelte app: %v\nOutput: %s", err, output)
		}

		// Verify files created in subdirs
		if _, err := os.Stat(filepath.Join("services", "api", "go.mod")); err != nil {
			t.Errorf("Go service not created correctly: %v", err)
		}
		if _, err := os.Stat(filepath.Join("apps", "web", "package.json")); err != nil {
			t.Errorf("Svelte app not created correctly: %v", err)
		}
	})

	t.Run("3. Verify Centralized Manifest", func(t *testing.T) {
		_ = os.Chdir(repoPath)
		
		output, err := runCLI("list", "projects")
		if err != nil {
			t.Fatalf("Failed to list projects: %v\nOutput: %s", err, output)
		}

		if !strings.Contains(output, "api") || !strings.Contains(output, "web") {
			t.Errorf("Manifest failed to track all projects. Output: %s", output)
		}
	})

	t.Run("4. Apply Subproject Template from Root", func(t *testing.T) {
		_ = os.Chdir(repoPath)

		// Apply a linter specifically to the API service
		output, err := runCLI("apply", "--with", "go_linter", "services/api")
		if err != nil {
			t.Fatalf("Failed to apply template to subdir: %v\nOutput: %s", err, output)
		}

		// Verify linter config is in the subdir, not the root
		if _, err := os.Stat(filepath.Join("services", "api", ".golangci.yml")); err != nil {
			t.Errorf("Linter config missing from subdir: %v", err)
		}
		if _, err := os.Stat(".golangci.yml"); err == nil {
			t.Error("Linter config incorrectly created at root")
		}
	})

	t.Run("5. Transaction Safety Across Monorepo", func(t *testing.T) {
		_ = os.Chdir(repoPath)

		// Try to apply a template with an invalid configuration that causes failure
		// This should roll back any changes made during the process.
		// We use a known failure case: applying a template with a required var missing (if we had one)
		// Or just applying something to a path that isn't a directory.
		
		dummyFile := filepath.Join(repoPath, "dummy.txt")
		_ = os.WriteFile(dummyFile, []byte("i am a file"), 0644)
		
		_, err := runCLI("apply", "--lang", "go", "dummy.txt")
		if err == nil {
			t.Fatal("Expected failure when applying to a file path")
		}

		// Verify scbake.toml still only has the original projects (didn't add "dummy.txt")
		output, _ := runCLI("list", "projects")
		if strings.Contains(output, "dummy.txt") {
			t.Error("Rollback failed: dummy.txt added to projects list")
		}
	})
}
