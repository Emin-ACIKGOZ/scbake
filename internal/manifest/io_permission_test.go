// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package manifest

import (
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"testing"
)

// TestFindProjectRoot_PermissionErrorDoesNotAdoptAncestor is a regression test
// for SCB-017: an unreadable directory segment is treated as "no marker", and
// the walk must not expose a marker-less hostile ancestor above it.
func TestFindProjectRoot_PermissionErrorDoesNotAdoptAncestor(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions are not enforced")
	}

	base := t.TempDir()
	skipIfAncestorHasMarker(t, base)

	hostileDir := filepath.Join(base, "hostile")
	lockedDir := filepath.Join(hostileDir, "locked")
	victimDir := filepath.Join(lockedDir, "victim")
	if err := os.MkdirAll(victimDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, hostileDir, "[metadata]\nlicense = \"EVIL\"\n")

	if err := os.Chmod(lockedDir, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, fileutil.DirPerms) })

	found, err := FindProjectRoot(victimDir)
	if err != nil {
		t.Fatalf("FindProjectRoot failed: %v", err)
	}
	if found != victimDir {
		t.Errorf("hostile ancestor adopted through unreadable directory. Want %s, Got %s", victimDir, found)
	}

	// Load must fail closed: it cannot read the start directory, and it must
	// not silently return the hostile ancestor manifest.
	m, _, err := Load(victimDir)
	if err == nil {
		t.Fatalf("Load() succeeded past an unreadable directory with metadata %v", m.Metadata)
	}
}
