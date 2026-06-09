package templateregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func createTarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		}
		_ = tw.WriteHeader(hdr)
		_, _ = tw.Write([]byte(content))
	}

	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestExtractTarGz_Basic(t *testing.T) {
	destDir := t.TempDir()
	archive := createTarGz(map[string]string{
		"templates/main.go.tpl": "package main",
		"templates/handler.tpl": "func handler() {}",
	})

	if err := extractTarGz(bytes.NewReader(archive), destDir); err != nil {
		t.Fatalf("extractTarGz failed: %v", err)
	}

	//nolint:gosec // test temp directory, safe read
	content, err := os.ReadFile(filepath.Join(destDir, "templates/main.go.tpl"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "package main" {
		t.Errorf("expected 'package main', got %q", string(content))
	}

	//nolint:gosec // test temp directory, safe read
	content, err = os.ReadFile(filepath.Join(destDir, "templates/handler.tpl"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "func handler() {}" {
		t.Errorf("expected 'func handler() {}', got %q", string(content))
	}
}

func TestExtractTarGz_DirectoryStructure(t *testing.T) {
	destDir := t.TempDir()
	archive := createTarGz(map[string]string{
		"templates/nested/deep/file.tpl": "deep content",
	})

	if err := extractTarGz(bytes.NewReader(archive), destDir); err != nil {
		t.Fatalf("extractTarGz failed: %v", err)
	}

	//nolint:gosec // test temp directory, safe read
	content, err := os.ReadFile(filepath.Join(destDir, "templates/nested/deep/file.tpl"))
	if err != nil {
		t.Fatalf("failed to read nested file: %v", err)
	}
	if string(content) != "deep content" {
		t.Errorf("expected 'deep content', got %q", string(content))
	}
}

func TestExtractTarGz_PathTraversal(t *testing.T) {
	destDir := t.TempDir()

	//nolint:gosec // test temp directory
	_ = os.WriteFile(filepath.Join(destDir, "marker"), []byte("marker"), 0644)

	tests := []struct {
		name     string
		filePath string
	}{
		{"parent directory", "../escape.tpl"},
		{"absolute path", "/tmp/escape.tpl"},
		{"encoded traversal", "foo/../../etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archive := createTarGz(map[string]string{
				tt.filePath: "evil",
			})

			err := extractTarGz(bytes.NewReader(archive), destDir)
			if err == nil {
				t.Error("expected error for path traversal, got nil")
			}
		})
	}
}
