// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// issue138ChmodTree restores permissions recursively; symlinks are skipped so
// the helper never follows one.
func issue138ChmodTree(t *testing.T, root string, mode os.FileMode) {
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

// issue138EvictionRoots lists .evict-* entries under the template cache root.
func issue138EvictionRoots(t *testing.T, cacheRoot string) []string {
	t.Helper()

	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		t.Fatalf("reading cache root %s: %v", cacheRoot, err)
	}
	roots := make([]string, 0)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".evict-") {
			roots = append(roots, entry.Name())
		}
	}
	return roots
}

// TestIssue138EdgePostSaveWarningExitsZero is the review's exact CLI probe: a
// read-only live cache makes cleanup fail after the save. The command must warn
// on stderr, print success on stdout, exit 0, persist the removal, and reclaim
// the aged residue on the next registry mutation.
//
//nolint:cyclop // one branch per independent CLI/DURABLE-state invariant
func TestIssue138EdgePostSaveWarningExitsZero(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows chmod does not enforce Unix read-only semantics")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: read-only permissions do not block deletion")
	}

	_ = issue138Setup(t)
	srv := issue138RegistryServer(t)
	issue138MustRun(t, "template", "registry", "add", "team", srv.URL)
	issue138MustRun(t, "template", "registry", "add", "other", "http://127.0.0.1:1")
	issue138MustRun(t, "template", "pull", "editorconfig", "--registry", "team")

	cacheRoot := filepath.Join(userCacheDir(t), "scbake", "templates")
	teamCache := issue138RegistryCacheDir(t, "team")
	if err := os.Chmod(teamCache, 0o555); err != nil { //nolint:gosec // test fixture: deliberately read-only live cache
		t.Fatalf("chmod team cache read-only: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(teamCache, 0o755) //nolint:gosec // restore permissions so temp dirs clean up
		for _, root := range issue138EvictionRoots(t, cacheRoot) {
			issue138ChmodTree(t, filepath.Join(cacheRoot, root), 0o755)
		}
	})

	stdout, stderr, err := issue138RunCLISplit(t, "template", "registry", "remove", "team")
	if err != nil {
		t.Fatalf("remove exited non-zero after a durable save: %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	for _, want := range []string{"Warning:", "team", ".evict-", cacheRoot} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr does not contain %q:\n%s", want, stderr)
		}
	}
	if !strings.Contains(stdout, `Removed registry "team"`) {
		t.Errorf("stdout does not contain the success line:\n%s", stdout)
	}

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("cannot determine user config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "scbake", "registries.json")
	config, err := os.ReadFile(filepath.Clean(configPath))
	if err != nil {
		t.Fatalf("reading persisted config: %v", err)
	}
	if strings.Contains(string(config), `"team"`) {
		t.Errorf("persisted config still lists team:\n%s", config)
	}
	if !strings.Contains(string(config), `"other"`) {
		t.Errorf("persisted config lost other:\n%s", config)
	}
	if _, statErr := os.Lstat(teamCache); !errors.Is(statErr, fs.ErrNotExist) {
		t.Errorf("live team cache still exists: %v", statErr)
	}

	roots := issue138EvictionRoots(t, cacheRoot)
	if len(roots) != 1 {
		t.Fatalf("keyed residue roots = %v, want exactly one", roots)
	}
	root := filepath.Join(cacheRoot, roots[0])
	issue138ChmodTree(t, root, 0o755)
	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(root, old, old); err != nil {
		t.Fatalf("aging residue: %v", err)
	}

	issue138MustRun(t, "template", "registry", "add", "third", "http://127.0.0.1:1")
	if roots := issue138EvictionRoots(t, cacheRoot); len(roots) != 0 {
		t.Errorf("next add did not reclaim aged residue: %v", roots)
	}
}
