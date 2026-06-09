package integration

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTarGz(files map[string]string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, content := range files {
		tw.WriteHeader(&tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0644,
		})
		tw.Write([]byte(content))
	}

	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestRegistryPullEndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(origWd) })
	_ = os.Chdir(tmpDir)

	archive := createTarGz(map[string]string{
		".editorconfig.tpl": "root = true\n[*]\nindent_style = registry_override\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	xdgConfig := filepath.Join(tmpDir, "xdg-config")
	xdgCache := filepath.Join(tmpDir, "xdg-cache")
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("XDG_CACHE_HOME", xdgCache)

	out, err := runCLI("template", "registry", "add", "testreg", srv.URL)
	if err != nil {
		t.Fatalf("add registry: %v\n%s", err, out)
	}

	out, err = runCLI("template", "pull", "editorconfig", "--registry", "testreg")
	if err != nil {
		t.Fatalf("pull template: %v\n%s", err, out)
	}

	cacheDir := filepath.Join(xdgCache, "scbake/templates", "testreg", "editorconfig")
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		t.Fatalf("cache dir not created: %s", cacheDir)
	}

	projectPath := filepath.Join(tmpDir, "registry-test-project")
	out, err = runCLI("new", "registry-test-project", "--with", "editorconfig")
	if err != nil {
		t.Fatalf("scbake new: %v\n%s", err, out)
	}

	content, err := os.ReadFile(filepath.Join(projectPath, ".editorconfig"))
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

	archive := createTarGz(map[string]string{
		"makefile.tpl": "REGISTRY_OVERRIDE_MAKEFILE:\n\t@echo overridden\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	xdgConfig := filepath.Join(tmpDir, "xdg-config")
	xdgCache := filepath.Join(tmpDir, "xdg-cache")
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("XDG_CACHE_HOME", xdgCache)

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

	content, err := os.ReadFile(filepath.Join(projectPath, "Makefile"))
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

	archive := createTarGz(map[string]string{
		"scbake-template/.editorconfig.tpl": "root = true\n[*]\nindent_style = subdir_override\n",
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			w.Write(archive)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	xdgConfig := filepath.Join(tmpDir, "xdg-config")
	xdgCache := filepath.Join(tmpDir, "xdg-cache")
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("XDG_CACHE_HOME", xdgCache)

	out, err := runCLI("template", "registry", "add", "subreg", srv.URL, "--subdirectory", "scbake-template")
	if err != nil {
		t.Fatalf("add registry: %v\n%s", err, out)
	}

	out, err = runCLI("template", "pull", "editorconfig", "--registry", "subreg")
	if err != nil {
		t.Fatalf("pull template: %v\n%s", err, out)
	}

	cacheDir := filepath.Join(xdgCache, "scbake/templates", "subreg", "editorconfig")
	if _, err := os.Stat(filepath.Join(cacheDir, ".editorconfig.tpl")); os.IsNotExist(err) {
		t.Fatalf("subdirectory template not promoted to cache root: %s", cacheDir)
	}

	projectPath := filepath.Join(tmpDir, "subdir-test-project")
	out, err = runCLI("new", "subdir-test-project", "--with", "editorconfig")
	if err != nil {
		t.Fatalf("scbake new: %v\n%s", err, out)
	}

	content, err := os.ReadFile(filepath.Join(projectPath, ".editorconfig"))
	if err != nil {
		t.Fatalf(".editorconfig not created: %v", err)
	}
	if !strings.Contains(string(content), "subdir_override") {
		t.Errorf("expected subdirectory override content, got:\n%s", string(content))
	}
}
