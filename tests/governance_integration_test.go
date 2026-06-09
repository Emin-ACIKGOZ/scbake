package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

		//nolint:gosec // Test project path
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

		//nolint:gosec // Test project path
		ownersBytes, err := os.ReadFile(filepath.Join(projectPath, ".github", "CODEOWNERS"))
		if err != nil {
			t.Fatalf("CODEOWNERS not created: %v", err)
		}
		if !strings.Contains(string(ownersBytes), "* @maintainers") {
			t.Errorf("CODEOWNERS missing maintainers entry")
		}
	})

	t.Run("2. Simulate Drift and Customization", func(_ *testing.T) {
		ownersPath := filepath.Join(projectPath, ".github", "CODEOWNERS")
		//nolint:gosec // Test project path
		f, _ := os.OpenFile(ownersPath, os.O_APPEND|os.O_WRONLY, 0644)
		_, _ = f.WriteString("docs/ @tech-writers\n")
		_ = f.Close()

		secPath := filepath.Join(projectPath, "SECURITY.md")
		//nolint:gosec // Test project path
		_ = os.WriteFile(secPath, []byte("# Custom Security\nContact us directly."), 0644)
	})

	t.Run("3. Failed Update (Drift Detection)", func(t *testing.T) {
		_ = os.Chdir(projectPath)

		// Try to apply an update with default conflict strategy (fail). Should fail because SECURITY.md has drifted.
		output, err := runCLI("apply", "--with", "compliance", "--license", "Apache-2.0", "--copyright-holder", "Bob Corp")
		if err == nil {
			t.Fatalf("Expected apply to fail due to drift, but it succeeded.")
		}
		if !strings.Contains(output, "has manual modifications") {
			t.Errorf("Expected 'manual modifications' error, got: %s", output)
		}
	})

	t.Run("4. Update with Artifact Strategy", func(t *testing.T) {
		_ = os.Chdir(projectPath)

		// Apply update with --conflict-strategy=artifact
		output, err := runCLI("apply", "--with", "compliance", "--conflict-strategy=artifact", "--license", "Apache-2.0", "--copyright-holder", "Bob Corp")
		if err != nil {
			t.Fatalf("Failed to apply update: %v\nOutput: %s", err, output)
		}

		// Verify License changed to Apache and new holder (because it wasn't modified by the user)
		licenseBytes, _ := os.ReadFile("LICENSE")
		licenseText := string(licenseBytes)
		if !strings.Contains(licenseText, "Apache License") {
			t.Errorf("LICENSE was not updated to Apache")
		}
		if !strings.Contains(licenseText, "Bob Corp") {
			t.Errorf("LICENSE copyright holder was not updated")
		}

		// Verify SECURITY.md artifact was created because it HAD drifted
		if _, err := os.Stat("SECURITY.md.scbake-new"); os.IsNotExist(err) {
			t.Errorf("Artifact SECURITY.md.scbake-new was not created for drifted file")
		}

		// Verify original drifted SECURITY.md is untouched
		secBytes, _ := os.ReadFile("SECURITY.md")
		if !strings.Contains(string(secBytes), "Custom Security") {
			t.Errorf("Drifted SECURITY.md was overwritten despite artifact strategy")
		}

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

func TestGovernance_DryRunNoChanges(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	output, err := runCLI("new", "dryrun-proj", "--dry-run", "--with", "compliance,community", "--license", "MIT", "--copyright-holder", "Test Corp")
	if err != nil {
		t.Fatalf("Dry-run new failed: %v\nOutput: %s", err, output)
	}

	projectPath := filepath.Join(tmpDir, "dryrun-proj")
	if _, err := os.Stat(projectPath); !os.IsNotExist(err) {
		t.Error("Dry-run should not create the project directory")
	}
}

func TestGovernance_MissingLicenseFlag(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	output, err := runCLI("new", "no-license-proj", "--with", "compliance", "--copyright-holder", "Test Corp")
	if err == nil {
		t.Fatal("Expected error when --license is missing with compliance")
	}
	if !strings.Contains(output, "license") {
		t.Errorf("Error should mention license, got: %s", output)
	}
}

func TestGovernance_MissingCopyrightHolderFlag(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	output, err := runCLI("new", "no-holder-proj", "--with", "compliance", "--license", "MIT")
	if err == nil {
		t.Fatal("Expected error when --copyright-holder is missing with compliance")
	}
	if !strings.Contains(output, "copyright") {
		t.Errorf("Error should mention copyright, got: %s", output)
	}
}

func TestGovernance_UnsupportedSPDX(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	_, err := runCLI("new", "badspdx-proj", "--with", "compliance", "--license", "NOT-A-REAL-LICENSE", "--copyright-holder", "Test Corp")
	if err == nil {
		t.Fatal("Expected error for unsupported SPDX identifier")
	}
}
