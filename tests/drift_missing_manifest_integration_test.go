// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// requireNonZeroExit fails unless err is an exec.ExitError with a non-zero
// status, so a test cannot mistake a successful command for a CI gate.
func requireNonZeroExit(t *testing.T, out string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a non-zero exit, got success:\n%s", out)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an exit error, got %T: %v\n%s", err, err, out)
	}
	if exitErr.ExitCode() == 0 {
		t.Fatalf("expected a non-zero exit code, got 0:\n%s", out)
	}
}

// TestDriftWithoutManifestFailsClosed is the regression test for issue #149: an
// empty directory has no generated tree to compare, so drift must fail instead
// of reporting a clean result and letting a CI gate pass.
func TestDriftWithoutManifestFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	skipIfAncestorHasMarker(t, tmpDir)
	t.Chdir(tmpDir)

	out, err := runCLI("drift")
	requireNonZeroExit(t, out, err)
	if !strings.Contains(out, "scbake.toml") {
		t.Errorf("drift did not report the missing manifest: %s", out)
	}
}

// TestDriftNonexistentPathFailsClosed covers a mistyped or stale CI path: the
// missing directory must produce an error, not a clean scan.
func TestDriftNonexistentPathFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	skipIfAncestorHasMarker(t, tmpDir)
	missingPath := filepath.Join(tmpDir, "no-such-project")

	out, err := runCLI("drift", missingPath)
	requireNonZeroExit(t, out, err)
	if !strings.Contains(out, "scbake.toml") {
		t.Errorf("drift did not report the missing manifest: %s", out)
	}
}

// TestDriftManifestWithoutManagedFilesIsClean guards against over-failing: a
// real project whose manifest records no managed files is legitimately clean.
func TestDriftManifestWithoutManagedFilesIsClean(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)
	mustWrite(t, filepath.Join(tmpDir, "scbake.toml"), "scbake_version = \"v0.3.0\"\n\n[managed_files]\n")

	out, err := runCLI("drift")
	if err != nil {
		t.Fatalf("drift on a manifest with no managed files failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No drift detected.") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestDriftModifiedManagedFileStillFails guards against over-failing: adding
// the manifest existence check must not hide real drift in an existing project.
func TestDriftModifiedManagedFileStillFails(t *testing.T) {
	tmpDir := t.TempDir()
	t.Chdir(tmpDir)

	pristine := []byte("generated\n")
	hash := fmt.Sprintf("%x", sha256.Sum256(pristine))
	mustWrite(t, filepath.Join(tmpDir, "Makefile"), string(pristine))
	mustWrite(t, filepath.Join(tmpDir, "scbake.toml"),
		fmt.Sprintf("scbake_version = \"v0.3.0\"\n\n[managed_files]\n\"Makefile\" = %q\n", hash))
	mustWrite(t, filepath.Join(tmpDir, "Makefile"), "locally edited\n")

	out, err := runCLI("drift")
	requireNonZeroExit(t, out, err)
	for _, want := range []string{"Drift detected", "modified", "Makefile"} {
		if !strings.Contains(out, want) {
			t.Errorf("drift output missing %q: %s", want, out)
		}
	}
}

// TestDriftRepairWithoutManifestStillFails pins the unchanged --repair behavior
// for issue #149: repair cannot restore a project that was never scaffolded.
func TestDriftRepairWithoutManifestStillFails(t *testing.T) {
	tmpDir := t.TempDir()
	skipIfAncestorHasMarker(t, tmpDir)
	t.Chdir(tmpDir)

	out, err := runCLI("drift", "--repair")
	requireNonZeroExit(t, out, err)
	if !strings.Contains(out, "scbake.toml") {
		t.Errorf("drift --repair did not report the missing manifest: %s", out)
	}
	if _, statErr := os.Stat(filepath.Join(tmpDir, "scbake.toml")); !os.IsNotExist(statErr) {
		t.Errorf("drift --repair created a manifest; stat err = %v", statErr)
	}
}
