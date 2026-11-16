// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"

	"github.com/BurntSushi/toml"
)

// The absent-at-load contract is part of the package's exported behavior, not
// its API surface. These assignments fail to compile if a signature changes.
var (
	_ func(string) (*types.Manifest, string, error)                   = Load
	_ func(string, DiscoveryOptions) (*types.Manifest, string, error) = LoadWithOptions
	_ func(*types.Manifest, string) error                             = Save
	_                                                                 = DefaultSbakeVersion
)

// manifestPathFor returns the scbake.toml path inside dir.
func manifestPathFor(dir string) string {
	return filepath.Join(dir, fileutil.ManifestFileName)
}

// readManifestFor reads the raw scbake.toml bytes inside dir.
func readManifestFor(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(manifestPathFor(dir))
	if err != nil {
		t.Fatalf("failed to read manifest: %v", err)
	}
	return string(data)
}

// modTimeOf returns the current modification time of path.
func modTimeOf(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat %s: %v", path, err)
	}
	return info.ModTime()
}

// setModTime forces a distinct modification time so conflict detection does not
// depend on the filesystem's mtime resolution.
func setModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatalf("failed to set modification time on %s: %v", path, err)
	}
}

// requireNoTempManifests asserts Save left no scbake.toml.*.tmp file behind.
func requireNoTempManifests(t *testing.T, dir string) {
	t.Helper()
	leftovers, err := filepath.Glob(filepath.Join(dir, fileutil.ManifestFileName+".*.tmp"))
	if err != nil {
		t.Fatalf("glob temp manifest files failed: %v", err)
	}
	if len(leftovers) != 0 {
		t.Errorf("temp manifest files were not cleaned up: %v", leftovers)
	}
}

// requireConflict fails unless err reports a manifest conflict.
func requireConflict(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("Save() succeeded, want a manifest conflict")
	}
	if !strings.Contains(err.Error(), "manifest conflict") {
		t.Fatalf("Save() error = %q, want a manifest conflict", err)
	}
}

// TestSave_ConflictWhenManifestAppearsAfterAbsentLoad covers the core issue:
// Load observed no manifest, so a file that appears before Save was written by
// another process and must not be silently overwritten. The table varies the
// external writer's technique to prove the check keys off existence rather
// than how the file was created.
func TestSave_ConflictWhenManifestAppearsAfterAbsentLoad(t *testing.T) { //nolint:cyclop // assertion-heavy table regression test
	const externalBody = "[metadata]\nlicense = \"EXTERNAL\"\n"

	tests := []struct {
		name   string
		create func(t *testing.T, dir string)
	}{
		{
			name: "external creates empty file",
			create: func(t *testing.T, dir string) {
				t.Helper()
				writeManifestFile(t, dir, "")
			},
		},
		{
			name: "external creates populated manifest",
			create: func(t *testing.T, dir string) {
				t.Helper()
				writeManifestFile(t, dir, externalBody)
			},
		},
		{
			name: "external atomic replace",
			create: func(t *testing.T, dir string) {
				t.Helper()
				tmp := filepath.Join(dir, "external.toml.tmp")
				if err := os.WriteFile(tmp, []byte(externalBody), fileutil.PrivateFilePerms); err != nil {
					t.Fatalf("failed to write external temp manifest: %v", err)
				}
				if err := os.Rename(tmp, manifestPathFor(dir)); err != nil {
					t.Fatalf("failed to rename external manifest: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			m, root, err := Load(dir)
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}
			if root != dir {
				t.Fatalf("Load() root = %s, want %s", root, dir)
			}
			if _, statErr := os.Stat(manifestPathFor(dir)); !os.IsNotExist(statErr) {
				t.Fatalf("precondition failed: manifest already exists (stat err = %v)", statErr)
			}

			tt.create(t, dir)
			before := readManifestFor(t, dir)

			m.Metadata = map[string]string{"license": "OURS"}
			err = Save(m, root)
			requireConflict(t, err)

			if after := readManifestFor(t, dir); after != before {
				t.Errorf("Save() overwrote the externally created manifest.\nwant %q\ngot  %q", before, after)
			}
			requireNoTempManifests(t, dir)

			// Recovery: reloading accepts the external state, so a subsequent
			// Save in this process is no longer blocked.
			reloaded, reloadRoot, err := Load(root)
			if err != nil {
				t.Fatalf("Load() after conflict failed: %v", err)
			}
			reloaded.Metadata = map[string]string{"license": "OURS"}
			if err := Save(reloaded, reloadRoot); err != nil {
				t.Fatalf("Save() after reload failed: %v", err)
			}
			if got := readManifestFor(t, dir); !strings.Contains(got, `license = "OURS"`) {
				t.Errorf("reloaded Save did not persist its content: %q", got)
			}
		})
	}
}

// TestSave_NoFalseConflictOnRepeatedSaveAfterAbsentLoad covers the counterpart:
// after the first Save creates the manifest, the fingerprint must be refreshed
// so the same process can save again without a spurious conflict.
func TestSave_NoFalseConflictOnRepeatedSaveAfterAbsentLoad(t *testing.T) {
	tests := []struct {
		name string
		save func(t *testing.T, m *types.Manifest, root string)
	}{
		{
			name: "unchanged manifest saved twice",
			save: func(t *testing.T, m *types.Manifest, root string) {
				t.Helper()
				if err := Save(m, root); err != nil {
					t.Fatalf("first Save() after absent Load failed: %v", err)
				}
				if err := Save(m, root); err != nil {
					t.Fatalf("second Save() after absent Load reported a false conflict: %v", err)
				}
			},
		},
		{
			name: "manifest mutated between saves",
			save: func(t *testing.T, m *types.Manifest, root string) {
				t.Helper()
				if err := Save(m, root); err != nil {
					t.Fatalf("first Save() after absent Load failed: %v", err)
				}
				m.Projects = append(m.Projects, types.Project{Name: "api", Path: "api", Language: "go"})
				if err := Save(m, root); err != nil {
					t.Fatalf("second Save() after absent Load reported a false conflict: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()

			m, root, err := Load(dir)
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}

			tt.save(t, m, root)
			requireNoTempManifests(t, dir)

			reloaded, _, err := Load(root)
			if err != nil {
				t.Fatalf("Load() after saves failed: %v", err)
			}
			if len(reloaded.Projects) != len(m.Projects) {
				t.Errorf("round-trip projects = %d, want %d", len(reloaded.Projects), len(m.Projects))
			}
		})
	}
}

// TestSave_NoFalseConflictOnRepeatedSaveAfterExistingLoad pins the existing
// manifest path: loading a file then saving twice must not trip the conflict
// check, including when a reload separates the two saves.
func TestSave_NoFalseConflictOnRepeatedSaveAfterExistingLoad(t *testing.T) { //nolint:cyclop // assertion-heavy table regression test
	const initialBody = "[metadata]\nlicense = \"INITIAL\"\n"

	tests := []struct {
		name string
		save func(t *testing.T, m *types.Manifest, root string)
	}{
		{
			name: "unchanged manifest saved twice",
			save: func(t *testing.T, m *types.Manifest, root string) {
				t.Helper()
				if err := Save(m, root); err != nil {
					t.Fatalf("first Save() after existing Load failed: %v", err)
				}
				if err := Save(m, root); err != nil {
					t.Fatalf("second Save() after existing Load reported a false conflict: %v", err)
				}
			},
		},
		{
			name: "manifest mutated between saves",
			save: func(t *testing.T, m *types.Manifest, root string) {
				t.Helper()
				if err := Save(m, root); err != nil {
					t.Fatalf("first Save() after existing Load failed: %v", err)
				}
				m.Projects = append(m.Projects, types.Project{Name: "web", Path: "web", Language: "ts"})
				if err := Save(m, root); err != nil {
					t.Fatalf("second Save() after existing Load reported a false conflict: %v", err)
				}
			},
		},
		{
			name: "reload between saves",
			save: func(t *testing.T, m *types.Manifest, root string) {
				t.Helper()
				if err := Save(m, root); err != nil {
					t.Fatalf("first Save() after existing Load failed: %v", err)
				}
				m2, root2, err := Load(root)
				if err != nil {
					t.Fatalf("reload failed: %v", err)
				}
				if err := Save(m2, root2); err != nil {
					t.Fatalf("Save() after reload reported a false conflict: %v", err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifestFile(t, dir, initialBody)

			m, root, err := Load(dir)
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}
			if m.Metadata["license"] != "INITIAL" {
				t.Fatalf("Load() metadata = %v, want license=INITIAL", m.Metadata)
			}

			tt.save(t, m, root)
			requireNoTempManifests(t, dir)
		})
	}
}

// TestSave_ConflictWhenManifestChangedAfterLoad covers the pre-existing
// detected case, including an mtime-only change and an atomic external replace.
// Modification times are set explicitly so the result is independent of the
// filesystem's timestamp resolution.
func TestSave_ConflictWhenManifestChangedAfterLoad(t *testing.T) {
	const (
		initialBody  = "[metadata]\nlicense = \"INITIAL\"\n"
		externalBody = "[metadata]\nlicense = \"EXTERNAL\"\n"
	)

	tests := []struct {
		name string
		// mutate changes the manifest after Load and returns the content the
		// failed Save must leave untouched.
		mutate func(t *testing.T, dir string, loadedModTime time.Time) string
	}{
		{
			name: "content and mtime change",
			mutate: func(t *testing.T, dir string, loadedModTime time.Time) string {
				t.Helper()
				writeManifestFile(t, dir, externalBody)
				setModTime(t, manifestPathFor(dir), loadedModTime.Add(-time.Hour))
				return externalBody
			},
		},
		{
			name: "mtime only change",
			mutate: func(t *testing.T, dir string, loadedModTime time.Time) string {
				t.Helper()
				setModTime(t, manifestPathFor(dir), loadedModTime.Add(-time.Hour))
				return initialBody
			},
		},
		{
			name: "atomic external replace",
			mutate: func(t *testing.T, dir string, loadedModTime time.Time) string {
				t.Helper()
				tmp := filepath.Join(dir, "external.toml.tmp")
				if err := os.WriteFile(tmp, []byte(externalBody), fileutil.PrivateFilePerms); err != nil {
					t.Fatalf("failed to write external temp manifest: %v", err)
				}
				setModTime(t, tmp, loadedModTime.Add(-time.Hour))
				if err := os.Rename(tmp, manifestPathFor(dir)); err != nil {
					t.Fatalf("failed to rename external manifest: %v", err)
				}
				return externalBody
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifestFile(t, dir, initialBody)

			m, root, err := Load(dir)
			if err != nil {
				t.Fatalf("Load() failed: %v", err)
			}
			loadedModTime := modTimeOf(t, manifestPathFor(dir))
			wantContent := tt.mutate(t, dir, loadedModTime)

			m.Metadata = map[string]string{"license": "OURS"}
			err = Save(m, root)
			requireConflict(t, err)

			if got := readManifestFor(t, dir); got != wantContent {
				t.Errorf("Save() overwrote the externally modified manifest.\nwant %q\ngot  %q", wantContent, got)
			}
			requireNoTempManifests(t, dir)
		})
	}
}

// TestManifestLoadSaveRoundTrip covers the healthy first-save path and pins the
// on-disk schema: no fingerprint bookkeeping is serialized and every field of
// types.Manifest survives a Save/Load cycle unchanged.
func TestManifestLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()

	_, root, err := Load(dir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	want := &types.Manifest{
		SbakeVersion: DefaultSbakeVersion,
		Projects: []types.Project{{
			Name:      "api",
			Path:      "services/api",
			Language:  "go",
			Templates: []string{"go-cli", "license"},
		}},
		Templates: []types.Template{{Name: "license", Path: "LICENSE"}},
		Metadata:  map[string]string{"license": "Apache-2.0"},
		ManagedFiles: map[string]string{
			"LICENSE": "5f4dcc3b5aa765d61d8327deb882cf99",
		},
	}
	if err := Save(want, root); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}
	requireNoTempManifests(t, dir)

	got, gotRoot, err := Load(root)
	if err != nil {
		t.Fatalf("Load() after Save failed: %v", err)
	}
	if gotRoot != root {
		t.Errorf("Load() root = %s, want %s", gotRoot, root)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip manifest mismatch.\nwant %#v\ngot  %#v", want, got)
	}

	// The persisted schema must not grow optimistic-locking fields.
	var raw map[string]any
	if _, err := toml.Decode(readManifestFor(t, dir), &raw); err != nil {
		t.Fatalf("failed to decode written manifest: %v", err)
	}
	allowed := map[string]bool{
		"scbake_version": true,
		"projects":       true,
		"templates":      true,
		"metadata":       true,
		"managed_files":  true,
	}
	for key := range raw {
		if !allowed[key] {
			t.Errorf("unexpected top-level key %q written to scbake.toml", key)
		}
	}
}
