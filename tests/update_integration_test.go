// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// frozenTime is a fixed timestamp used to detect needless rewrites: a file
// whose mtime is unchanged after a command was not written to.
var frozenTime = time.Unix(1_000_000_000, 0)

// freezeMtime pins a file's modification time so a rewrite is detectable.
func freezeMtime(t *testing.T, path string) {
	t.Helper()
	if err := os.Chtimes(path, frozenTime, frozenTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

// assertNotRewritten fails if path's mtime changed since freezeMtime.
func assertNotRewritten(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.ModTime().Equal(frozenTime) {
		t.Errorf("%s was rewritten by a no-op update", path)
	}
}

// mustWrite writes body to a test-controlled path.
func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// mustRead returns the contents of a test-controlled path.
func mustRead(t *testing.T, path string) string {
	t.Helper()
	//nolint:gosec // test-controlled temp path
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// setupUpdateProject creates a project whose Makefile comes from a controlled
// override, then chdirs into it.
func setupUpdateProject(t *testing.T, body string) (projectPath, overrideDir string) {
	t.Helper()
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	overrideDir = filepath.Join(tmpDir, "overrides")
	if err := os.MkdirAll(overrideDir, 0o750); err != nil {
		t.Fatalf("mkdir overrides: %v", err)
	}
	mustWrite(t, filepath.Join(overrideDir, "makefile.tpl"), body)

	projectPath = filepath.Join(tmpDir, "proj")
	out, err := runCLI("new", "proj", "--with", "makefile", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	_ = os.Chdir(projectPath)
	return projectPath, overrideDir
}

// TestUpdateRecordsProvenance verifies D2: the manifest records the real
// template reference rather than the old placeholder.
func TestUpdateRecordsProvenance(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")

	manifest := mustRead(t, filepath.Join(projectPath, "scbake.toml"))
	if !strings.Contains(manifest, `name = "makefile"`) {
		t.Errorf("manifest does not record the makefile template:\n%s", manifest)
	}
	if strings.Contains(manifest, "root-templates") {
		t.Errorf("manifest still records the placeholder:\n%s", manifest)
	}
}

// TestUpdatePreservesLocalEditWhenTemplateUnchanged is the mutation-sensitive
// idempotency guard: an update mutated to always-rewrite would clobber the
// local edit.
func TestUpdatePreservesLocalEditWhenTemplateUnchanged(t *testing.T) {
	projectPath, overrideDir := setupUpdateProject(t, "alpha\nshared\nomega\n")
	makefile := filepath.Join(projectPath, "Makefile")

	edited := "alpha-local\nshared\nomega\n"
	mustWrite(t, makefile, edited)
	freezeMtime(t, makefile)

	out, err := runCLI("update", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	if got := mustRead(t, makefile); got != edited {
		t.Fatalf("local edit not preserved: got %q, want %q", got, edited)
	}
	// A no-op update must not rewrite the file. This is the mutation-sensitive
	// guard: an update mutated to always-rewrite changes the mtime.
	assertNotRewritten(t, makefile)
}

// TestUpdateIsIdempotent verifies I1: running update twice with no change is a
// byte-for-byte no-op.
func TestUpdateIsIdempotent(t *testing.T) {
	projectPath, overrideDir := setupUpdateProject(t, "alpha\nshared\nomega\n")
	makefile := filepath.Join(projectPath, "Makefile")
	manifest := filepath.Join(projectPath, "scbake.toml")
	freezeMtime(t, makefile)

	if out, err := runCLI("update", "--template-dir", overrideDir); err != nil {
		t.Fatalf("first update failed: %v\n%s", err, out)
	}
	firstFile, firstManifest := mustRead(t, makefile), mustRead(t, manifest)

	if out, err := runCLI("update", "--template-dir", overrideDir); err != nil {
		t.Fatalf("second update failed: %v\n%s", err, out)
	}
	if second := mustRead(t, makefile); second != firstFile {
		t.Errorf("update not idempotent: %q -> %q", firstFile, second)
	}
	if second := mustRead(t, manifest); second != firstManifest {
		t.Errorf("update churned the manifest:\n%s\n---\n%s", firstManifest, second)
	}
	assertNotRewritten(t, makefile)
}

// TestUpdateAppliesTemplateChange verifies that a template improvement reaches
// a project with no local edits.
func TestUpdateAppliesTemplateChange(t *testing.T) {
	projectPath, overrideDir := setupUpdateProject(t, "alpha\nshared\nomega\n")
	mustWrite(t, filepath.Join(overrideDir, "makefile.tpl"), "alpha\nshared\nomega-template\n")

	out, err := runCLI("update", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	if got := mustRead(t, filepath.Join(projectPath, "Makefile")); got != "alpha\nshared\nomega-template\n" {
		t.Fatalf("template change not applied: %q", got)
	}
}

// TestUpdateMergesDisjointChanges verifies three-way merge: a local edit and a
// disjoint template edit are both kept.
func TestUpdateMergesDisjointChanges(t *testing.T) {
	projectPath, overrideDir := setupUpdateProject(t, "alpha\nshared\nomega\n")
	makefile := filepath.Join(projectPath, "Makefile")

	mustWrite(t, makefile, "alpha-local\nshared\nomega\n")
	mustWrite(t, filepath.Join(overrideDir, "makefile.tpl"), "alpha\nshared\nomega-template\n")

	out, err := runCLI("update", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	if got := mustRead(t, makefile); got != "alpha-local\nshared\nomega-template\n" {
		t.Fatalf("disjoint merge = %q", got)
	}
}

// TestUpdateConflictWritesArtifact verifies that a conflicting change is not
// clobbered and the template render is written to an artifact.
func TestUpdateConflictWritesArtifact(t *testing.T) {
	projectPath, overrideDir := setupUpdateProject(t, "alpha\nshared\nomega\n")
	makefile := filepath.Join(projectPath, "Makefile")

	local := "alpha\nshared-LOCAL\nomega\n"
	mustWrite(t, makefile, local)
	mustWrite(t, filepath.Join(overrideDir, "makefile.tpl"), "alpha\nshared-TEMPLATE\nomega\n")

	out, err := runCLI("update", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	if got := mustRead(t, makefile); got != local {
		t.Fatalf("conflict clobbered the local file: %q", got)
	}
	if got := mustRead(t, makefile+".scbake-new"); got != "alpha\nshared-TEMPLATE\nomega\n" {
		t.Fatalf("artifact = %q", got)
	}
}

// TestUpdateDoesNotDuplicateProjectBlocks is a regression test for the
// render-time manifest re-adding an existing project. Templates that range
// .Projects (such as makefile) must not emit the project block twice after an
// update.
func TestUpdateDoesNotDuplicateProjectBlocks(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	_ = os.Chdir(tmpDir)

	out, err := runCLI("new", "langproj", "--lang", "go", "--with", "makefile")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}
	projectPath := filepath.Join(tmpDir, "langproj")
	_ = os.Chdir(projectPath)

	if out, err := runCLI("update"); err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}

	makefile := mustRead(t, filepath.Join(projectPath, "Makefile"))
	if n := strings.Count(makefile, "Building "); n != 1 {
		t.Fatalf("Makefile rendered %d project blocks, want 1:\n%s", n, makefile)
	}
}
