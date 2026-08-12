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
		stepInitMonorepoRoot(t, repoName, repoPath)
	})

	t.Run("2. Add Subprojects from Root", func(t *testing.T) {
		stepAddSubprojects(t, repoPath)
	})

	t.Run("3. Verify Centralized Manifest", func(t *testing.T) {
		stepVerifyCentralizedManifest(t, repoPath)
	})

	t.Run("4. Apply Subproject Template from Root", func(t *testing.T) {
		stepApplySubprojectTemplate(t, repoPath)
	})

	t.Run("5. Transaction Safety Across Monorepo", func(t *testing.T) {
		stepVerifyTransactionSafety(t, repoPath)
	})
}

func stepInitMonorepoRoot(t *testing.T, repoName, repoPath string) {
	output, err := runCLI("new", repoName, "--with", "git,makefile,editorconfig")
	if err != nil {
		t.Fatalf("Failed to init root: %v\nOutput: %s", err, output)
	}

	if _, err := os.Stat(filepath.Join(repoPath, "scbake.toml")); err != nil {
		t.Errorf("Root manifest missing: %v", err)
	}
}

func stepAddSubprojects(t *testing.T, repoPath string) {
	_ = os.Chdir(repoPath)

	output, err := runCLI("apply", "--lang", "go", "services/api")
	if err != nil {
		t.Fatalf("Failed to add go service: %v\nOutput: %s", err, output)
	}

	output, err = runCLI("apply", "--lang", "svelte", "apps/web")
	if err != nil {
		t.Fatalf("Failed to add svelte app: %v\nOutput: %s", err, output)
	}

	if _, err := os.Stat(filepath.Join("services", "api", "go.mod")); err != nil {
		t.Errorf("Go service not created correctly: %v", err)
	}
	if _, err := os.Stat(filepath.Join("apps", "web", "package.json")); err != nil {
		t.Errorf("Svelte app not created correctly: %v", err)
	}
}

func stepVerifyCentralizedManifest(t *testing.T, repoPath string) {
	_ = os.Chdir(repoPath)

	output, err := runCLI("list", "projects")
	if err != nil {
		t.Fatalf("Failed to list projects: %v\nOutput: %s", err, output)
	}

	if !strings.Contains(output, "api") || !strings.Contains(output, "web") {
		t.Errorf("Manifest failed to track all projects. Output: %s", output)
	}
}

func stepApplySubprojectTemplate(t *testing.T, repoPath string) {
	_ = os.Chdir(repoPath)

	output, err := runCLI("apply", "--with", "go_linter", "services/api")
	if err != nil {
		t.Fatalf("Failed to apply template to subdir: %v\nOutput: %s", err, output)
	}

	if _, err := os.Stat(filepath.Join("services", "api", ".golangci.yml")); err != nil {
		t.Errorf("Linter config missing from subdir: %v", err)
	}
	if _, err := os.Stat(".golangci.yml"); err == nil {
		t.Error("Linter config incorrectly created at root")
	}
}

func stepVerifyTransactionSafety(t *testing.T, repoPath string) {
	_ = os.Chdir(repoPath)

	dummyFile := filepath.Join(repoPath, "dummy.txt")
	_ = os.WriteFile(dummyFile, []byte("i am a file"), 0600)

	_, err := runCLI("apply", "--lang", "go", "dummy.txt")
	if err == nil {
		t.Fatal("Expected failure when applying to a file path")
	}

	output, _ := runCLI("list", "projects")
	if strings.Contains(output, "dummy.txt") {
		t.Error("Rollback failed: dummy.txt added to projects list")
	}
}
