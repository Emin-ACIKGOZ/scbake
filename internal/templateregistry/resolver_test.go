package templateregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCachePath_NotFound(t *testing.T) {
	dir := t.TempDir()
	result := ResolveCachePath(dir, "some/file.tpl")
	if result != "" {
		t.Errorf("expected empty for empty cache, got %q", result)
	}
}

func TestResolveCachePath_EmptyCacheDir(t *testing.T) {
	result := ResolveCachePath("", "some/file.tpl")
	if result != "" {
		t.Errorf("expected empty for empty cache dir, got %q", result)
	}
}

func TestResolveCachePath_FindsFile(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "acme")
	tplPath := filepath.Join(registryDir, "templates/main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(tplPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	result := ResolveCachePath(dir, "templates/main.go.tpl")
	if result == "" {
		t.Fatal("expected non-empty result for existing file")
	}
	if result != tplPath {
		t.Errorf("expected %q, got %q", tplPath, result)
	}
}

func TestResolveCachePath_SearchesAllRegistries(t *testing.T) {
	dir := t.TempDir()
	registry1 := filepath.Join(dir, "registry-a")
	registry2 := filepath.Join(dir, "registry-b")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(registry1, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(registry2, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	tplPath := filepath.Join(registry2, "shared.tpl")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	result := ResolveCachePath(dir, "shared.tpl")
	if result == "" {
		t.Fatal("expected to find file in registry-b")
	}
	if result != tplPath {
		t.Errorf("expected %q, got %q", tplPath, result)
	}
}

func TestLookupPath_Found(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "my-registry")
	tplPath := filepath.Join(registryDir, "main.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(registryDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	result := LookupPath(dir, "my-registry", "main.tpl")
	if result != tplPath {
		t.Errorf("expected %q, got %q", tplPath, result)
	}
}

func TestLookupPath_NotFound(t *testing.T) {
	dir := t.TempDir()
	result := LookupPath(dir, "my-registry", "nonexistent.tpl")
	if result != "" {
		t.Errorf("expected empty for missing file, got %q", result)
	}
}

func TestLookupPath_EmptyArgs(t *testing.T) {
	if result := LookupPath("", "reg", "file"); result != "" {
		t.Errorf("expected empty for empty cacheDir, got %q", result)
	}
	if result := LookupPath("/cache", "", "file"); result != "" {
		t.Errorf("expected empty for empty registryName, got %q", result)
	}
}
