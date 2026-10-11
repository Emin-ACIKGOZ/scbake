package templateregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveCachePath_NotFound(t *testing.T) {
	dir := t.TempDir()
	result := ResolveCachePath(dir, "", "some/file.tpl")
	if result != "" {
		t.Errorf("expected empty for empty cache, got %q", result)
	}
}

func TestResolveCachePath_EmptyCacheDir(t *testing.T) {
	result := ResolveCachePath("", "", "some/file.tpl")
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

	result := ResolveCachePath(dir, "", "templates/main.go.tpl")
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

	result := ResolveCachePath(dir, "", "shared.tpl")
	if result == "" {
		t.Fatal("expected to find file in registry-b")
	}
	if result != tplPath {
		t.Errorf("expected %q, got %q", tplPath, result)
	}
}

func TestResolveCachePath_NestedTemplateDir(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "acme")
	tplDir := filepath.Join(registryDir, "editorconfig")
	tplPath := filepath.Join(tplDir, "main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// The requested template name constrains the nested lookup to "editorconfig".
	result := ResolveCachePath(dir, "editorconfig", "main.go.tpl")
	if result == "" {
		t.Fatal("expected non-empty result for file in nested template dir")
	}
	if result != tplPath {
		t.Errorf("expected %q, got %q", tplPath, result)
	}
}

// TestResolveCachePath_ConstrainsToRequestedTemplate is a regression test for
// SCB-022: a request for one template must never resolve to a different
// template's file that happens to share a name. Before the fix the resolver
// scanned every template subdirectory and returned the first sibling match.
func TestResolveCachePath_ConstrainsToRequestedTemplate(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "acme")

	aDir := filepath.Join(registryDir, "a")
	bDir := filepath.Join(registryDir, "b")
	for _, d := range []string{aDir, bDir} {
		//nolint:gosec // test temp directory
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatalf("MkdirAll failed: %v", err)
		}
	}
	aPath := filepath.Join(aDir, "main.go.tpl")
	bPath := filepath.Join(bDir, "main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(aPath, []byte("template a"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(bPath, []byte("template b"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	result := ResolveCachePath(dir, "a", "main.go.tpl")
	if result != aPath {
		t.Fatalf("expected template a's file %q, got %q", aPath, result)
	}
	if result == bPath {
		t.Fatalf("resolved to a different template's file %q", bPath)
	}

	// Requesting b must return b's file too. The old sibling scan returned the
	// first match in directory order ("a"), so this direction catches the bug
	// regardless of which directory sorts first.
	result = ResolveCachePath(dir, "b", "main.go.tpl")
	if result != bPath {
		t.Fatalf("expected template b's file %q, got %q", bPath, result)
	}
	if result == aPath {
		t.Fatalf("resolved to a different template's file %q", aPath)
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
