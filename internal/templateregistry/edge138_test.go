// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package templateregistry

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// edgeAge moves an entry's modification time comfortably past the sweep grace
// window. It intentionally uses a literal duration instead of the unexported
// constant so this file also compiles against the pre-fix tip during the
// fail-on-tip comparison.
func edgeAge(t *testing.T, path string) {
	t.Helper()

	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("age %s: %v", path, err)
	}
}

// edgeRoots lists .evict-* entries under the cache root.
func edgeRoots(t *testing.T, cacheDir string) []string {
	t.Helper()

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("reading cache dir: %v", err)
	}
	roots := make([]string, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), evictionStagePrefix) {
			roots = append(roots, entry.Name())
		}
	}
	sort.Strings(roots)
	return roots
}

// edgeChmodTree restores permissions recursively so t.TempDir cleanup succeeds.
// Symlinks are never chmod-ed (that would follow them).
func edgeChmodTree(t *testing.T, root string, mode os.FileMode) {
	t.Helper()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		return os.Chmod(path, mode) //nolint:gosec // test fixture restores deliberately read-only permissions
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("chmod tree %s: %v", root, err)
	}
}

// edgeStageCrash performs the staging primitive without the save, modeling a
// crash between rename-aside and config persistence.
func edgeStageCrash(t *testing.T, m *Manager, name string) stagedRegistryCache {
	t.Helper()

	staged, err := m.stageRegistryCache(name)
	if err != nil {
		t.Fatalf("stageRegistryCache(%q): %v", name, err)
	}
	if staged.staging == "" {
		t.Fatalf("stageRegistryCache(%q) staged nothing", name)
	}
	return staged
}

// edgeDropRegistry removes an entry from the in-memory config and saves, which
// models the post-save state left behind when cleanup failed.
func edgeDropRegistry(t *testing.T, m *Manager, name string) {
	t.Helper()

	kept := make([]Registry, 0, len(m.config.Registries))
	for _, reg := range m.config.Registries {
		if reg.Name != name {
			kept = append(kept, reg)
		}
	}
	m.config = Config{Registries: kept}
	if err := m.save(); err != nil {
		t.Fatalf("saving config without %q: %v", name, err)
	}
}

// TestEdge138CrashAfterStageRestoredByAdd models a crash after the rename-aside
// and proves the next Add restores the same-identity cache.
func TestEdge138CrashAfterStageRestoredByAdd(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	teamFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")
	edgeAge(t, staged.staging)

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other", "third"}); got != teamFile || reg != "team" {
		t.Errorf("crash residue was not restored: got (%q, %q), want (%q, %q)", got, reg, teamFile, "team")
	}
}

// TestEdge138CrashAfterStageRestoredByRemove proves Remove sweeps the residue
// before staging the (now restored) live cache for deletion.
func TestEdge138CrashAfterStageRestoredByRemove(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")
	edgeAge(t, staged.staging)

	if err := m.Remove("team"); err != nil {
		t.Fatalf("Remove team: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if _, err := os.Lstat(filepath.Join(cacheDir, "team")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("live team cache survived removal (sweep ran after staging?): %v", err)
	}
	if got := m.List(); len(got) != 1 || got[0].Name != "other" {
		t.Errorf("config after remove = %v, want [other]", got)
	}
}

// TestEdge138CrashAfterStageRestoredBySetToken proves token rotation (which
// excludes the token from the key) is a recovery point.
func TestEdge138CrashAfterStageRestoredBySetToken(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	teamFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")
	edgeAge(t, staged.staging)

	if err := m.SetToken("team", "rotated"); err != nil {
		t.Fatalf("SetToken: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other"}); got != teamFile || reg != "team" {
		t.Errorf("token-change sweep did not restore cache: got (%q, %q)", got, reg)
	}
	if reg := m.Get("team"); reg == nil || reg.Token != "rotated" {
		t.Errorf("token was not rotated: %+v", reg)
	}
}

// TestEdge138PostSaveResidueSweptByNextAdd models a cleanup failure after the
// save (config no longer lists team) and proves the next mutation deletes it.
func TestEdge138PostSaveResidueSweptByNextAdd(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")
	edgeDropRegistry(t, m, "team")
	edgeAge(t, staged.staging)

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other", "third"}); got != "" || reg != "" {
		t.Errorf("post-save residue was restored: got (%q, %q)", got, reg)
	}
}

// TestEdge138CrossIdentityResidueDeleted proves a source change yields a
// different key, so the old tree is deleted rather than restored.
func TestEdge138CrossIdentityResidueDeleted(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from old url")

	staged := edgeStageCrash(t, m, "team")
	m.config.Registries[0].URL = "https://team.example/v2"
	if err := m.save(); err != nil {
		t.Fatalf("save changed URL: %v", err)
	}
	edgeAge(t, staged.staging)

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other", "third"}); got != "" || reg != "" {
		t.Errorf("cross-identity residue was served: got (%q, %q)", got, reg)
	}
}

// TestEdge138ConfiguredEvictNameSkippedAndGhostDeleted proves a live registry
// named .evict-* is never touched while unattributed keyed residue is deleted.
func TestEdge138ConfiguredEvictNameSkippedAndGhostDeleted(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	if err := m.Add(".evict-team", "https://evict.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add .evict-team: %v", err)
	}
	liveFile := issue138SeedEntry(t, cacheDir, ".evict-team", "go-api", "main.go.tpl", "live evict-named registry")

	ghostName := evictionStagePrefix + "0123456789ab-42"
	ghostDir := filepath.Join(cacheDir, ghostName)
	writeTestFile(t, filepath.Join(ghostDir, "junk.txt"), "ghost")
	edgeAge(t, ghostDir)

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	if _, err := os.Stat(liveFile); err != nil {
		t.Errorf("live .evict-* registry was damaged: %v", err)
	}
	if _, err := os.Lstat(ghostDir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ghost keyed root survived the sweep: %v", err)
	}
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{".evict-team"}); got != liveFile || reg != ".evict-team" {
		t.Errorf("live .evict-* registry no longer resolves: got (%q, %q)", got, reg)
	}
}

// TestEdge138YoungStagingIsSkipped proves the grace period protects in-flight
// staging; the next mutation after aging restores it.
func TestEdge138YoungStagingIsSkipped(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	teamFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	if roots := edgeRoots(t, cacheDir); len(roots) != 1 {
		t.Fatalf("young staging roots = %v, want exactly one", roots)
	}
	if _, err := os.Lstat(filepath.Join(cacheDir, "team")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("young residue was restored before the grace period: %v", err)
	}

	edgeAge(t, staged.staging)
	if err := m.Add("fourth", "https://fourth.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add fourth: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other", "third", "fourth"}); got != teamFile || reg != "team" {
		t.Errorf("aged residue was not restored: got (%q, %q)", got, reg)
	}
}

// TestEdge138SaveAndRestoreFailureRepairedBySweep forces both the save and the
// in-process restore to fail, then proves the next mutation repairs the
// attributable residue.
//
//nolint:cyclop // fixture setup plus one branch per independent failure invariant
func TestEdge138SaveAndRestoreFailureRepairedBySweep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not enforce Unix read-only semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only permissions do not block writes")
	}

	configPath, cacheDir, m := issue138CacheFixture(t)
	teamFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")
	if err := os.Chmod(configPath, 0o400); err != nil {
		t.Fatalf("chmod config read-only: %v", err)
	}
	if err := os.Chmod(cacheDir, 0o500); err != nil { //nolint:gosec // test fixture: deliberately read-only cache root
		t.Fatalf("chmod cache root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(configPath, configFilePerm)
		_ = os.Chmod(cacheDir, 0o750) //nolint:gosec // restore permissions so TempDir cleanup works
	})

	// Force the combined failure directly: the save cannot write the config and
	// the in-process restore cannot rename the staged tree back.
	if saveErr := m.save(); saveErr == nil {
		t.Fatal("save succeeded with a read-only config, want an error")
	}
	if restoreErr := m.restoreRegistryCache(staged); restoreErr == nil {
		t.Fatal("restore succeeded with a read-only cache root, want an error")
	}
	if _, statErr := os.Lstat(staged.staging); statErr != nil {
		t.Fatalf("attributable residue was lost: %v", statErr)
	}
	if got := m.List(); len(got) != 2 || got[0].Name != "team" {
		t.Fatalf("in-memory config changed: %v", got)
	}

	// Recover permissions, age the residue, and let a fresh manager's Add
	// repair the configured registry.
	if err := os.Chmod(configPath, configFilePerm); err != nil {
		t.Fatalf("restore config: %v", err)
	}
	if err := os.Chmod(cacheDir, 0o750); err != nil { //nolint:gosec // restore permissions so TempDir cleanup works
		t.Fatalf("restore cache root: %v", err)
	}
	edgeAge(t, staged.staging)

	m2 := NewManagerWithPaths(configPath, cacheDir)
	if err := m2.load(); err != nil {
		t.Fatalf("reloading config: %v", err)
	}
	if err := m2.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other", "third"}); got != teamFile || reg != "team" {
		t.Errorf("combined-failure residue was not repaired: got (%q, %q), want (%q, %q)", got, reg, teamFile, "team")
	}
}

// TestEdge138LongRegistryNameRoundTrip exercises a 250-byte registry name: the
// live path is long, but the keyed staging root stays fixed-length.
func TestEdge138LongRegistryNameRoundTrip(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "registries.json")
	cacheDir := filepath.Join(dir, "cache")
	m := NewManagerWithPaths(configPath, cacheDir)

	name := strings.Repeat("a", 250)
	if err := m.Add(name, "https://long.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add 250-byte name: %v", err)
	}
	issue138SeedEntry(t, cacheDir, name, "go-api", "main.go.tpl", "long")

	if err := m.Remove(name); err != nil {
		t.Fatalf("Remove 250-byte name: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if _, err := os.Lstat(filepath.Join(cacheDir, name)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("long-name cache survived removal: %v", err)
	}
}

// TestEdge138SymlinkedChildInResidueNotFollowed proves deleting staged residue
// unlinks a child symlink instead of following it out of the cache.
func TestEdge138SymlinkedChildInResidueNotFollowed(t *testing.T) {
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
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	outside := filepath.Join(dir, "outside")
	sentinel := filepath.Join(outside, "keep.txt")
	writeTestFile(t, sentinel, "must survive")
	if err := os.Symlink(outside, filepath.Join(cacheDir, "team", "escape")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	staged := edgeStageCrash(t, m, "team")
	edgeDropRegistry(t, m, "team")
	edgeAge(t, staged.staging)

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if content, err := os.ReadFile(filepath.Clean(sentinel)); err != nil || string(content) != "must survive" {
		t.Errorf("sweep followed a child symlink and touched %s: %v", sentinel, err)
	}
}

// TestEdge138LegacyResidueIsLeftAlone proves unparseable legacy roots are never
// guessed at or deleted.
func TestEdge138LegacyResidueIsLeftAlone(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)

	legacy := []string{evictionStagePrefix + "2784686251", evictionStagePrefix + "stale"}
	for _, name := range legacy {
		path := filepath.Join(cacheDir, name)
		writeTestFile(t, filepath.Join(path, "junk.txt"), "legacy")
		edgeAge(t, path)
	}

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	for _, name := range legacy {
		if _, err := os.Stat(filepath.Join(cacheDir, name, "junk.txt")); err != nil {
			t.Errorf("legacy residue %q was deleted: %v", name, err)
		}
	}
}

// TestEdge138ReAddAfterCrashDoesNotRestoreStale proves a crash residue for a
// removed name is deleted, never restored under a new source.
func TestEdge138ReAddAfterCrashDoesNotRestoreStale(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from old url")

	staged := edgeStageCrash(t, m, "team")
	edgeDropRegistry(t, m, "team")
	edgeAge(t, staged.staging)

	if err := m.Add("team", "https://team.example/v2", "", "", ""); err != nil {
		t.Fatalf("re-add team: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other"}); got != "" || reg != "" {
		t.Errorf("stale cache was restored under a new source: got (%q, %q)", got, reg)
	}
}

// TestEdge138ReAddSameIdentityDoesNotRestoreStagedBytes proves the Add sweep
// runs before the append: re-adding the same name/source must not resurrect
// bytes staged by the earlier removal.
func TestEdge138ReAddSameIdentityDoesNotRestoreStagedBytes(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")
	edgeDropRegistry(t, m, "team")
	edgeAge(t, staged.staging)

	// Same name and URL as the removed registry: the key would match if the
	// append happened before the sweep.
	if err := m.Add("team", "https://team.example/v1", "", "", ""); err != nil {
		t.Fatalf("re-add team: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if got, reg := ResolveCacheEntry(cacheDir, "go-api", "main.go.tpl", []string{"team", "other"}); got != "" || reg != "" {
		t.Errorf("same-identity staged bytes were resurrected by Add: got (%q, %q)", got, reg)
	}
}
