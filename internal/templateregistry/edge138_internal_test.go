// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package templateregistry

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEdge138EvictionKeyPinned pins the length-prefixed field order and 12-hex
// truncation: roots already on disk depend on this exact encoding.
func TestEdge138EvictionKeyPinned(t *testing.T) {
	reg := Registry{Name: "team", URL: "https://team.example/v1"}
	const want = "33394598daa4"
	if got := evictionKey(reg); got != want {
		t.Errorf("evictionKey = %q, want %q (field-order stability contract)", got, want)
	}
	if len(evictionKey(reg)) != evictionKeyHexLen {
		t.Errorf("evictionKey length = %d, want %d", len(evictionKey(reg)), evictionKeyHexLen)
	}
	if !isLowerHex(evictionKey(reg)) {
		t.Errorf("evictionKey is not lowercase hex: %q", evictionKey(reg))
	}
}

// TestEdge138EvictionKeyFields proves the key binds name and source fields and
// deliberately excludes the token.
func TestEdge138EvictionKeyFields(t *testing.T) {
	base := Registry{
		Name:         "team",
		URL:          "https://team.example/v1",
		Token:        "first-token",
		Version:      "v1",
		Subdirectory: "sub",
	}
	baseKey := evictionKey(base)

	tokenRotated := base
	tokenRotated.Token = "second-token"
	if got := evictionKey(tokenRotated); got != baseKey {
		t.Errorf("token changed the key: %q != %q", got, baseKey)
	}

	variants := map[string]func(Registry) Registry{
		"name": func(r Registry) Registry { r.Name = "other"; return r },
		"url":  func(r Registry) Registry { r.URL = "https://team.example/v2"; return r },
		"version": func(r Registry) Registry {
			r.Version = "v2"
			return r
		},
		"subdirectory": func(r Registry) Registry { r.Subdirectory = "other"; return r },
	}
	for field, mutate := range variants {
		t.Run(field, func(t *testing.T) {
			if got := evictionKey(mutate(base)); got == baseKey {
				t.Errorf("%s is not part of the key: %q", field, got)
			}
		})
	}
}

// TestEdge138ParseEvictionRoot pins which basenames are attributable. Legacy
// random roots and malformed names must be rejected (never guessed at).
func TestEdge138ParseEvictionRoot(t *testing.T) {
	valid := map[string]string{
		".evict-0123456789ab-1":          "0123456789ab",
		".evict-abcdef012345-0":          "abcdef012345",
		".evict-abcdef012345-1234567890": "abcdef012345",
	}
	for base, wantKey := range valid {
		key, ok := parseEvictionRoot(base)
		if !ok || key != wantKey {
			t.Errorf("parseEvictionRoot(%q) = (%q, %v), want (%q, true)", base, key, ok, wantKey)
		}
	}

	invalid := []string{
		"",
		".evict-",
		".evict-stale",
		".evict-2784686251", // legacy random root
		".evict-0123456789ab",
		".evict-0123456789ab-",
		".evict-0123456789ab-12a",
		".evict-0123456789ab-1-2",
		".evict-0123456789AB-1",
		".evict-0123456789a-1",
		".evict-0123456789abc-1",
		".evict_0123456789ab-1",
		".evict-0123456789ag-1",
		".EVICT-0123456789ab-1",
		".evict-0123456789ab- 1",
		"evict-0123456789ab-1",
	}
	for _, base := range invalid {
		if key, ok := parseEvictionRoot(base); ok || key != "" {
			t.Errorf("parseEvictionRoot(%q) = (%q, %v), want rejected", base, key, ok)
		}
	}
}

// TestEdge138SymlinkedStagingRootUnlinkedNotFollowed drives the sweep with an
// advanced clock so a symlinked root passes the grace check without chmod-ing
// (which would follow) the link. The target must survive.
func TestEdge138SymlinkedStagingRootUnlinkedNotFollowed(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		t.Fatalf("MkdirAll cache: %v", err)
	}

	outside := filepath.Join(dir, "outside")
	sentinel := filepath.Join(outside, "keep.txt")
	writeTestFile(t, sentinel, "must survive")

	root := filepath.Join(cacheDir, evictionStagePrefix+"0123456789ab-77")
	if err := os.Symlink(outside, root); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}

	m := NewManagerWithPaths(filepath.Join(dir, "registries.json"), cacheDir)
	m.sweepEvictedCaches(time.Now().Add(2 * evictionSweepGrace))

	if _, err := os.Lstat(root); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlinked staging root was not unlinked: %v", err)
	}
	if content, err := os.ReadFile(filepath.Clean(sentinel)); err != nil || string(content) != "must survive" {
		t.Errorf("sweep followed the staging-root symlink and touched %s: %v", sentinel, err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("symlink target directory was removed: %v", err)
	}
}

// TestEdge138ConfiguredKeyResidueNotRestoredWhenLiveExists proves the live path
// wins: a matching key with a live directory is deleted, not overwritten.
func TestEdge138ConfiguredKeyResidueNotRestoredWhenLiveExists(t *testing.T) {
	_, cacheDir, m := issue138CacheFixture(t)
	issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "from team")

	staged := edgeStageCrash(t, m, "team")
	// Recreate a live cache (as a concurrent pull would) before the sweep.
	liveFile := issue138SeedEntry(t, cacheDir, "team", "go-api", "main.go.tpl", "newer live copy")
	edgeAge(t, staged.staging)

	if err := m.Add("third", "https://third.example/v1", "", "", ""); err != nil {
		t.Fatalf("Add third: %v", err)
	}
	assertNoEvictionStaging(t, cacheDir)
	if content, err := os.ReadFile(filepath.Clean(liveFile)); err != nil || string(content) != "newer live copy" {
		t.Errorf("live cache was overwritten by restored residue: %q, %v", content, err)
	}
}
