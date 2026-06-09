// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: gpl3-or-later

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestGovernanceLifecycle tests a complex, realistic lifecycle for compliance and community templates:
// 1. Initial creation (new repo)
// 2. Simulating user drift/modifications
// 3. Updating standards via apply
// 4. Overwriting templates vs surgical appending
//nolint:cyclop // Integration tests naturally have high complexity
func TestGovernanceLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	projectName := "corp-service"
	projectPath := filepath.Join(tmpDir, projectName)

	t.Run("1. Initial Creation", func(t *testing.T) {
		output, err := runCLI("new", projectName, "--with", "compliance,community", "--license", "MIT", "--copyright-holder", "Alice Inc")
		if err != nil {
			t.Fatalf("Failed to create project: %v\nOutput: %s", err, output)
		}

		// Verify License
		//nolint:gosec // Test code
		licenseBytes, err := os.ReadFile(filepath.Join(projectPath, "LICENSE"))
		if err != nil {
			t.Fatalf("LICENSE not created: %v", err)
		}
		licenseText := string(licenseBytes)
		if !strings.Contains(licenseText, "MIT License") {
			t.Errorf("LICENSE is not MIT")
		}
		if !strings.Contains(licenseText, "Alice Inc") {
			t.Errorf("LICENSE missing copyright holder")
		}

		// Verify CODEOWNERS
		//nolint:gosec // Test code
		ownersBytes, err := os.ReadFile(filepath.Join(projectPath, ".github", "CODEOWNERS"))
		if err != nil {
			t.Fatalf("CODEOWNERS not created: %v", err)
		}
		if !strings.Contains(string(ownersBytes), "* @maintainers") {
			t.Errorf("CODEOWNERS missing maintainers entry")
		}
	})

	t.Run("2. Simulate Drift and Customization", func(_ *testing.T) {
		// User adds a custom team to CODEOWNERS
		ownersPath := filepath.Join(projectPath, ".github", "CODEOWNERS")
		//nolint:gosec // Just a test
		f, _ := os.OpenFile(ownersPath, os.O_APPEND|os.O_WRONLY, 0644)
		_, _ = f.WriteString("docs/ @tech-writers\n")
		_ = f.Close()

		// User modifies SECURITY.md
		secPath := filepath.Join(projectPath, "SECURITY.md")
		//nolint:gosec // Just a test
		_ = os.WriteFile(secPath, []byte("# Custom Security\nContact us directly."), 0644)
	})

	t.Run("3. Failed Update (Safety Check)", func(t *testing.T) {
		_ = os.Chdir(projectPath)

		// Try to apply an update without --force. Should fail because SECURITY.md and LICENSE exist.
		output, err := runCLI("apply", "--with", "compliance", "--license", "Apache-2.0", "--copyright-holder", "Bob Corp")
		if err == nil {
			t.Fatalf("Expected apply to fail due to existing files, but it succeeded.")
		}
		if !strings.Contains(output, "file already exists") {
			t.Errorf("Expected 'file already exists' error, got: %s", output)
		}
	})

	t.Run("4. Successful Force Update", func(t *testing.T) {
		_ = os.Chdir(projectPath)

		// Apply update with --force
		output, err := runCLI("apply", "--with", "compliance", "--force", "--license", "Apache-2.0", "--copyright-holder", "Bob Corp")
		if err != nil {
			t.Fatalf("Failed to apply update: %v\nOutput: %s", err, output)
		}

		// Verify License changed to Apache and new holder
		licenseBytes, _ := os.ReadFile("LICENSE")
		licenseText := string(licenseBytes)
		if !strings.Contains(licenseText, "Apache License") {
			t.Errorf("LICENSE was not updated to Apache")
		}
		if !strings.Contains(licenseText, "Bob Corp") {
			t.Errorf("LICENSE copyright holder was not updated")
		}
		if strings.Contains(licenseText, "Alice Inc") {
			t.Errorf("Old copyright holder still present in LICENSE")
		}

		// Verify SECURITY.md was reset to template standard
		secBytes, _ := os.ReadFile("SECURITY.md")
		secText := string(secBytes)
		if !strings.Contains(secText, "Reporting a Vulnerability") {
			t.Errorf("SECURITY.md was not reset to standard template")
		}

		// Verify CODEOWNERS preserved custom lines but didn't duplicate the base line
		ownersBytes, _ := os.ReadFile(filepath.Join(".github", "CODEOWNERS"))
		ownersText := string(ownersBytes)
		if !strings.Contains(ownersText, "docs/ @tech-writers") {
			t.Errorf("CODEOWNERS lost custom append")
		}
		if strings.Count(ownersText, "* @maintainers") != 1 {
			t.Errorf("CODEOWNERS duplicated base entry")
		}
	})
}
