// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package templateregistry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// issue138SeedEntry creates a manifest-verified cache entry for
// <cacheDir>/<registry>/<template>/<file> and returns the file path.
func issue138SeedEntry(t *testing.T, cacheDir, registry, templateName, file, content string) string {
	t.Helper()

	tplDir := filepath.Join(cacheDir, registry, templateName)
	writeTestFile(t, filepath.Join(tplDir, file), content)
	writeValidManifest(t, tplDir, templateName)
	return filepath.Join(tplDir, file)
}

// issue138WriteOrder records the registry precedence order used by the resolver.
func issue138WriteOrder(t *testing.T, cacheDir string, names ...string) {
	t.Helper()

	body := `{"registries":["` + strings.Join(names, `","`) + `"]}`
	writeTestFile(t, filepath.Join(cacheDir, registryOrderFileName), body)
}

// TestIssue138_ResolverIgnoresUnconfiguredRegistry proves membership is
// config-derived: a fully valid cache entry for an unconfigured registry is
// never returned.
func TestIssue138_ResolverIgnoresUnconfiguredRegistry(t *testing.T) {
	cacheDir := t.TempDir()
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	got, registry := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"other"})
	if got != "" || registry != "" {
		t.Errorf("unconfigured registry was consulted: got (%q, %q), want empty", got, registry)
	}
}

// TestIssue138_ResolverEmptyAllowlistFailsClosed pins that nil and empty
// allowlists disable the registry tier instead of scanning every cache dir.
func TestIssue138_ResolverEmptyAllowlistFailsClosed(t *testing.T) {
	cacheDir := t.TempDir()
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")
	issue138WriteOrder(t, cacheDir, "team")

	for name, allow := range map[string][]string{
		"nil":   nil,
		"empty": {},
	} {
		t.Run(name, func(t *testing.T) {
			got, registry := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", allow)
			if got != "" || registry != "" {
				t.Errorf("empty allowlist scanned the cache: got (%q, %q), want empty", got, registry)
			}
		})
	}
}

// TestIssue138_ResolverOrderFilePrecedence resolves among multiple configured
// registries using the recorded order, and reports the supplying registry.
func TestIssue138_ResolverOrderFilePrecedence(t *testing.T) {
	cacheDir := t.TempDir()
	alpha := issue138SeedEntry(t, cacheDir, "alpha", "go-api", "main.go.tpl", "from alpha")
	zeta := issue138SeedEntry(t, cacheDir, "zeta", "go-api", "main.go.tpl", "from zeta")

	// Lexical order would pick alpha; the order file must pick zeta.
	issue138WriteOrder(t, cacheDir, "zeta", "alpha")

	got, registry := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"alpha", "zeta"})
	if got != zeta || registry != "zeta" {
		t.Errorf("order-file precedence: got (%q, %q), want (%q, %q)", got, registry, zeta, "zeta")
	}

	// Without an order file, precedence falls back to lexical order.
	if err := os.Remove(filepath.Join(cacheDir, registryOrderFileName)); err != nil {
		t.Fatalf("removing order file: %v", err)
	}
	got, registry = ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"zeta", "alpha"})
	if got != alpha || registry != "alpha" {
		t.Errorf("lexical fallback precedence: got (%q, %q), want (%q, %q)", got, registry, alpha, "alpha")
	}
}

// TestIssue138_ResolverNameIsTheSupplyingRegistry verifies the returned name
// matches the registry whose file was selected, not merely the first configured.
func TestIssue138_ResolverNameIsTheSupplyingRegistry(t *testing.T) {
	cacheDir := t.TempDir()
	issue138SeedEntry(t, cacheDir, "alpha", "go-api", "only.tpl", "only alpha has this file")
	issue138SeedEntry(t, cacheDir, "beta", "go-api", "main.go.tpl", "from beta")

	got, registry := ResolveCacheEntry(cacheDir, "go-api", "only.tpl", []string{"beta", "alpha"})
	want := filepath.Join(cacheDir, "alpha", "go-api", "only.tpl")
	if got != want || registry != "alpha" {
		t.Errorf("got (%q, %q), want (%q, %q)", got, registry, want, "alpha")
	}
}

// issue138CacheFixture creates a cache root containing two registries plus the
// order file and returns (configPath, cacheDir, manager).
func issue138CacheFixture(t *testing.T) (string, string, *Manager) {
	t.Helper()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	if err := m.Add("team", "https://team.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add team: %v", err)
	}
	if err := m.Add("other", "https://other.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add other: %v", err)
	}
	return configPath, cacheDir, m
}

// TestIssue138_RemoveEvictsOnlyTargetRegistry proves eviction is scoped to
// <cache>/<name>: sibling registries, the cache root, and the order file stay.
func TestIssue138_RemoveEvictsOnlyTargetRegistry(t *testing.T) {
	configPath, cacheDir, m := issue138CacheFixture(t)

	teamFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "team")
	otherFile := issue138SeedEntry(t, cacheDir, "other", "go-api", "main.go.tpl", "other")
	orderPath := filepath.Join(cacheDir, registryOrderFileName)

	if err := m.Remove("team"); err != nil {
		t.Fatalf("Remove(team): %v", err)
	}

	if _, err := os.Stat(filepath.Dir(filepath.Dir(teamFile))); !os.IsNotExist(err) {
		t.Errorf("team cache dir survived removal: %v", err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Errorf("other registry cache was damaged by eviction: %v", err)
	}
	if _, err := os.Stat(cacheDir); err != nil {
		t.Errorf("cache root was removed: %v", err)
	}
	if info, err := os.Stat(orderPath); err != nil || !info.Mode().IsRegular() {
		t.Errorf("order file was removed: %v", err)
	}
	if order := readRegistryOrder(cacheDir); len(order) != 1 || order[0] != "other" {
		t.Errorf("order file = %v, want [other]", order)
	}

	// The config change is persisted: a fresh manager sees only "other".
	m2 := NewManagerWithPaths(configPath, cacheDir)
	if err := m2.load(); err != nil {
		t.Fatalf("reloading config: %v", err)
	}
	if got := m2.List(); len(got) != 1 || got[0].Name != "other" {
		t.Errorf("persisted registries = %v, want [other]", got)
	}
}

// TestIssue138_RemoveMissingCacheIsOK proves eviction tolerates a missing cache
// root, a missing registry directory, and an unset cache path.
func TestIssue138_RemoveMissingCacheIsOK(t *testing.T) {
	t.Run("missing target directory", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "registries.json")
		cacheDir := filepath.Join(t.TempDir(), "cache")
		m := NewManagerWithPaths(configPath, cacheDir)
		if err := os.MkdirAll(cacheDir, cacheDirPerm); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := m.Add("team", "https://team.example/v1", "", "", ""); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := m.Remove("team"); err != nil {
			t.Errorf("Remove with no cached templates: %v", err)
		}
	})

	t.Run("missing cache root", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "registries.json")
		cacheDir := filepath.Join(t.TempDir(), "absent", "cache")
		m := NewManagerWithPaths(configPath, cacheDir)
		if err := m.Add("team", "https://team.example/v1", "", "", ""); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := m.Remove("team"); err != nil {
			t.Errorf("Remove with no cache root: %v", err)
		}
	})

	t.Run("empty cache path", func(t *testing.T) {
		m := NewManagerWithPaths(filepath.Join(t.TempDir(), "registries.json"), "")
		if err := m.Add("team", "https://team.example/v1", "", "", ""); err != nil {
			t.Fatalf("Add: %v", err)
		}
		if err := m.Remove("team"); err != nil {
			t.Errorf("Remove with empty cache dir: %v", err)
		}
	})
}

// TestIssue138_RemoveEscapingSymlinkUnlinksOnly proves a symlinked registry
// directory is removed as a link, never followed.
func TestIssue138_RemoveEscapingSymlinkUnlinksOnly(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)
	if err := m.Add("team", "https://team.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := os.MkdirAll(cacheDir, cacheDirPerm); err != nil {
		t.Fatalf("MkdirAll cache: %v", err)
	}

	outside := filepath.Join(dir, "outside")
	sentinel := filepath.Join(outside, "keep.txt")
	writeTestFile(t, sentinel, "must survive")

	link := filepath.Join(cacheDir, "team")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	if err := m.Remove("team"); err != nil {
		t.Fatalf("Remove with symlinked cache: %v", err)
	}

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("symlink was not removed: %v", err)
	}
	if content, err := os.ReadFile(filepath.Clean(sentinel)); err != nil || string(content) != "must survive" {
		t.Errorf("eviction followed the symlink and touched %s: %v", sentinel, err)
	}
}

// TestIssue138_EvictionFailureKeepsConfigConsistent proves a failed eviction
// aborts Remove, leaves the registry configured on disk, and returns an
// actionable error naming the registry.
func TestIssue138_EvictionFailureKeepsConfigConsistent(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	// A regular file where the cache directory should be makes every
	// <cache>/<name> lookup fail with ENOTDIR on all platforms.
	cachePath := filepath.Join(dir, "cache-is-a-file")
	if err := os.WriteFile(cachePath, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("seeding cache file: %v", err)
	}

	m := NewManagerWithPaths(configPath, cachePath)
	if err := m.Add("team", "https://team.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}

	err := m.Remove("team")
	if err == nil {
		t.Fatal("expected eviction failure, got nil")
	}
	if !strings.Contains(err.Error(), "team") || !strings.Contains(err.Error(), "evicting cached templates") {
		t.Errorf("error is not actionable (should name the registry and operation): %v", err)
	}

	if got := m.List(); len(got) != 1 || got[0].Name != "team" {
		t.Errorf("in-memory config changed after failed eviction: %v", got)
	}
	m2 := NewManagerWithPaths(configPath, cachePath)
	if err := m2.load(); err != nil {
		t.Fatalf("reloading config: %v", err)
	}
	if got := m2.List(); len(got) != 1 || got[0].Name != "team" {
		t.Errorf("persisted config changed after failed eviction: %v", got)
	}
}
