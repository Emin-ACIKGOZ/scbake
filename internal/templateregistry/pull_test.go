package templateregistry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"scbake/internal/testutil"
	"strings"
	"testing"
	"time"
)

const (
	testTplContent = "package main"
	testHandler    = "func handler() {}"
)

func TestExtractTarGz_Basic(t *testing.T) {
	destDir := t.TempDir()
	archive := testutil.CreateTarGz(map[string]string{
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
	archive := testutil.CreateTarGz(map[string]string{
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
			archive := testutil.CreateTarGz(map[string]string{
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
	archive := testutil.CreateTarGz(map[string]string{
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
	archive := testutil.CreateTarGz(map[string]string{
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
	archive := testutil.CreateTarGz(map[string]string{
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

// TestDoDownload_BodyReadableAfterReturn is a regression test for SCB-026:
// the request context must stay alive after doDownload returns so the caller
// can consume the response body. Previously the deferred cancel() canceled
// the request context as soon as doDownload returned, so the body read failed
// with "context canceled".
func TestDoDownload_BodyReadableAfterReturn(t *testing.T) {
	payload := []byte("some response body")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Ensure doDownload has returned (and, pre-fix, canceled the
		// context) before the body becomes available.
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	resp, err := doDownload(srv.URL, "", "tpl")
	if err != nil {
		t.Fatalf("doDownload failed: %v", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body after doDownload returned failed: %v", err)
	}
	if !bytes.Equal(body, payload) {
		t.Errorf("expected body %q, got %q", payload, body)
	}
}

// TestPullFromURL_ReadsBodyAfterHeaders is the end-to-end regression test for
// SCB-026: a pull must succeed even when the server sends headers before the
// body, so the response body is read after doDownload has returned.
func TestPullFromURL_ReadsBodyAfterHeaders(t *testing.T) {
	archive := testutil.CreateTarGz(map[string]string{
		"main.go.tpl": testTplContent,
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("PullFromURL failed: %v", err)
	}

	cacheDir := TemplateCachePath(m.cacheDir, "acme", "go-api")
	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(cacheDir, "main.go.tpl"))
	if err != nil {
		t.Fatalf("failed to read pulled template: %v", err)
	}
	if string(content) != testTplContent {
		t.Errorf("expected %q, got %q", testTplContent, string(content))
	}
}

// TestDoDownload_ErrorRedactsCredentials is a regression test for SCB-034:
// failures must not echo credentials embedded in the registry URL.
func TestDoDownload_ErrorRedactsCredentials(t *testing.T) {
	// Port 1 is not listening, so the request fails while dialing.
	resp, err := doDownload("https://user:supersecret@127.0.0.1:1/x.tar.gz", "", "tpl")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected doDownload to fail against an unreachable host")
	}

	for _, secret := range []string{"supersecret", "user:", "user@"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks credentials (%q): %v", secret, err)
		}
	}
	if !strings.Contains(err.Error(), "127.0.0.1:1/x.tar.gz") {
		t.Errorf("error should still include the useful host/path: %v", err)
	}
}

// TestDoDownload_ErrorRedactsControlCharCredentials guards against a credential
// leak through a wrapped parse error: url.Error renders its URL with %q, so a
// literal substring replacement misses control characters that the quoting
// escapes. The redaction must operate on the parsed URL, not the rendered text.
func TestDoDownload_ErrorRedactsControlCharCredentials(t *testing.T) {
	resp, err := doDownload("https://user:pass@host/x\ty", "", "tpl")
	if resp != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		t.Fatal("expected doDownload to reject a URL with a control character")
	}

	for _, secret := range []string{"user:pass", ":pass", "pass@"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaks credentials (%q): %v", secret, err)
		}
	}
}

func TestPullFromURL_CachedSkipRedownload(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"f.tpl": "content"}))
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

// TestPullFromURL_RepullsPoisonedCacheWithoutManifest is a regression test for
// SCB-025: a pre-existing cache directory with no manifest is not trusted, so
// the pull re-downloads the template and records a manifest.
func TestPullFromURL_RepullsPoisonedCacheWithoutManifest(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"main.go.tpl": "trusted content"}))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	// Plant attacker content with no manifest, as if the cache were poisoned.
	destDir := TemplateCachePath(m.cacheDir, "acme", "go-api")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(destDir, 0750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(destDir, "main.go.tpl"), []byte("attacker"), 0640); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("PullFromURL failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected poisoned cache to trigger a re-download, got %d server calls", callCount)
	}
	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(destDir, "main.go.tpl"))
	if err != nil {
		t.Fatalf("reading re-downloaded template failed: %v", err)
	}
	if string(content) != "trusted content" {
		t.Errorf("expected re-downloaded content %q, got %q", "trusted content", string(content))
	}
	if _, err := os.Stat(TemplateManifestPath(m.cacheDir, "acme", "go-api")); err != nil {
		t.Errorf("expected a cache manifest to be written: %v", err)
	}
}

// TestPullFromURL_RepullsTamperedCache verifies that editing a cached file
// after a successful pull invalidates the cache and triggers a re-download.
func TestPullFromURL_RepullsTamperedCache(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"main.go.tpl": "trusted content"}))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("first PullFromURL failed: %v", err)
	}

	destDir := TemplateCachePath(m.cacheDir, "acme", "go-api")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(destDir, "main.go.tpl"), []byte("attacker"), 0640); err != nil {
		t.Fatalf("tampering failed: %v", err)
	}

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("second PullFromURL failed: %v", err)
	}

	if callCount != 2 {
		t.Fatalf("expected tampered cache to trigger a re-download, got %d server calls", callCount)
	}
	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(destDir, "main.go.tpl"))
	if err != nil {
		t.Fatalf("reading restored template failed: %v", err)
	}
	if string(content) != "trusted content" {
		t.Errorf("expected restored content %q, got %q", "trusted content", string(content))
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

func TestExtractTarGz_DecompressionBombLimiter(t *testing.T) {
	destDir := t.TempDir()

	// Construct an oversized payload (> maxArchiveBytes)
	oversizedBytes := int64(maxArchiveBytes) + 1024
	payload := bytes.Repeat([]byte("A"), int(oversizedBytes))

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{
		Name: "large_file.txt",
		Size: oversizedBytes,
		Mode: 0644,
	}
	_ = tw.WriteHeader(hdr)
	_, _ = tw.Write(payload)
	_ = tw.Close()
	_ = gz.Close()

	// Extract archive
	_ = extractTarGz(bytes.NewReader(buf.Bytes()), destDir)

	// Verify extracted file size on disk is capped at maxArchiveBytes
	extractedPath := filepath.Join(destDir, "large_file.txt")
	info, err := os.Stat(extractedPath)
	if err != nil {
		t.Fatalf("failed to stat extracted file: %v", err)
	}

	if info.Size() > int64(maxArchiveBytes) {
		t.Errorf("decompression limiter failed: extracted size %d exceeds maxArchiveBytes %d", info.Size(), maxArchiveBytes)
	}
}
