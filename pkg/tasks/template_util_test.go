package tasks

import (
	"embed"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/templateregistry"
	"scbake/internal/testutil"
	"testing"
)

//go:embed testdata/simple.tpl
var testEmbedFS embed.FS

// pullIntoCache downloads a single-file template into registryCacheDir so the
// registry cache holds a manifest-verified per-template entry, matching how
// Pull/PullFromURL populate the cache. The flat/legacy layout is unverified and
// no longer resolvable, so tests must set up the real layout.
func pullIntoCache(t *testing.T, registryCacheDir, registryName, templateName, tplPath, content string) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(testutil.CreateTarGz(map[string]string{tplPath: content}))
	}))
	t.Cleanup(srv.Close)

	m := templateregistry.NewManagerWithPaths(filepath.Join(t.TempDir(), "registries.json"), registryCacheDir)
	if err := m.PullFromURL(registryName, templateName, srv.URL, ""); err != nil {
		t.Fatalf("PullFromURL failed: %v", err)
	}
}

func TestReadTemplate_FromEmbedded(t *testing.T) {
	t.Parallel()

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "", "", "")
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

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir, "", "")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if string(content) != overrideContent {
		t.Errorf("expected override content %q, got %q", overrideContent, string(content))
	}
}

func TestReadTemplate_OverrideDirEmpty(t *testing.T) {
	t.Parallel()

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "", "", "")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected content from embedded fallback")
	}
}

func TestReadTemplate_OverrideDirNonExistent(t *testing.T) {
	t.Parallel()

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "/nonexistent/path", "", "")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("expected content from embedded fallback")
	}
}

func TestReadTemplate_EmbeddedFileNotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadTemplate(testEmbedFS, "nonexistent.tpl", "", "", "")
	if err == nil {
		t.Fatal("expected error for nonexistent embedded file")
	}
}

func TestReadTemplate_OverrideWithPermissionError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce POSIX mode bits; a 0000 file is still readable")
	}

	tmpDir := t.TempDir()
	overridePath := filepath.Join(tmpDir, "testdata/simple.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	if err := os.WriteFile(overridePath, []byte("content"), 0000); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	_, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir, "", "")
	if err == nil {
		t.Fatal("expected error for permission denied on override file")
	}
}

func TestReadTemplate_OverridePathTraversal(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	maliciousPath := "../../etc/passwd"

	_, err := ReadTemplate(testEmbedFS, maliciousPath, tmpDir, "", "")
	if err == nil {
		t.Fatal("expected error for path traversal to nonexistent file")
	}
}

func TestReadTemplate_OverrideDanglingSymlinkFallsThrough(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	tmpDir := t.TempDir()
	// An intermediate component of the requested path is a dangling symlink.
	// SafePath/canonicalize fails closed on it with a wrapped os.ErrNotExist;
	// resolution must treat that as a fall-through to the embedded template
	// rather than a fatal error.
	if err := os.Symlink(filepath.Join(tmpDir, "missing"), filepath.Join(tmpDir, "testdata")); err != nil {
		t.Fatalf("Symlink failed: %v", err)
	}

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir, "", "")
	if err != nil {
		t.Fatalf("ReadTemplate should fall through a dangling symlink to the embedded template: %v", err)
	}
	expected := "Hello {{ (index .Projects 0).Name }} from {{ .SbakeVersion }}"
	if string(content) != expected {
		t.Errorf("expected embedded content %q, got %q", expected, string(content))
	}
}

func TestReadTemplate_OverrideEscapingSymlinkRejected(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}

	tmpDir := t.TempDir()
	outside := t.TempDir()
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(outside, "simple.tpl"), []byte("secret"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	// A symlink in the override dir that escapes it must stay a fatal error,
	// not a silent fall-through.
	if err := os.Symlink(outside, filepath.Join(tmpDir, "testdata")); err != nil {
		t.Fatalf("Symlink failed: %v", err)
	}

	if _, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir, "", ""); err == nil {
		t.Fatal("expected rejection of symlink escaping the override dir")
	}
}

func TestReadTemplate_OverrideDirWithTrailingSlash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	overrideContent := "trailing slash test"
	overridePath := filepath.Join(tmpDir, "testdata/simple.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Trailing slash should be cleaned/normalized
	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir+"/", "", "")
	if err != nil {
		t.Fatalf("ReadTemplate with trailing slash failed: %v", err)
	}
	if string(content) != overrideContent {
		t.Errorf("expected %q with trailing slash, got %q", overrideContent, string(content))
	}
}

func TestReadTemplate_OverrideSubdirectoryStructure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	overrideContent := "nested override"
	overridePath := filepath.Join(tmpDir, "nested/deep/template.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// Verify the override file is read from the subdirectory structure
	content, err := ReadTemplate(testEmbedFS, "nested/deep/template.tpl", tmpDir, "", "")
	if err != nil {
		t.Fatalf("ReadTemplate should read override from subdirectories: %v", err)
	}
	if string(content) != overrideContent {
		t.Errorf("expected %q, got %q", overrideContent, string(content))
	}
}

func TestReadTemplate_EmptyEmbeddedFSWithOverride(t *testing.T) {
	t.Parallel()

	// Use a minimal embed.FS with no templates
	var emptyFS embed.FS

	tmpDir := t.TempDir()
	overrideContent := "only from override"
	overridePath := filepath.Join(tmpDir, "custom.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	content, err := ReadTemplate(emptyFS, "custom.tpl", tmpDir, "", "")
	if err != nil {
		t.Fatalf("ReadTemplate with empty FS and override file should succeed: %v", err)
	}
	if string(content) != overrideContent {
		t.Errorf("expected %q, got %q", overrideContent, string(content))
	}
}

func TestReadTemplate_OverrideOnlyInDirNotFound(t *testing.T) {
	t.Parallel()

	var emptyFS embed.FS
	tmpDir := t.TempDir()

	_, err := ReadTemplate(emptyFS, "nonexistent.tpl", tmpDir, "", "")
	if err == nil {
		t.Fatal("expected error when file not in override dir or embedded FS")
	}
}

func TestReadTemplate_RegistryCacheFallback(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	registryCacheDir := filepath.Join(tmpDir, "registry-cache")
	cacheContent := "from registry cache"
	pullIntoCache(t, registryCacheDir, "acme", "go-api", "testdata/simple.tpl", cacheContent)

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "", registryCacheDir, "go-api")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if string(content) != cacheContent {
		t.Errorf("expected registry cache content %q, got %q", cacheContent, string(content))
	}
}

func TestReadTemplate_OverrideBeatsRegistryCache(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	overrideContent := "from override dir"
	overridePath := filepath.Join(tmpDir, "testdata/simple.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(overridePath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(overridePath, []byte(overrideContent), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	registryCacheDir := filepath.Join(tmpDir, "registry-cache")
	pullIntoCache(t, registryCacheDir, "acme", "go-api", "testdata/simple.tpl", "from registry")

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", tmpDir, registryCacheDir, "go-api")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	if string(content) != overrideContent {
		t.Errorf("expected override dir content %q, got %q", overrideContent, string(content))
	}
}

func TestReadTemplate_EmbeddedFallbackWhenRegistryCacheEmpty(t *testing.T) {
	t.Parallel()

	emptyCacheDir := t.TempDir()

	content, err := ReadTemplate(testEmbedFS, "testdata/simple.tpl", "", emptyCacheDir, "")
	if err != nil {
		t.Fatalf("ReadTemplate failed: %v", err)
	}
	expected := "Hello {{ (index .Projects 0).Name }} from {{ .SbakeVersion }}"
	if string(content) != expected {
		t.Errorf("expected embedded content %q, got %q", expected, string(content))
	}
}
