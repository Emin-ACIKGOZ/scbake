// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package core

import (
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"testing"
)

// saveNewManifest runs persistManifestState on a root with no pre-existing
// manifest, then returns the transaction and the saved path.
func saveNewManifest(t *testing.T) (*transaction.Manager, string) {
	t.Helper()

	root := t.TempDir()
	m, rootPath, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("manifest.Load: %v", err)
	}
	manifestPath := filepath.Join(rootPath, fileutil.ManifestFileName)
	if _, statErr := os.Stat(manifestPath); !os.IsNotExist(statErr) {
		t.Fatalf("test root unexpectedly has a manifest: %v", statErr)
	}

	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	tc := types.TaskContext{Manifest: m, Tx: tx}
	if err := persistManifestState(m, tc, &manifestChanges{}, rootPath, tx); err != nil {
		t.Fatalf("persistManifestState: %v", err)
	}
	if _, statErr := os.Stat(manifestPath); statErr != nil {
		t.Fatalf("manifest was not saved: %v", statErr)
	}
	return tx, manifestPath
}

// TestPersistManifestState_SnapshotsCreatedManifest verifies that a manifest
// created by the transaction is content-verified before rollback removes it.
func TestPersistManifestState_SnapshotsCreatedManifest(t *testing.T) {
	t.Run("unchanged manifest is removed", func(t *testing.T) {
		tx, manifestPath := saveNewManifest(t)

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback failed: %v", err)
		}
		if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
			t.Errorf("rollback left the unchanged created manifest: %v", err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(manifestPath), fileutil.InternalDir)); !os.IsNotExist(err) {
			t.Errorf("rollback left transaction state: %v", err)
		}
	})

	t.Run("externally modified manifest is kept", func(t *testing.T) {
		tx, manifestPath := saveNewManifest(t)

		data, err := os.ReadFile(manifestPath) //nolint:gosec // test-controlled path
		if err != nil {
			t.Fatal(err)
		}
		const external = "# external edit\n"
		if err := os.WriteFile(manifestPath, append(data, []byte(external)...), fileutil.FilePerms); err != nil { //nolint:gosec // test-controlled manifest path
			t.Fatal(err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback treated a kept manifest as an error: %v", err)
		}
		kept, err := os.ReadFile(manifestPath) //nolint:gosec // test-controlled path
		if err != nil {
			t.Fatalf("rollback removed the externally modified manifest: %v", err)
		}
		if string(kept) != string(data)+external {
			t.Errorf("kept manifest content changed: got %q", kept)
		}
	})
}
