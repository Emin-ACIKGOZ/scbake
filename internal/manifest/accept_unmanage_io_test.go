// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"

	"scbake/internal/types"
)

// TestSaveLoadRoundTripAcceptedUnmanaged pins the additive TOML schema: both
// fields survive an encode/decode cycle unchanged and are emitted as expected.
func TestSaveLoadRoundTripAcceptedUnmanaged(t *testing.T) {
	root := t.TempDir()
	original := &types.Manifest{
		SbakeVersion: DefaultSbakeVersion,
		ManagedFiles: map[string]string{
			"main.go":              "pristine-main",
			"services/api/main.go": "pristine-api",
			".gitignore":           "ignore",
		},
		AcceptedFiles: map[string]string{
			"main.go":              "accepted-main",
			"services/api/main.go": "accepted-api",
		},
		UnmanagedFiles: []string{"services/api/LICENSE"},
	}
	if err := Save(original, root); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(root, "scbake.toml")) //nolint:gosec // test-controlled fixture path under t.TempDir()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	for _, want := range []string{
		"[accepted_files]",
		`"main.go" = "accepted-main"`,
		`"services/api/main.go" = "accepted-api"`,
		`unmanaged_files = ["services/api/LICENSE"]`,
	} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("saved manifest missing %q:\n%s", want, raw)
		}
	}

	loaded, _, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.AcceptedFiles, original.AcceptedFiles) {
		t.Errorf("accepted round-trip = %v, want %v", loaded.AcceptedFiles, original.AcceptedFiles)
	}
	if !reflect.DeepEqual(loaded.UnmanagedFiles, original.UnmanagedFiles) {
		t.Errorf("unmanaged round-trip = %v, want %v", loaded.UnmanagedFiles, original.UnmanagedFiles)
	}
	if !reflect.DeepEqual(loaded.ManagedFiles, original.ManagedFiles) {
		t.Errorf("managed round-trip changed: %v", loaded.ManagedFiles)
	}
}

// TestSaveOmitsEmptyAdditiveFields pins omitempty: a manifest with no accepted
// or unmanaged state does not grow the new tables.
func TestSaveOmitsEmptyAdditiveFields(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{
		SbakeVersion: DefaultSbakeVersion,
		ManagedFiles: map[string]string{"main.go": "hash"},
	}
	if err := Save(m, root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "scbake.toml")) //nolint:gosec // test-controlled fixture path under t.TempDir()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	for _, unwanted := range []string{"accepted_files", "unmanaged_files"} {
		if strings.Contains(string(raw), unwanted) {
			t.Errorf("empty manifest emitted %q:\n%s", unwanted, raw)
		}
	}
}

// TestSavedAdditiveFieldsLoadInLegacyReader models the downgrade hazard: an
// older reader ignores the new fields and still loads the managed ledger. The
// new state is not honored by old binaries (documented), but the file stays
// decodable.
func TestSavedAdditiveFieldsLoadInLegacyReader(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{
		SbakeVersion:  DefaultSbakeVersion,
		ManagedFiles:  map[string]string{"main.go": "pristine"},
		AcceptedFiles: map[string]string{"main.go": "accepted"},
		UnmanagedFiles: []string{
			"detached.txt",
		},
	}
	if err := Save(m, root); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "scbake.toml")) //nolint:gosec // test-controlled fixture path under t.TempDir()
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}

	var legacy struct {
		SbakeVersion string            `toml:"scbake_version"`
		ManagedFiles map[string]string `toml:"managed_files"`
	}
	if _, err := toml.Decode(string(raw), &legacy); err != nil {
		t.Fatalf("legacy reader rejected the manifest: %v", err)
	}
	if legacy.ManagedFiles["main.go"] != "pristine" {
		t.Errorf("legacy reader missed the managed ledger: %v", legacy.ManagedFiles)
	}
}

// TestLoadAcceptedAndUnmanagedFromHandWrittenManifest verifies forward
// compatibility: a hand-written manifest using both fields is accepted.
func TestLoadAcceptedAndUnmanagedFromHandWrittenManifest(t *testing.T) {
	root := t.TempDir()
	body := `scbake_version = "v0.3.0"
unmanaged_files = ["old.txt"]

[managed_files]
  "main.go" = "pristine"

[accepted_files]
  "main.go" = "accepted"
`
	if err := os.WriteFile(filepath.Join(root, "scbake.toml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	loaded, _, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AcceptedFiles["main.go"] != "accepted" {
		t.Errorf("accepted map = %v", loaded.AcceptedFiles)
	}
	if !reflect.DeepEqual(loaded.UnmanagedFiles, []string{"old.txt"}) {
		t.Errorf("unmanaged list = %v", loaded.UnmanagedFiles)
	}
}
