// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package templateregistry

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestIssue138Revision_ReadOnlyConfigRestoresCache proves the staged eviction is
// rolled back when the config cannot be saved: the on-disk config is byte
// identical, the cache entry is back at its original path, siblings survive, and
// no staging directory is left behind.
//
//nolint:cyclop // fixture setup plus one branch per independent rollback invariant
func TestIssue138Revision_ReadOnlyConfigRestoresCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not enforce Unix read-only semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only file permissions do not block writes")
	}

	_, cacheDir, m := issue138CacheFixture(t)
	teamFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "team")
	otherFile := issue138SeedEntry(t, cacheDir, "other", "go-api", "main.go.tpl", "other")
	teamDir := filepath.Dir(filepath.Dir(teamFile))

	configPath := filepath.Join(filepath.Dir(cacheDir), "registries.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("fixture config missing at %s: %v", configPath, err)
	}
	before, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		t.Fatalf("reading config before Remove: %v", err)
	}

	if err := os.Chmod(configPath, 0o400); err != nil {
		t.Fatalf("chmod config read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(configPath, 0o600) })

	err = m.Remove("team")
	if err == nil {
		t.Fatal("Remove succeeded with a read-only config, want a save error")
	}
	if !strings.Contains(err.Error(), "registry config") && !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error does not name the failed save: %v", err)
	}

	after, readErr := os.ReadFile(filepath.Clean(configPath))
	if readErr != nil {
		t.Fatalf("reading config after Remove: %v", readErr)
	}
	if string(after) != string(before) {
		t.Errorf("config changed after failed save:\nbefore=%s\nafter=%s", before, after)
	}
	if got := m.List(); len(got) != 2 || got[0].Name != "team" || got[1].Name != "other" {
		t.Errorf("in-memory config was not restored: %v", got)
	}
	if _, err := os.Stat(teamFile); err != nil {
		t.Errorf("cache entry was not restored to %s: %v", teamDir, err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Errorf("sibling registry cache was damaged: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
}

// TestIssue138Revision_ReadOnlyCacheLeavesEverything proves a staging failure is
// detected before the config is touched: config, cache, and in-memory state are
// unchanged and the error names the registry and operation.
//
//nolint:cyclop // fixture setup plus one branch per independent invariant
func TestIssue138Revision_ReadOnlyCacheLeavesEverything(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not enforce Unix read-only semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only directory permissions do not block renames")
	}

	_, cacheDir, m := issue138CacheFixture(t)
	teamFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "team")
	otherFile := issue138SeedEntry(t, cacheDir, "other", "go-api", "main.go.tpl", "other")

	configPath := filepath.Join(filepath.Dir(cacheDir), "registries.json")
	before, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		t.Fatalf("reading config before Remove: %v", err)
	}

	if err := os.Chmod(cacheDir, 0o500); err != nil { //nolint:gosec // test fixture: deliberately read-only cache root
		t.Fatalf("chmod cache read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(cacheDir, 0o750) }) //nolint:gosec // restore permissions so TempDir cleanup works

	err = m.Remove("team")
	if err == nil {
		t.Fatal("Remove succeeded with a read-only cache root, want a staging error")
	}
	if !strings.Contains(err.Error(), "team") || !strings.Contains(err.Error(), "evicting cached templates") {
		t.Errorf("error is not actionable (registry and operation): %v", err)
	}

	after, readErr := os.ReadFile(filepath.Clean(configPath))
	if readErr != nil {
		t.Fatalf("reading config after Remove: %v", readErr)
	}
	if string(after) != string(before) {
		t.Errorf("config changed after failed staging:\nbefore=%s\nafter=%s", before, after)
	}
	if got := m.List(); len(got) != 2 {
		t.Errorf("in-memory config changed after failed staging: %v", got)
	}
	if _, err := os.Stat(teamFile); err != nil {
		t.Errorf("team cache was touched by failed staging: %v", err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Errorf("sibling registry cache was damaged: %v", err)
	}
}

// TestIssue138Revision_RemoveSymlinkToSiblingUnlinksOnly proves a symlink at the
// registry path is removed as a link; its sibling target and contents survive.
func TestIssue138Revision_RemoveSymlinkToSiblingUnlinksOnly(t *testing.T) {
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

	otherFile := issue138SeedEntry(t, cacheDir, "other", "go-api", "main.go.tpl", "other")
	link := filepath.Join(cacheDir, "team")
	if err := os.Symlink(filepath.Join(cacheDir, "other"), link); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	if err := m.Remove("team"); err != nil {
		t.Fatalf("Remove with symlinked cache: %v", err)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("symlink was not unlinked: %v", err)
	}
	if _, err := os.Stat(otherFile); err != nil {
		t.Errorf("symlink target was modified: %v", err)
	}
	if target, err := os.Readlink(link); err == nil {
		t.Errorf("link still resolves to %q", target)
	}
	assertNoEvictionStaging(t, cacheDir)
}

// TestIssue138Revision_RemoveInvalidNameIsNoOp seeds a config that bypasses Add's
// validation and proves an unsafe name never renames or deletes anything; only
// the config entry is dropped.
func TestIssue138Revision_RemoveInvalidNameIsNoOp(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	writeTestFile(t, configPath, `{"registries":[{"name":"..","url":"https://bad.example/v1"}]}`)
	writeTestFile(t, filepath.Join(cacheDir, "keep.txt"), "must survive")

	m := NewManagerWithPaths(configPath, cacheDir)
	if err := m.load(); err != nil {
		t.Fatalf("load seeded config: %v", err)
	}
	if err := m.Remove(".."); err != nil {
		t.Fatalf("Remove invalid name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "keep.txt")); err != nil {
		t.Errorf("invalid-name removal touched the cache: %v", err)
	}
	if got := m.List(); len(got) != 0 {
		t.Errorf("config entry was not removed: %v", got)
	}
	assertNoEvictionStaging(t, cacheDir)
}

// TestIssue138Revision_OrderFileCannotAdmitUnconfigured proves the order file
// cannot make a cached, unconfigured registry eligible.
func TestIssue138Revision_OrderFileCannotAdmitUnconfigured(t *testing.T) {
	cacheDir := t.TempDir()
	issue138SeedEntry(t, cacheDir, "ghost", "go-api", "main.go.tpl", "ghost")
	issue138WriteOrder(t, cacheDir, "ghost")

	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team"}); got != "" || reg != "" {
		t.Errorf("order file admitted unconfigured registry: got (%q, %q)", got, reg)
	}
}

// assertNoEvictionStaging fails when a .evict-* staging entry survived.
func assertNoEvictionStaging(t *testing.T, cacheDir string) {
	t.Helper()

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("reading cache dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".evict-") {
			t.Errorf("staging entry %q was not cleaned up", entry.Name())
		}
	}
}
