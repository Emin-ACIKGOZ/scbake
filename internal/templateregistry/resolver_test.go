package templateregistry

import (
	"os"
	"path/filepath"
	"testing"
)

// writeValidManifest records a manifest matching tplDir's current contents so
// tests exercise the verified cache-hit path.
func writeValidManifest(t *testing.T, tplDir, name string) {
	t.Helper()
	hash, err := hashTree(tplDir)
	if err != nil {
		t.Fatalf("hashTree failed: %v", err)
	}
	if err := writeCacheManifest(tplDir+manifestSuffix, name, hash); err != nil {
		t.Fatalf("writeCacheManifest failed: %v", err)
	}
}

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
	tplDir := filepath.Join(registryDir, "go-api")
	tplPath := filepath.Join(tplDir, "templates/main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(filepath.Dir(tplPath), 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	writeValidManifest(t, tplDir, "go-api")

	result := ResolveCachePath(dir, "go-api", "templates/main.go.tpl")
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

	// Only registry-b holds a verified template; registry-a must be skipped and
	// the search must continue until the file is found.
	tplDir := filepath.Join(registry2, "go-api")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	tplPath := filepath.Join(tplDir, "shared.tpl")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	writeValidManifest(t, tplDir, "go-api")

	result := ResolveCachePath(dir, "go-api", "shared.tpl")
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
	writeValidManifest(t, tplDir, "editorconfig")

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
	writeValidManifest(t, aDir, "a")
	writeValidManifest(t, bDir, "b")

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

// TestResolveCachePath_IgnoresTamperedTemplate is a regression test for
// SCB-025: a template directory whose contents no longer match its manifest
// must not be rendered.
func TestResolveCachePath_IgnoresTamperedTemplate(t *testing.T) {
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "acme", "go-api")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	tplPath := filepath.Join(tplDir, "main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("trusted"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	writeValidManifest(t, tplDir, "go-api")

	if got := ResolveCachePath(dir, "go-api", "main.go.tpl"); got != tplPath {
		t.Fatalf("expected verified template to resolve to %q, got %q", tplPath, got)
	}

	// Tamper with the file after the manifest was recorded.
	//nolint:gosec // test temp directory
	if err := os.WriteFile(tplPath, []byte("attacker"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if got := ResolveCachePath(dir, "go-api", "main.go.tpl"); got != "" {
		t.Errorf("expected tampered template to be ignored, got %q", got)
	}
}

// TestResolveCachePath_IgnoresUnmanifestedTemplate verifies the fail-closed
// behavior: a template directory without a manifest is never rendered.
func TestResolveCachePath_IgnoresUnmanifestedTemplate(t *testing.T) {
	dir := t.TempDir()
	tplDir := filepath.Join(dir, "acme", "go-api")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}
	//nolint:gosec // test temp directory
	if err := os.WriteFile(filepath.Join(tplDir, "main.go.tpl"), []byte("content"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if got := ResolveCachePath(dir, "go-api", "main.go.tpl"); got != "" {
		t.Errorf("expected template without a manifest to be ignored, got %q", got)
	}
}

// TestResolveCachePath_PrefersVerifiedTemplateOverLegacyFlat is a regression
// test for F5: an unverified flat/legacy file must not shadow a verified
// per-template file with the same relative path. Before the fix the resolver
// checked the flat path first, so a planted legacy file won.
func TestResolveCachePath_PrefersVerifiedTemplateOverLegacyFlat(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "acme")
	tplDir := filepath.Join(registryDir, "go-api")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(tplDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	// Verified per-template file.
	nestedPath := filepath.Join(tplDir, "main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(nestedPath, []byte("verified"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	writeValidManifest(t, tplDir, "go-api")

	// Unverified flat/legacy file with the same relative path, planted to shadow
	// the verified one.
	flatPath := filepath.Join(registryDir, "main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(flatPath, []byte("legacy attacker"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	got := ResolveCachePath(dir, "go-api", "main.go.tpl")
	if got != nestedPath {
		t.Fatalf("expected verified per-template file %q, got %q", nestedPath, got)
	}
}

// TestResolveCachePath_IgnoresFlatLegacyFile is a regression test for the
// unverified flat cache bypass: a flat <registry>/<cleanPath> file must never be
// returned, even when no verified per-template copy exists. Pull/PullFromURL
// only ever write the per-template layout, so a flat file can only be planted
// locally and must not be rendered.
func TestResolveCachePath_IgnoresFlatLegacyFile(t *testing.T) {
	dir := t.TempDir()
	registryDir := filepath.Join(dir, "acme")
	//nolint:gosec // test temp directory
	if err := os.MkdirAll(registryDir, 0755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	flatPath := filepath.Join(registryDir, "main.go.tpl")
	//nolint:gosec // test temp directory
	if err := os.WriteFile(flatPath, []byte("legacy attacker"), 0644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	if got := ResolveCachePath(dir, "go-api", "main.go.tpl"); got != "" {
		t.Errorf("expected flat legacy file to be ignored, got %q", got)
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
