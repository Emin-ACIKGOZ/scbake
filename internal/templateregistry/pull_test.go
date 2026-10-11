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
	"sync"
	"testing"
	"time"
)

const (
	testTplContent = "package main"
	testHandler    = "func handler() {}"
	testTrusted    = "trusted content"
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
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"main.go.tpl": testTrusted}))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	// Plant attacker content with no manifest, as if the cache were poisoned.
	destDir := TemplateCachePath(m.cacheDir, "acme", "go-api")
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
	if string(content) != testTrusted {
		t.Errorf("expected re-downloaded content %q, got %q", testTrusted, string(content))
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
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"main.go.tpl": testTrusted}))
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
	if string(content) != testTrusted {
		t.Errorf("expected restored content %q, got %q", testTrusted, string(content))
	}
}

// TestCacheIsValid_RejectsEmptyTree is a regression test for the empty-tree
// cache hit: an empty template directory whose manifest records the empty-tree
// hash must not be treated as valid, which would suppress a re-pull and let an
// empty tree masquerade as a cache hit.
func TestCacheIsValid_RejectsEmptyTree(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, "acme", "go-api")
	if err := os.MkdirAll(destDir, 0750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	hash, err := hashTree(destDir)
	if err != nil {
		t.Fatalf("hashTree failed: %v", err)
	}
	manifestPath := TemplateManifestPath(dir, "acme", "go-api")
	if err := writeCacheManifest(manifestPath, "go-api", hash); err != nil {
		t.Fatalf("writeCacheManifest failed: %v", err)
	}

	if cacheIsValid(destDir, manifestPath) {
		t.Error("expected empty template tree to be rejected as an invalid cache")
	}
}

// TestPullFromURL_RepullsEmptyTreeCache verifies end to end that a cache
// directory with no regular files is re-pulled even when its manifest records
// the empty-tree hash, so an empty tree cannot suppress a download.
func TestPullFromURL_RepullsEmptyTreeCache(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{"main.go.tpl": testTrusted}))
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	destDir := TemplateCachePath(m.cacheDir, "acme", "go-api")
	if err := os.MkdirAll(destDir, 0750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	hash, err := hashTree(destDir)
	if err != nil {
		t.Fatalf("hashTree failed: %v", err)
	}
	if err := writeCacheManifest(TemplateManifestPath(m.cacheDir, "acme", "go-api"), "go-api", hash); err != nil {
		t.Fatalf("writeCacheManifest failed: %v", err)
	}

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err != nil {
		t.Fatalf("PullFromURL failed: %v", err)
	}

	if callCount != 1 {
		t.Fatalf("expected empty-tree cache to trigger a re-download, got %d server calls", callCount)
	}
	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(destDir, "main.go.tpl"))
	if err != nil {
		t.Fatalf("reading re-downloaded template failed: %v", err)
	}
	if string(content) != testTrusted {
		t.Errorf("expected re-downloaded content %q, got %q", testTrusted, string(content))
	}
}

// TestPullFromURL_FailedRepullKeepsExistingCache is a regression test for F6:
// a failed re-pull (the server returns 5xx) must not destroy an existing cache
// directory. Before the fix pullFromURL removed the cache and manifest up front,
// so a transient failure left the user with no cached template.
func TestPullFromURL_FailedRepullKeepsExistingCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	// Plant an existing (unmanifested, so invalid) cache that the pull will try
	// to refresh.
	destDir := TemplateCachePath(m.cacheDir, "acme", "go-api")
	if err := os.MkdirAll(destDir, 0750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	oldContent := []byte("previously cached content")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(destDir, "main.go.tpl"), oldContent, 0640); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err == nil {
		t.Fatal("expected PullFromURL to fail against a 5xx server")
	}

	//nolint:gosec // test temp directory
	content, err := os.ReadFile(filepath.Join(destDir, "main.go.tpl"))
	if err != nil {
		t.Fatalf("failed re-pull destroyed the existing cache: %v", err)
	}
	if !bytes.Equal(content, oldContent) {
		t.Errorf("expected existing cache content %q, got %q", oldContent, content)
	}
}

// TestHashTree_FailsOnSymlink is a regression test for F7: hashTree must fail
// closed when it encounters a non-regular entry such as a symlink. SafeReadFile
// follows symlinks, so silently skipping one would let an attacker add a symlink
// after the manifest was written without changing the verified hash.
func TestHashTree_FailsOnSymlink(t *testing.T) {
	dir := t.TempDir()
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(dir, "real.tpl"), []byte("data"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.Symlink("real.tpl", filepath.Join(dir, "link.tpl")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	if _, err := hashTree(dir); err == nil {
		t.Fatal("expected hashTree to fail on a symlink entry, got nil")
	}
}

// TestHashTree_LengthFraming is a regression test for F8: without length
// framing, distinct trees can collide ({x:"X", y:"Y"} and {x:"Xy\x00Y"} both
// encode to "x\x00Xy\x00Y"). Length-prefixing each component makes the encoding
// unambiguous, so the two hashes must now differ.
func TestHashTree_LengthFraming(t *testing.T) {
	dirA := t.TempDir()
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(dirA, "x"), []byte("X"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(dirA, "y"), []byte("Y"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	dirB := t.TempDir()
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(dirB, "x"), []byte("Xy\x00Y"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	hashA, err := hashTree(dirA)
	if err != nil {
		t.Fatalf("hashTree(dirA) failed: %v", err)
	}
	hashB, err := hashTree(dirB)
	if err != nil {
		t.Fatalf("hashTree(dirB) failed: %v", err)
	}

	if hashA == hashB {
		t.Fatalf("ambiguous trees hashed identically (%s); length framing is missing", hashA)
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

// TestPull_RejectsTraversalTemplateName is a regression test for SCB-010: a
// template name containing ".." must be rejected before any download, and must
// never cause os.RemoveAll(destDir+".tmp") to delete a directory outside the
// cache.
func TestPull_RejectsTraversalTemplateName(t *testing.T) {
	archive := testutil.CreateTarGz(map[string]string{"main.go.tpl": testTplContent})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), cacheDir)
	if err := m.Add("acme", srv.URL, "", "", ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	// cacheDir/acme/../../evil resolves to <dir>/evil, so destDir+".tmp" is
	// <dir>/evil.tmp. Plant it to prove it is not recursively deleted.
	sentinel := filepath.Join(dir, "evil.tmp")
	if err := os.MkdirAll(sentinel, 0750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	marker := filepath.Join(sentinel, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := m.Pull("acme", "../../evil"); err == nil {
		t.Fatal("expected Pull to reject a traversal template name")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("traversal pull deleted a directory outside the cache: %v", err)
	}
}

// TestPull_RejectsTraversalRegistryName is a regression test for SCB-010: a
// registry name containing a path separator or ".." must be rejected both when
// added and when used to pull.
func TestPull_RejectsTraversalRegistryName(t *testing.T) {
	archive := testutil.CreateTarGz(map[string]string{"main.go.tpl": testTplContent})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), cacheDir)

	if err := m.Add("../evil", srv.URL, "", "", ""); err == nil {
		t.Error("expected Add to reject a traversal registry name")
	}
	if err := m.PullFromURL("../evil", "go-api", srv.URL, ""); err == nil {
		t.Error("expected PullFromURL to reject a traversal registry name")
	}
}

// TestPull_RejectsTraversalSubdirectory is a regression test for SCB-011: a
// subdirectory of ".." must not make promoteSubdirectory return a directory
// outside the extraction temp dir. The config is seeded directly so the
// malicious value bypasses the Add-time check, modeling a hand-edited config.
func TestPull_RejectsTraversalSubdirectory(t *testing.T) {
	archive := testutil.CreateTarGz(map[string]string{"main.go.tpl": testTplContent})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), cacheDir)
	m.config.Registries = []Registry{{Name: "acme", URL: srv.URL, Subdirectory: "../.."}}

	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	marker := filepath.Join(cacheDir, "marker")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if err := m.Pull("acme", "go-api"); err == nil {
		t.Fatal("expected Pull to reject a traversal subdirectory")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("traversal subdirectory pull damaged the cache root: %v", err)
	}
}

// TestPromoteSubdirectory_RejectsTraversal verifies promoteSubdirectory never
// returns a path outside the temp dir, regardless of the subdirectory value.
func TestPromoteSubdirectory_RejectsTraversal(t *testing.T) {
	tmpDir := filepath.Join(t.TempDir(), "stage")
	if err := os.MkdirAll(tmpDir, 0750); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	for _, sub := range []string{"..", "../..", "nested/../../..", `..\..`} {
		got := promoteSubdirectory(tmpDir, sub)
		rel, err := filepath.Rel(tmpDir, got)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Errorf("promoteSubdirectory(%q, %q) = %q escapes the temp dir", tmpDir, sub, got)
		}
	}
}

// TestExtractToCache_RejectsTraversalDestDir verifies the staging path is
// contained in the cache before any destructive operation.
func TestExtractToCache_RejectsTraversalDestDir(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	archive := testutil.CreateTarGz(map[string]string{"main.go.tpl": testTplContent})

	// destDir escapes the cache root.
	outside := filepath.Join(dir, "outside")
	if err := extractToCache(bytes.NewReader(archive), cacheDir, outside, ""); err == nil {
		t.Fatal("expected extractToCache to reject a destDir outside the cache")
	}
}

// TestPullFromURL_RejectsSymlinkedRegistryDir is a regression test for the
// symlink bypass of the lexical containment checks: when <cache>/<registry> is
// a symlink pointing outside the cache, the destination resolves through the
// symlink, so a pull must fail closed instead of writing (and later removing)
// the template outside the cache root.
func TestPullFromURL_RejectsSymlinkedRegistryDir(t *testing.T) {
	archive := testutil.CreateTarGz(map[string]string{"main.go.tpl": testTplContent})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(cacheDir, 0750); err != nil {
		t.Fatalf("MkdirAll(cacheDir) failed: %v", err)
	}
	if err := os.MkdirAll(outside, 0750); err != nil {
		t.Fatalf("MkdirAll(outside) failed: %v", err)
	}
	// <cache>/acme -> <dir>/outside, so cacheDir/acme/go-api is outside.
	if err := os.Symlink(outside, filepath.Join(cacheDir, "acme")); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), cacheDir)

	if err := m.PullFromURL("acme", "go-api", srv.URL, ""); err == nil {
		t.Error("expected PullFromURL to reject a symlinked registry directory")
	}

	// Nothing may have been written or deleted through the symlink outside the
	// cache root.
	for _, name := range []string{"go-api", "go-api.tmp", "go-api.manifest.json"} {
		if _, statErr := os.Lstat(filepath.Join(outside, name)); !os.IsNotExist(statErr) {
			t.Errorf("pull touched %q outside the cache through a symlink (stat err: %v)", name, statErr)
		}
	}
}

// swapReader serves an archive but invokes swap exactly once, on the first Read.
// It lets a test swap <cache>/<registry> for an escaping symlink at the moment
// the download body starts being consumed, i.e. after extractToCache's initial
// containment checks but before its destructive operations.
type swapReader struct {
	data []byte
	once sync.Once
	swap func()
}

func (r *swapReader) Read(p []byte) (int, error) {
	r.once.Do(r.swap)
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

// TestExtractToCache_RechecksContainmentBeforeSwap is a deterministic regression
// test for the TOCTOU in extractToCache: containment was checked only once, at
// the top, but the download body is fully consumed (and extracted) before the
// destructive RemoveAll(destDir)/Rename(sourceDir, destDir) run. An attacker who
// replaces <cache>/<registry> with a symlink pointing outside the cache while
// the body is pending would make those operations follow the symlink, deleting
// and overwriting data outside the cache, while the pull reported success.
//
// The test swaps the registry directory for an escaping symlink on the first
// body Read and asserts extractToCache fails closed and leaves the outside
// sentinel intact.
func TestExtractToCache_RechecksContainmentBeforeSwap(t *testing.T) { //nolint:cyclop // deterministic TOCTOU regression test
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	registryDir := filepath.Join(cacheDir, "acme")
	destDir := filepath.Join(registryDir, "go-api")
	outside := filepath.Join(dir, "outside")

	if err := os.MkdirAll(registryDir, 0750); err != nil {
		t.Fatalf("MkdirAll(registryDir) failed: %v", err)
	}
	if err := os.MkdirAll(outside, 0750); err != nil {
		t.Fatalf("MkdirAll(outside) failed: %v", err)
	}
	// The attacker pre-creates the staging dir at the symlink target so
	// extraction still succeeds after the swap.
	if err := os.MkdirAll(filepath.Join(outside, "go-api.tmp"), 0750); err != nil {
		t.Fatalf("MkdirAll(outside tmp) failed: %v", err)
	}
	// A sentinel at the destination the final RemoveAll/Rename would clobber if
	// it followed the symlink.
	outsideDest := filepath.Join(outside, "go-api")
	if err := os.MkdirAll(outsideDest, 0750); err != nil {
		t.Fatalf("MkdirAll(outsideDest) failed: %v", err)
	}
	marker := filepath.Join(outsideDest, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatalf("WriteFile(marker) failed: %v", err)
	}

	archive := testutil.CreateTarGz(map[string]string{"main.go.tpl": testTplContent})

	swapped := false
	body := &swapReader{
		data: archive,
		swap: func() {
			// Replace <cache>/acme with a symlink to <dir>/outside, exactly as a
			// concurrent attacker would during the body read.
			if err := os.Rename(registryDir, filepath.Join(cacheDir, "acme.real")); err != nil {
				t.Errorf("renaming registry dir aside failed: %v", err)
				return
			}
			if err := os.Symlink(outside, registryDir); err != nil {
				t.Errorf("creating escaping symlink failed: %v", err)
				return
			}
			swapped = true
		},
	}

	if err := extractToCache(body, cacheDir, destDir, ""); err == nil {
		t.Fatal("expected extractToCache to fail when the registry dir is swapped for an escaping symlink")
	}
	if !swapped {
		t.Fatal("test did not trigger the swap; the body was never read")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("extractToCache deleted content outside the cache through a symlink: %v", err)
	}
}

// TestRemoveAllContained_RechecksSymlink is a focused unit test for the pre-op
// re-check: after a directory is swapped for an escaping symlink,
// removeAllContained must refuse to act rather than delete through the symlink.
func TestRemoveAllContained_RechecksSymlink(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	registryDir := filepath.Join(cacheDir, "acme")
	templateDir := filepath.Join(registryDir, "go-api")
	outside := filepath.Join(dir, "outside")

	if err := os.MkdirAll(templateDir, 0750); err != nil {
		t.Fatalf("MkdirAll(templateDir) failed: %v", err)
	}
	// A sentinel at the path a symlink-following RemoveAll would delete.
	sentinel := filepath.Join(outside, "go-api")
	if err := os.MkdirAll(sentinel, 0750); err != nil {
		t.Fatalf("MkdirAll(sentinel) failed: %v", err)
	}
	marker := filepath.Join(sentinel, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatalf("WriteFile(marker) failed: %v", err)
	}

	// Swap <cache>/acme for a symlink to <dir>/outside.
	if err := os.RemoveAll(registryDir); err != nil {
		t.Fatalf("RemoveAll(registryDir) failed: %v", err)
	}
	if err := os.Symlink(outside, registryDir); err != nil {
		t.Skipf("symlinks unsupported on this platform: %v", err)
	}

	if err := removeAllContained(cacheDir, templateDir); err == nil {
		t.Error("expected removeAllContained to refuse an escaping symlink")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("removeAllContained deleted through an escaping symlink: %v", err)
	}
}
