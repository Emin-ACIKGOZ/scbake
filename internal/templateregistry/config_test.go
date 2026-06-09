package templateregistry

import (
	"os"
	"path/filepath"
	"testing"
)

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
