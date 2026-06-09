package templateregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const (
	testTplContent = "package main"
	testHandler    = "func handler() {}"
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
		"templates/main.go.tpl": testTplContent,
		"templates/handler.tpl": testHandler,
	})

	if err := extractTarGz(bytes.NewReader(archive), destDir); err != nil {
		t.Fatalf("extractTarGz failed: %v", err)
	}

	//nolint:gosec // test temp directory, safe read
	content, err := os.ReadFile(filepath.Join(destDir, "templates/main.go.tpl"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != testTplContent {
		t.Errorf("expected %q, got %q", testTplContent, string(content))
	}

	//nolint:gosec // test temp directory, safe read
	content, err = os.ReadFile(filepath.Join(destDir, "templates/handler.tpl"))
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != testHandler {
		t.Errorf("expected %q, got %q", testHandler, string(content))
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

func TestPullFromURL_Subdirectory(t *testing.T) {
	archive := createTarGz(map[string]string{
		"template/main.go.tpl":  testTplContent,
		"template/handler.go":   "func handler() {}",
		".github/workflows.yml": "ci config",
		"README.md":             "docs",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	// Pull with subdirectory "template"
	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("PullFromURL failed: %v", err)
	}

	cacheDir := TemplateCachePath(m.cacheDir, "acme", "go-api")

	// Subdirectory should be promoted if it had a subdirectory config
	// but since PullFromURL doesn't have subdirectory param, files should be as-is
	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(cacheDir, "template/main.go.tpl"))
	if err != nil {
		t.Fatalf("failed to read template/main.go.tpl: %v", err)
	}
	if string(content) != testTplContent {
		t.Errorf("expected %q, got %q", testTplContent, string(content))
	}
}

func TestPullFromURL_SubdirectoryPromotion(t *testing.T) {
	// Archive with content in a "template/" subdirectory
	archive := createTarGz(map[string]string{
		"template/main.go.tpl":  testTplContent,
		"template/handler.go":   "func handler() {}",
		".github/workflows.yml": "ci config",
		"README.md":             "docs",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	// Add registry with subdirectory "template"
	if err := m.Add("acme", srv.URL, "", "", "template"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// Pull uses the registry's subdirectory
	if err := m.Pull("acme", "go-api"); err != nil {
		t.Fatalf("Pull failed: %v", err)
	}

	cacheDir := TemplateCachePath(m.cacheDir, "acme", "go-api")

	// Files from the subdirectory should be promoted to root
	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(cacheDir, "main.go.tpl"))
	if err != nil {
		t.Fatalf("subdirectory file not promoted: %v", err)
	}
	if string(content) != testTplContent {
		t.Errorf("expected %q, got %q", testTplContent, string(content))
	}

	//nolint:gosec // test temp directory
	_, err = os.ReadFile(filepath.Join(cacheDir, "handler.go"))
	if err != nil {
		t.Fatalf("subdirectory file handler.go not promoted: %v", err)
	}

	// Files outside the subdirectory should NOT be in cache
	_, err = os.Stat(filepath.Join(cacheDir, "README.md"))
	if !os.IsNotExist(err) {
		t.Error("README.md outside subdirectory should not exist in cache")
	}
}

func TestPullFromURL_WithAuth(t *testing.T) {
	archive := createTarGz(map[string]string{
		"main.tpl": "authed content",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test_token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.PullFromURL("acme", "go-api", srv.URL, "test_token"); err != nil {
		t.Fatalf("PullFromURL with auth failed: %v", err)
	}

	cacheDir := TemplateCachePath(m.cacheDir, "acme", "go-api")
	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(cacheDir, "main.tpl"))
	if err != nil {
		t.Fatalf("failed to read main.tpl: %v", err)
	}
	if string(content) != "authed content" {
		t.Errorf("expected 'authed content', got %q", string(content))
	}
}

func TestPullFromURL_CachedSkipRedownload(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_, _ = w.Write(createTarGz(map[string]string{"f.tpl": "content"}))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("first PullFromURL failed: %v", err)
	}

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("second PullFromURL failed: %v", err)
	}

	if callCount != 1 {
		t.Errorf("expected 1 server call (cached), got %d", callCount)
	}
}

func TestValidateTemplateCache_Valid(t *testing.T) {
	dir := t.TempDir()
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(dir, "file.tpl"), []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := validateTemplateCache(dir); err != nil {
		t.Errorf("expected no error for valid cache, got %v", err)
	}
}

func TestValidateTemplateCache_WrappedDir(t *testing.T) {
	dir := t.TempDir()
	inner := filepath.Join(dir, "templates")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(inner, "file.tpl"), []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := validateTemplateCache(dir); err != nil {
		t.Errorf("expected no error for wrapped dir, got %v", err)
	}
}

func TestValidateTemplateCache_Empty(t *testing.T) {
	dir := t.TempDir()

	if err := validateTemplateCache(dir); err == nil {
		t.Error("expected error for empty cache directory")
	}
}
