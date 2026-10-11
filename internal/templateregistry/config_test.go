package templateregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedactURL is a regression test for SCB-034: registry URLs may embed
// credentials and must be stripped before they are printed or logged.
func TestRedactURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"strips user and password", "https://user:pass@host/x", "https://host/x"},
		{"strips username only", "https://user@host:8443/x", "https://host:8443/x"},
		{"preserves plain url", "https://host/a/b", "https://host/a/b"},
		{"preserves query without credentials", "https://host/x?q=1", "https://host/x?q=1"},
		{
			"redacts credential query params",
			"https://user:pass@host/x?token=abc&foo=bar",
			"https://host/x?foo=bar&token=REDACTED",
		},
		{"unparseable url becomes placeholder", "http://[::1", "[invalid registry URL]"},
		{"opaque userinfo becomes placeholder", "user:pass@host/x", "[invalid registry URL]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURL(tt.in)
			if got != tt.want {
				t.Errorf("RedactURL(%q) = %q, want %q", tt.in, got, tt.want)
			}
			for _, secret := range []string{"pass", "abc"} {
				if strings.Contains(got, secret) {
					t.Errorf("RedactURL(%q) = %q still leaks %q", tt.in, got, secret)
				}
			}
		})
	}
}

// TestRedactURL_FragmentsAndVendorQueryKeys covers credential carriers that the
// exact-key allowlist missed: URL fragments (OAuth implicit-flow tokens) and
// vendor-specific query parameters such as AWS pre-signed URL signatures.
func TestRedactURL_FragmentsAndVendorQueryKeys(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		secrets []string
	}{
		{"fragment token", "https://host/x#access_token=FRAGSECRET", []string{"FRAGSECRET", "access_token"}},
		{"aws signature", "https://host/x?X-Amz-Signature=AWSSECRET", []string{"AWSSECRET"}},
		{"short sig", "https://host/x?sig=SIGSECRET", []string{"SIGSECRET"}},
		{"key param", "https://host/x?key=KEYSECRET", []string{"KEYSECRET"}},
		{"credential param", "https://host/x?credential=CREDSECRET", []string{"CREDSECRET"}},
		{"fragment and userinfo", "https://user:pass@host/x#token=FRAGSECRET", []string{"FRAGSECRET", "user:pass"}},
		{"oauth code", "https://host/x?code=CODESECRET", []string{"CODESECRET"}},
		{"session id", "https://host/x?session=SESSIONSECRET", []string{"SESSIONSECRET"}},
		{"jwt", "https://host/x?jwt=JWTSECRET", []string{"JWTSECRET"}},
		{"sas token", "https://host/x?SAS=SASSECRET", []string{"SASSECRET"}},
		{"malformed query with semicolon", "https://host/x?token=SEMISECRET;v=1", []string{"SEMISECRET"}},
		{"malformed escape", "https://host/x?code=CODESECRET&x=%zz", []string{"CODESECRET"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactURL(tt.in)
			for _, secret := range tt.secrets {
				if strings.Contains(got, secret) {
					t.Errorf("RedactURL(%q) = %q still leaks %q", tt.in, got, secret)
				}
			}
		})
	}
}

func TestAddAndList(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	if err := m.Add("acme", "https://templates.acme.com/v1", "", "", ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	registries := m.List()
	if len(registries) != 1 {
		t.Fatalf("expected 1 registry, got %d", len(registries))
	}
	if registries[0].Name != "acme" {
		t.Errorf("expected name 'acme', got %q", registries[0].Name)
	}
	if registries[0].URL != "https://templates.acme.com/v1" {
		t.Errorf("expected URL 'https://templates.acme.com/v1', got %q", registries[0].URL)
	}
	if registries[0].Token != "" {
		t.Errorf("expected empty token, got %q", registries[0].Token)
	}
}

func TestAddDuplicate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	if err := m.Add("acme", "https://templates.acme.com/v1", "", "", ""); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}
	if err := m.Add("acme", "https://other.com/v1", "", "", ""); err == nil {
		t.Fatal("expected error for duplicate registry name")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	if err := m.Add("acme", "https://templates.acme.com/v1", "", "", ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := m.Add("internal", "https://templates.internal/v1", "", "", ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if err := m.Remove("acme"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	registries := m.List()
	if len(registries) != 1 {
		t.Fatalf("expected 1 registry after removal, got %d", len(registries))
	}
	if registries[0].Name != "internal" {
		t.Errorf("expected remaining registry 'internal', got %q", registries[0].Name)
	}
}

func TestRemoveNotFound(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	if err := m.Remove("nonexistent"); err == nil {
		t.Fatal("expected error when removing non-existent registry")
	}
}

func TestListEmpty(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	registries := m.List()
	if len(registries) != 0 {
		t.Fatalf("expected empty list, got %d", len(registries))
	}
}

func TestPersistenceAcrossManagers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")

	m1 := NewManagerWithPaths(configPath, cacheDir)
	if err := m1.Add("acme", "https://templates.acme.com/v1", "tok_abc", "1.0.0", "template"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	m2 := NewManagerWithPaths(configPath, cacheDir)
	if err := m2.load(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	registries := m2.List()
	if len(registries) != 1 {
		t.Fatalf("expected 1 registry after reload, got %d", len(registries))
	}
	if registries[0].Name != "acme" {
		t.Errorf("expected 'acme', got %q", registries[0].Name)
	}
	if registries[0].Token != "tok_abc" {
		t.Errorf("expected token 'tok_abc', got %q", registries[0].Token)
	}
	if registries[0].Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got %q", registries[0].Version)
	}
	if registries[0].Subdirectory != "template" {
		t.Errorf("expected subdirectory 'template', got %q", registries[0].Subdirectory)
	}
}

func TestAddWithAllFields(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.Add("acme", "https://templates.acme.com/v1", "tok_xyz", "2.0.0", "scbake-template"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	r := m.Get("acme")
	if r == nil {
		t.Fatal("Get returned nil")
	}
	if r.URL != "https://templates.acme.com/v1" {
		t.Errorf("expected URL, got %q", r.URL)
	}
	if r.Token != "tok_xyz" {
		t.Errorf("expected token 'tok_xyz', got %q", r.Token)
	}
	if r.Version != "2.0.0" {
		t.Errorf("expected version '2.0.0', got %q", r.Version)
	}
	if r.Subdirectory != "scbake-template" {
		t.Errorf("expected subdirectory 'scbake-template', got %q", r.Subdirectory)
	}
}

func TestSetToken(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.Add("acme", "https://templates.acme.com/v1", "", "", ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if err := m.SetToken("acme", "new_token"); err != nil {
		t.Fatalf("SetToken failed: %v", err)
	}

	r := m.Get("acme")
	if r == nil {
		t.Fatal("Get returned nil after SetToken")
	}
	if r.Token != "new_token" {
		t.Errorf("expected 'new_token', got %q", r.Token)
	}

	// Verify persistence
	m2 := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))
	if err := m2.load(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	r2 := m2.Get("acme")
	if r2.Token != "new_token" {
		t.Errorf("persisted token expected 'new_token', got %q", r2.Token)
	}
}

func TestSetTokenNotFound(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.SetToken("nonexistent", "token"); err == nil {
		t.Fatal("expected error for SetToken on non-existent registry")
	}
}

func TestGetNotFound(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if r := m.Get("nonexistent"); r != nil {
		t.Fatalf("expected nil, got %+v", r)
	}
}

func TestCacheDir(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "mycache")
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), cacheDir)

	if m.CacheDir() != cacheDir {
		t.Errorf("expected cache dir %q, got %q", cacheDir, m.CacheDir())
	}

	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Error("cache dir should not be created by CacheDir() getter")
	}
}

func TestTemplateCachePath(t *testing.T) {
	result := TemplateCachePath("/cache", "acme", "go-api")
	expected := filepath.Join("/cache", "acme", "go-api")
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// TestValidateName covers the single-path-segment validator used for registry,
// template, and project names.
func TestValidateName(t *testing.T) {
	valid := []string{"acme", "go-api", "a", "A1._-", "my project"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"",
		".",
		"..",
		"a/b",
		`a\b`,
		"/abs",
		`C:\abs`,
		"../evil",
		"bad\x00name",
		"bad\nname",
		"colon:name",
		"CON",
		"nul",
		"COM1",
		"LPT9",
		"trailing.",
		"trailing ",
		".. ",
	}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) = nil, want error", name)
		}
	}
}

// TestAddRejectsTraversalRegistryName is a regression test for SCB-010: a
// traversal registry name must be rejected before it is stored, so it can never
// be joined into a cache path that escapes the cache root.
func TestAddRejectsTraversalRegistryName(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	for _, name := range []string{"../evil", "..", "a/b", "/abs"} {
		if err := m.Add(name, "https://example.com/v1", "", "", ""); err == nil {
			t.Errorf("Add(%q) = nil, want error for traversal registry name", name)
		}
	}
}

// TestAddRejectsTraversalSubdirectory is a regression test for SCB-011: a
// --subdirectory value that is absolute or contains ".." must be rejected.
func TestAddRejectsTraversalSubdirectory(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	for _, sub := range []string{"../..", "..", "nested/../../etc", "/abs", `..\..`, ".. ", "template/CON"} {
		if err := m.Add("acme", "https://example.com/v1", "", "", sub); err == nil {
			t.Errorf("Add with subdirectory %q = nil, want error", sub)
		}
	}

	// A legitimate relative (possibly multi-segment) subdirectory is accepted.
	if err := m.Add("acme", "https://example.com/v1", "", "", "template/nested"); err != nil {
		t.Errorf("Add with valid subdirectory failed: %v", err)
	}
}

// TestListPreservesInsertionOrder is a regression test for SCB-021: the
// registry list (and therefore the resolution precedence) must preserve the
// order registries were added rather than sorting by name.
func TestListPreservesInsertionOrder(t *testing.T) {
	dir := t.TempDir()
	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), filepath.Join(dir, "cache"))

	if err := m.Add("zeta", "https://zeta.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add zeta failed: %v", err)
	}
	if err := m.Add("alpha", "https://alpha.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add alpha failed: %v", err)
	}

	list := m.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 registries, got %d", len(list))
	}
	if list[0].Name != "zeta" || list[1].Name != "alpha" {
		t.Fatalf("expected insertion order [zeta alpha], got [%s %s]", list[0].Name, list[1].Name)
	}
}

// TestTemplateCachePathRejectsTraversal is a regression test for SCB-010:
// TemplateCachePath must never build a path outside the cache root from a
// traversal registry or template name.
func TestTemplateCachePathRejectsTraversal(t *testing.T) {
	for _, tc := range []struct{ registry, template string }{
		{"../evil", "go-api"},
		{"acme", "../../evil"},
		{"..", ".."},
		{"a/b", "c"},
	} {
		if got := TemplateCachePath("/cache", tc.registry, tc.template); got != "" {
			t.Errorf("TemplateCachePath(/cache, %q, %q) = %q, want empty", tc.registry, tc.template, got)
		}
	}
}
