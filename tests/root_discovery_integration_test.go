// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// skipIfAncestorHasMarker keeps the marker-less fixture meaningful when TMPDIR
// itself lives inside a project (for example TMPDIR=$PWD/.tmp). Such an
// environment cannot represent "no .git anywhere above"; skip with a clear
// reason instead of asserting the wrong policy. It checks the ancestors of
// fixtureRoot (not fixtureRoot itself).
func skipIfAncestorHasMarker(tb testing.TB, fixtureRoot string) {
	tb.Helper()
	for dir := filepath.Dir(fixtureRoot); ; {
		for _, name := range []string{fileutil.ManifestFileName, fileutil.GitDir} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				tb.Skipf("fixture root %q is inside a project (%s); cannot build a marker-less fixture",
					fixtureRoot, filepath.Join(dir, name))
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return
		}
		dir = parent
	}
}

// setupMarkerlessMonorepo creates a non-Git central manifest with the given
// body and returns the monorepo root. The caller must build subdirectories
// without markers and chdir into them to exercise marker-less discovery.
func setupMarkerlessMonorepo(t *testing.T, manifestBody string) string {
	t.Helper()
	tmpDir := t.TempDir()
	skipIfAncestorHasMarker(t, tmpDir)

	monoDir := filepath.Join(tmpDir, "mono")
	if err := os.MkdirAll(monoDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	centralManifest := filepath.Join(monoDir, fileutil.ManifestFileName)
	if err := os.WriteFile(centralManifest, []byte(manifestBody), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	return monoDir
}

// makeSubdir creates parent/name and returns the new directory.
func makeSubdir(t *testing.T, parent, name string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestMarkerlessApplyDefaultCreatesLocalManifest is the end-to-end regression
// test for SCB-017: a non-Git central manifest must not be adopted by default.
func TestMarkerlessApplyDefaultCreatesLocalManifest(t *testing.T) {
	monoDir := setupMarkerlessMonorepo(t, "")
	defaultDir := makeSubdir(t, monoDir, "default")
	centralManifest := filepath.Join(monoDir, fileutil.ManifestFileName)
	t.Chdir(monoDir)

	if out, err := runCLI("apply", "--with", "makefile", "default"); err != nil {
		t.Fatalf("apply failed: %v\nOutput: %s", err, out)
	}

	if _, err := os.Stat(filepath.Join(defaultDir, fileutil.ManifestFileName)); err != nil {
		t.Errorf("default apply should create a manifest in the start directory: %v", err)
	}

	central, err := os.ReadFile(centralManifest) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("reading central manifest failed: %v", err)
	}
	if len(central) != 0 {
		t.Errorf("central manifest was modified by a marker-less apply: %q", central)
	}
}

// TestMarkerlessApplyOptInUsesCentralManifest proves the flag restores the
// documented non-Git monorepo behavior through the apply command.
func TestMarkerlessApplyOptInUsesCentralManifest(t *testing.T) {
	monoDir := setupMarkerlessMonorepo(t, "")
	optinDir := makeSubdir(t, monoDir, "optin")
	centralManifest := filepath.Join(monoDir, fileutil.ManifestFileName)
	t.Chdir(monoDir)

	if out, err := runCLI("apply", "--allow-ancestor-search", "--with", "makefile", "optin"); err != nil {
		t.Fatalf("apply --allow-ancestor-search failed: %v\nOutput: %s", err, out)
	}

	if _, err := os.Stat(filepath.Join(optinDir, fileutil.ManifestFileName)); !os.IsNotExist(err) {
		t.Errorf("opt-in apply should not create a nested manifest, stat err = %v", err)
	}

	central, err := os.ReadFile(centralManifest) //nolint:gosec // test-controlled temp path
	if err != nil {
		t.Fatalf("reading central manifest failed: %v", err)
	}
	content := string(central)
	if !strings.Contains(content, `name = "makefile"`) {
		t.Errorf("central manifest does not record the template: %q", content)
	}
	if !strings.Contains(content, `path = "optin"`) {
		t.Errorf("central manifest does not record the subproject path: %q", content)
	}

	if _, err := os.Stat(filepath.Join(optinDir, "Makefile")); err != nil {
		t.Errorf("opt-in apply did not generate the Makefile: %v", err)
	}
}

// TestMarkerlessListProjectsFlagPlumbing proves `list projects` forwards the
// flag to root discovery: without it the local (absent) manifest is empty.
func TestMarkerlessListProjectsFlagPlumbing(t *testing.T) {
	monoDir := setupMarkerlessMonorepo(t, "[[projects]]\nname = \"api\"\npath = \"services/api\"\nlanguage = \"go\"\n")
	subDir := makeSubdir(t, monoDir, "sub")
	t.Chdir(subDir)

	if out, err := runCLI("list", "projects"); err != nil {
		t.Fatalf("list projects failed: %v\nOutput: %s", err, out)
	} else if !strings.Contains(out, "No projects found.") {
		t.Errorf("default list adopted the ancestor manifest: %s", out)
	}

	out, err := runCLI("list", "projects", "--allow-ancestor-search")
	if err != nil {
		t.Fatalf("list projects --allow-ancestor-search failed: %v\nOutput: %s", err, out)
	}
	for _, want := range []string{"api", "lang: go", "path: services/api"} {
		if !strings.Contains(out, want) {
			t.Errorf("opt-in list output missing %q: %s", want, out)
		}
	}
}

// TestMarkerlessDriftFlagPlumbing proves `drift` forwards the flag: without it
// the local (possibly absent) manifest hides the recorded drift.
func TestMarkerlessDriftFlagPlumbing(t *testing.T) {
	monoDir := setupMarkerlessMonorepo(t, "[managed_files]\n\"sub/Makefile\" = \"0000000000000000000000000000000000000000000000000000000000000000\"\n")
	subDir := makeSubdir(t, monoDir, "sub")
	if err := os.WriteFile(filepath.Join(subDir, "Makefile"), []byte("locally edited\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	t.Chdir(subDir)

	if out, err := runCLI("drift"); err != nil {
		t.Fatalf("default drift failed: %v\nOutput: %s", err, out)
	} else if !strings.Contains(out, "No drift detected.") {
		t.Errorf("default drift evaluated the ancestor manifest: %s", out)
	}

	out, err := runCLI("drift", "--allow-ancestor-search")
	if err == nil {
		t.Fatalf("opt-in drift reported no drift for a mismatched file:\n%s", out)
	}
	if !strings.Contains(out, "Drift detected") {
		t.Errorf("opt-in drift did not report drift: %s", out)
	}
}
