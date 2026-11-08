// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
	"testing"
)

// TestReadOnlyCommandsRunWhileRootLockHeld verifies drift, list, apply
// --dry-run, and template registry commands never take the project mutation
// lock. If any of them did, they would fail with ErrBusy here.
func TestReadOnlyCommandsRunWhileRootLockHeld(t *testing.T) {
	resetFlags()
	t.Cleanup(resetFlags)

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, fileutil.ManifestFileName), nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	lock, err := transaction.AcquireRootLock(root)
	if err != nil {
		t.Fatalf("AcquireRootLock failed: %v", err)
	}
	defer func() { _ = lock.Release() }()

	t.Chdir(root)

	if err := executeCLI("drift"); err != nil {
		t.Errorf("drift while locked failed: %v", err)
	}
	if err := executeCLI("list", "projects"); err != nil {
		t.Errorf("list projects while locked failed: %v", err)
	}
	if err := executeCLI("apply", "--dry-run", "--with", "editorconfig", "."); err != nil {
		t.Errorf("apply --dry-run while locked failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".editorconfig")); !os.IsNotExist(err) {
		t.Errorf("apply --dry-run created .editorconfig (stat err: %v)", err)
	}

	isolateXDGDirs(t)
	if err := executeCLI("template", "registry", "add", "local", "https://example.invalid/templates.zip"); err != nil {
		t.Errorf("template registry add while locked failed: %v", err)
	}
	if err := executeCLI("template", "registry", "list"); err != nil {
		t.Errorf("template registry list while locked failed: %v", err)
	}
}
