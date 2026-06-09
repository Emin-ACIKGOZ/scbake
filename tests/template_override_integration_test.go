package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTemplateOverrides(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	overrideDir := filepath.Join(tmpDir, "my-overrides")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(overrideDir, 0755); err != nil {
		t.Fatalf("Failed to create override dir: %v", err)
	}

	overrides := map[string]string{
		"templates/CONTRIBUTING.md.tpl":    "# OVERRIDDEN CONTRIBUTING\n",
		"templates/CODE_OF_CONDUCT.md.tpl": "# OVERRIDDEN CODE OF CONDUCT\n",
		"templates/SUPPORT.md.tpl":         "# OVERRIDDEN SUPPORT\n",
		"templates/GOVERNANCE.md.tpl":      "# OVERRIDDEN GOVERNANCE\n",
		"main.yml.tpl":                     "name: OVERRIDDEN CI\n",
		".editorconfig.tpl":                "root = true\n[*.go]\nindent_style = OVERRIDDEN\n",
	}

	for relPath, content := range overrides {
		fullPath := filepath.Join(overrideDir, relPath)
		//nolint:gosec // test temp directory
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("MkdirAll for %s failed: %v", relPath, err)
		}
		//nolint:gosec // test temp directory
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile for %s failed: %v", relPath, err)
		}
	}

	projectPath := filepath.Join(tmpDir, "test-override")

	output, err := runCLI("new", "test-override", "--with", "community,ci_github,editorconfig", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("Failed to create project with overrides: %v\nOutput: %s", err, output)
	}

	checks := []struct {
		path   string
		marker string
		label  string
	}{
		{filepath.Join(projectPath, "CONTRIBUTING.md"), "OVERRIDDEN CONTRIBUTING", "CONTRIBUTING.md"},
		{filepath.Join(projectPath, "CODE_OF_CONDUCT.md"), "OVERRIDDEN CODE OF CONDUCT", "CODE_OF_CONDUCT.md"},
		{filepath.Join(projectPath, "SUPPORT.md"), "OVERRIDDEN SUPPORT", "SUPPORT.md"},
		{filepath.Join(projectPath, "GOVERNANCE.md"), "OVERRIDDEN GOVERNANCE", "GOVERNANCE.md"},
		{filepath.Join(projectPath, ".github", "workflows", "main.yml"), "OVERRIDDEN CI", "main.yml"},
		{filepath.Join(projectPath, ".editorconfig"), "OVERRIDDEN", ".editorconfig"},
	}

	for _, c := range checks {
		content, err := os.ReadFile(c.path)
		if err != nil {
			t.Fatalf("%s was not created: %v", c.label, err)
		}
		if !strings.Contains(string(content), c.marker) {
			t.Errorf("%s was not overridden. Got:\n%s", c.label, string(content))
		}
	}
}

func TestTemplateOverrides_Makefile(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	overrideDir := filepath.Join(tmpDir, "my-overrides")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(overrideDir, 0755); err != nil {
		t.Fatalf("Failed to create override dir: %v", err)
	}

	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(overrideDir, "makefile.tpl"), []byte("OVERRIDDEN MAKEFILE:\nall:\n\techo overridden\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	projectPath := filepath.Join(tmpDir, "test-makefile")
	output, err := runCLI("new", "test-makefile", "--with", "makefile", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("Failed to create project: %v\nOutput: %s", err, output)
	}

	//nolint:gosec // reading test-created file from project dir
	makefileContent, err := os.ReadFile(filepath.Join(projectPath, "Makefile"))
	if err != nil {
		t.Fatalf("Makefile not created: %v", err)
	}
	if !strings.Contains(string(makefileContent), "OVERRIDDEN MAKEFILE") {
		t.Errorf("Makefile was not overridden. Got:\n%s", string(makefileContent))
	}
}

func TestTemplateOverrides_FallbackToEmbedded(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	overrideDir := filepath.Join(tmpDir, "empty-overrides")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(overrideDir, 0755); err != nil {
		t.Fatalf("Failed to create override dir: %v", err)
	}

	projectPath := filepath.Join(tmpDir, "test-fallback")
	output, err := runCLI("new", "test-fallback", "--with", "editorconfig", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("Failed to create project: %v\nOutput: %s", err, output)
	}

	if _, err := os.Stat(filepath.Join(projectPath, ".editorconfig")); os.IsNotExist(err) {
		t.Error(".editorconfig not created from embedded fallback")
	}
}

func TestTemplateOverrides_WithEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	overrideDir := filepath.Join(tmpDir, "env-overrides")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(overrideDir, 0755); err != nil {
		t.Fatalf("Failed to create override dir: %v", err)
	}

	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(overrideDir, ".editorconfig.tpl"), []byte("root = true\n# ENV OVERRIDE\n"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	projectPath := filepath.Join(tmpDir, "test-env")

	t.Setenv("SCBAKE_TEMPLATE_DIR", overrideDir)

	output, err := runCLI("new", "test-env", "--with", "editorconfig")
	if err != nil {
		t.Fatalf("Failed to create project: %v\nOutput: %s", err, output)
	}

	//nolint:gosec // reading test-created file from project dir
	content, err := os.ReadFile(filepath.Join(projectPath, ".editorconfig"))
	if err != nil {
		t.Fatalf(".editorconfig not created: %v", err)
	}
	if !strings.Contains(string(content), "ENV OVERRIDE") {
		t.Errorf("Env var override not applied. Got:\n%s", string(content))
	}
}
