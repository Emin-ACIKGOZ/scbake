// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package templateregistry

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// edge138RequireWritableChmod skips on platforms where chmod cannot enforce the
// read-only behavior these tests depend on.
func edge138RequireWritableChmod(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not enforce Unix read-only semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only permissions do not block deletion")
	}
}

// edge138ReadOnlyLiveCacheFixture stages the review's exact probe: a cached
// registry directory made non-writable so the post-save RemoveAll fails. It
// returns the config path, cache dir, manager, and read-only live path.
func edge138ReadOnlyLiveCacheFixture(t *testing.T) (string, string, *Manager, string) {
	t.Helper()

	configPath, cacheDir, m := issue138CacheFixture(t)
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")
	live := filepath.Join(cacheDir, "team")
	if err := os.Chmod(live, 0o555); err != nil { //nolint:gosec // test fixture: deliberately read-only live cache
		t.Fatalf("chmod live cache read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(live, 0o755) //nolint:gosec // restore permissions so TempDir cleanup works
		for _, root := range edgeRoots(t, cacheDir) {
			edgeChmodTree(t, filepath.Join(cacheDir, root), 0o755)
		}
	})
	return configPath, cacheDir, m, live
}

// TestEdge138PostSaveDiscardIsWarning proves the review's exact probe: the
// removal is durable and returns *EvictionWarning (not a failure), and the aged
// residue is reclaimed by the next Add.
//
//nolint:cyclop // assertion-heavy end-to-end warning contract
func TestEdge138PostSaveDiscardIsWarning(t *testing.T) {
	edge138RequireWritableChmod(t)
	configPath, cacheDir, m, live := edge138ReadOnlyLiveCacheFixture(t)

	err := m.Remove("team")
	if err == nil {
		t.Fatal("Remove returned nil, want *EvictionWarning")
	}
	var warning *EvictionWarning
	if !errors.As(err, &warning) {
		t.Fatalf("Remove error = %T %v, want *EvictionWarning", err, err)
	}
	if warning.Registry != "team" {
		t.Errorf("warning.Registry = %q, want team", warning.Registry)
	}
	if !strings.HasPrefix(filepath.Base(warning.Path), evictionStagePrefix) {
		t.Errorf("warning.Path = %q, want a %s* staging path", warning.Path, evictionStagePrefix)
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("errors.Is(err, fs.ErrPermission) = false: %v", err)
	}

	// The config change is durable even though cleanup failed.
	if got := m.List(); len(got) != 1 || got[0].Name != "other" {
		t.Errorf("in-memory config after warning = %v, want [other]", got)
	}
	reloaded := NewManagerWithPaths(configPath, cacheDir)
	if loadErr := reloaded.load(); loadErr != nil {
		t.Fatalf("reloading config: %v", loadErr)
	}
	if got := reloaded.List(); len(got) != 1 || got[0].Name != "other" {
		t.Errorf("persisted config after warning = %v, want [other]", got)
	}
	if _, statErr := os.Lstat(live); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("read-only live cache survived durable removal: %v", statErr)
	}

	roots := edgeRoots(t, cacheDir)
	if len(roots) != 1 {
		t.Fatalf("keyed residue roots = %v, want exactly one", roots)
	}
	if _, ok := parseEvictionRoot(roots[0]); !ok {
		t.Errorf("residue %q is not attributable", roots[0])
	}

	// Age + restore permissions + next Add reclaims the residue.
	root := filepath.Join(cacheDir, roots[0])
	edgeChmodTree(t, root, 0o755)
	edgeAge(t, root)
	if addErr := m.Add("third", "http://127.0.0.1:1", "", "", ""); addErr != nil {
		t.Fatalf("Add third: %v", addErr)
	}
	if roots := edgeRoots(t, cacheDir); len(roots) != 0 {
		t.Errorf("Add did not reclaim aged residue: %v", roots)
	}
}

// TestEdge138ReadOnlyLiveDirRemovalIsDurable isolates the durable-success
// invariant: the registry is gone from the persisted config and the live cache
// path is absent even though the directory was read-only.
func TestEdge138ReadOnlyLiveDirRemovalIsDurable(t *testing.T) {
	edge138RequireWritableChmod(t)
	configPath, _, m, live := edge138ReadOnlyLiveCacheFixture(t)

	err := m.Remove("team")
	var warning *EvictionWarning
	if !errors.As(err, &warning) {
		t.Fatalf("Remove error = %T %v, want *EvictionWarning", err, err)
	}

	data, readErr := os.ReadFile(filepath.Clean(configPath))
	if readErr != nil {
		t.Fatalf("reading persisted config: %v", readErr)
	}
	if strings.Contains(string(data), `"team"`) {
		t.Errorf("persisted config still lists team:\n%s", data)
	}
	if !strings.Contains(string(data), `"other"`) {
		t.Errorf("persisted config lost other:\n%s", data)
	}
	if _, statErr := os.Lstat(live); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("live cache path still exists: %v", statErr)
	}
}

// TestEdge138WarningRetrySweepsAndReportsNotFound proves a retry after the
// warning reclaims the aged residue first and then reports the documented
// "not found" error.
func TestEdge138WarningRetrySweepsAndReportsNotFound(t *testing.T) {
	edge138RequireWritableChmod(t)
	_, cacheDir, m, _ := edge138ReadOnlyLiveCacheFixture(t)

	err := m.Remove("team")
	var warning *EvictionWarning
	if !errors.As(err, &warning) {
		t.Fatalf("Remove error = %T %v, want *EvictionWarning", err, err)
	}

	roots := edgeRoots(t, cacheDir)
	if len(roots) != 1 {
		t.Fatalf("keyed residue roots = %v, want exactly one", roots)
	}
	root := filepath.Join(cacheDir, roots[0])
	edgeChmodTree(t, root, 0o755)
	edgeAge(t, root)

	retryErr := m.Remove("team")
	if retryErr == nil || !strings.Contains(retryErr.Error(), `registry "team" not found`) {
		t.Fatalf("retry error = %v, want registry %q not found", retryErr, "team")
	}
	if roots := edgeRoots(t, cacheDir); len(roots) != 0 {
		t.Errorf("retry did not reclaim aged residue: %v", roots)
	}
}
