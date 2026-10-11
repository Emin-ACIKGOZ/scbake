package integration

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"scbake/internal/testutil"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// userCacheDir returns the cache root the CLI resolves via os.UserCacheDir().
// That API honors XDG_CACHE_HOME on Linux but uses the native
// ~/Library/Caches location on macOS, so deriving the expectation the same way
// keeps this integration test portable across platforms.
func userCacheDir(t *testing.T) string {
	t.Helper()
	dir, err := os.UserCacheDir()
	if err != nil {
		t.Fatalf("cannot determine user cache dir: %v", err)
	}
	return dir
}

// isolateUserDirs points HOME and the platform config/cache environment at a
// temp dir so os.UserConfigDir/os.UserCacheDir resolve there on every platform
// (Linux honors XDG_*, macOS uses $HOME/Library/..., Windows uses %AppData%).
// Without this, macOS and Windows tests share the real user config and leak
// registry state between tests.
func isolateUserDirs(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	t.Setenv("AppData", filepath.Join(dir, "AppData"))
	t.Setenv("LocalAppData", filepath.Join(dir, "LocalAppData"))
	return dir
}

func TestRegistryPullEndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(tmpDir)

	archive := testutil.CreateTarGz(map[string]string{
		".editorconfig.tpl": "root = true\n[*]\nindent_style = registry_override\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			_, _ = w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	isolateUserDirs(t)

	out, err := runCLI("template", "registry", "add", "testreg", srv.URL)
	if err != nil {
		t.Fatalf("add registry: %v\n%s", err, out)
	}

	out, err = runCLI("template", "pull", "editorconfig", "--registry", "testreg")
	if err != nil {
		t.Fatalf("pull template: %v\n%s", err, out)
	}

	cacheDir := filepath.Join(userCacheDir(t), "scbake", "templates", "testreg", "editorconfig")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Fatalf("cache dir not created: %s", cacheDir)
	}

	projectPath := filepath.Join(tmpDir, "registry-test-project")
	out, err = runCLI("new", "registry-test-project", "--with", "editorconfig")
	if err != nil {
		t.Fatalf("scbake new: %v\n%s", err, out)
	}

	content, err := fileutil.SafeReadFile(projectPath, ".editorconfig")
	if err != nil {
		t.Fatalf(".editorconfig not created: %v", err)
	}
	if !strings.Contains(string(content), "registry_override") {
		t.Errorf("expected registry override content, got embedded:\n%s", string(content))
	}
}

func TestRegistryPull_WithVersion(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(tmpDir)

	archive := testutil.CreateTarGz(map[string]string{
		"makefile.tpl": "REGISTRY_OVERRIDE_MAKEFILE:\n\t@echo overridden\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			_, _ = w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	isolateUserDirs(t)

	out, err := runCLI("template", "registry", "add", "verreg", srv.URL, "--version", "1.2.0")
	if err != nil {
		t.Fatalf("add registry: %v\n%s", err, out)
	}

	out, err = runCLI("template", "pull", "makefile", "--registry", "verreg")
	if err != nil {
		t.Fatalf("pull template: %v\n%s", err, out)
	}

	projectPath := filepath.Join(tmpDir, "version-test-project")
	out, err = runCLI("new", "version-test-project", "--with", "makefile")
	if err != nil {
		t.Fatalf("scbake new: %v\n%s", err, out)
	}

	content, err := fileutil.SafeReadFile(projectPath, "Makefile")
	if err != nil {
		t.Fatalf("Makefile not created: %v", err)
	}
	if !strings.Contains(string(content), "REGISTRY_OVERRIDE_MAKEFILE") {
		t.Errorf("expected registry override content, got:\n%s", string(content))
	}
}

func TestRegistryPull_WithSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(tmpDir)

	archive := testutil.CreateTarGz(map[string]string{
		"scbake-template/.editorconfig.tpl": "root = true\n[*]\nindent_style = subdir_override\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			_, _ = w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	isolateUserDirs(t)

	out, err := runCLI("template", "registry", "add", "subreg", srv.URL, "--subdirectory", "scbake-template")
	if err != nil {
		t.Fatalf("add registry: %v\n%s", err, out)
	}

	out, err = runCLI("template", "pull", "editorconfig", "--registry", "subreg")
	if err != nil {
		t.Fatalf("pull template: %v\n%s", err, out)
	}

	cacheDir := filepath.Join(userCacheDir(t), "scbake", "templates", "subreg", "editorconfig")
	if _, err := os.Stat(filepath.Join(cacheDir, ".editorconfig.tpl")); os.IsNotExist(err) {
		t.Fatalf("subdirectory template not promoted to cache root: %s", cacheDir)
	}

	projectPath := filepath.Join(tmpDir, "subdir-test-project")
	out, err = runCLI("new", "subdir-test-project", "--with", "editorconfig")
	if err != nil {
		t.Fatalf("scbake new: %v\n%s", err, out)
	}

	content, err := fileutil.SafeReadFile(projectPath, ".editorconfig")
	if err != nil {
		t.Fatalf(".editorconfig not created: %v", err)
	}
	if !strings.Contains(string(content), "subdir_override") {
		t.Errorf("expected subdirectory override content, got:\n%s", string(content))
	}
}
