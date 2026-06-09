package tasks

import (
	"embed"
	"os"
	"path/filepath"
	"testing"
)

//go:embed testdata/simple.tpl
var testEmbedFS embed.FS

func TestReadTemplate_FromEmbedded(t *testing.T) {
	t.Parallel()

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected non-empty content from embedded")
	}
}

func TestReadTemplate_OverrideTakesPrecedence(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	overrideContent := "overridden template content"
	overridePath := filepath.Join(tmpDir, "testdata/simple.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir)
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if string(content) != overrideContent {
		t.Errorf("expected override content %q, got %q", overrideContent, string(content))
	}
}

func TestReadTemplate_OverrideDirEmpty(t *testing.T) {
	t.Parallel()

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected content from embedded fallback")
	}
}

func TestReadTemplate_OverrideDirNonExistent(t *testing.T) {
	t.Parallel()

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "/nonexistent/path")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected content from embedded fallback")
	}
}

func TestReadTemplate_EmbeddedFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadTemplate(testEmbedFS, "nonexistent.tpl", "")
	if err == nil {
		t.Fatal("expected error for nonexistent embedded file")
	}
}

func TestReadTemplate_OverrideWithPermissionError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "testdata/simple.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte("content"), 0000); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir)
	if err == nil {
		t.Fatal("expected error for permission denied on override file")
	}
}

func TestReadTemplate_OverridePathTraversal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	maliciousPath := "../../etc/passwd"

	_, err := ReadTemplate(testEmbedFS, maliciousPath, tmpDir)
	if err == nil {
		t.Fatal("expected error for path traversal to nonexistent file")
	}
}
