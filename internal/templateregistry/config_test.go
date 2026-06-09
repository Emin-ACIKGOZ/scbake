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

	if err := m.Add("acme", "https://templates.acme.com/v1"); err != nil {
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
}

func TestAddDuplicate(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	if err := m.Add("acme", "https://templates.acme.com/v1"); err != nil {
		t.Fatalf("first Add failed: %v", err)
	}
	if err := m.Add("acme", "https://other.com/v1"); err == nil {
		t.Fatal("expected error for duplicate registry name")
	}
}

func TestRemove(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	if err := m.Add("acme", "https://templates.acme.com/v1"); err != nil {
		t.Fatalf("Add failed: %v", err)
	}
	if err := m.Add("internal", "https://templates.internal/v1"); err != nil {
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
	if err := m1.Add("acme", "https://templates.acme.com/v1"); err != nil {
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
