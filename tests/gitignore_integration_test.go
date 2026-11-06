// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// requireSingleInternalStateIgnore fails unless the root .gitignore has exactly
// one standalone .scbake/ entry.
func requireSingleInternalStateIgnore(t *testing.T, project string) {
	t.Helper()

	content := mustRead(t, filepath.Join(project, fileutil.GitIgnore))
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimRight(line, " \t\r") == ".scbake/" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("%s has %d .scbake/ entries, want 1:\n%s", fileutil.GitIgnore, count, content)
	}
}

// requireBaseShadow fails unless .scbake/base exists and contains a shadow file.
func requireBaseShadow(t *testing.T, project string) {
	t.Helper()

	base := filepath.Join(project, fileutil.InternalDir, fileutil.BaseDir)
	info, err := os.Stat(base)
	if err != nil {
		t.Fatalf("base shadow directory is missing: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", base)
	}

	found := false
	err = filepath.Walk(base, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking base shadow directory: %v", err)
	}
	if !found {
		t.Fatalf("base shadow directory %s contains no shadow files", base)
	}
}

// decodeManifest parses the project manifest.
func decodeManifest(t *testing.T, project string) types.Manifest {
	t.Helper()

	var m types.Manifest
	if _, err := toml.DecodeFile(filepath.Join(project, fileutil.ManifestFileName), &m); err != nil {
		t.Fatalf("decoding %s: %v", fileutil.ManifestFileName, err)
	}
	return m
}

// requireInternalStateNotManaged fails if any .scbake path was recorded in the
// managed_files ledger. Base shadows are state, not generated project files.
func requireInternalStateNotManaged(t *testing.T, m types.Manifest) {
	t.Helper()

	for key := range m.ManagedFiles {
		normalized := filepath.ToSlash(key)
		if normalized == fileutil.InternalDir || strings.HasPrefix(normalized, fileutil.InternalDir+"/") {
			t.Errorf("%s unexpectedly tracks internal state %q", fileutil.ManifestFileName, key)
		}
	}
}

// TestNewWithMakefileIgnoresInternalState is the regression test for issue
// #101: a tooling-only project has no language .gitignore, so the universal
// helper must create one and keep the persistent .scbake/base state untracked.
func TestNewWithMakefileIgnoresInternalState(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)

	out, err := runCLI("new", "tooling", "--with", "makefile")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	project := filepath.Join(work, "tooling")
	requireSingleInternalStateIgnore(t, project)
	requireBaseShadow(t, project)

	manifest := decodeManifest(t, project)
	requireInternalStateNotManaged(t, manifest)
	if _, ok := manifest.ManagedFiles["Makefile"]; !ok {
		t.Errorf("%s does not track Makefile: %v", fileutil.ManifestFileName, manifest.ManagedFiles)
	}

	// Applying the same selection again must not append a second entry.
	for i := 1; i <= 2; i++ {
		out, err := runCLI("apply", "--with", "makefile", project)
		if err != nil {
			t.Fatalf("apply run %d failed: %v\n%s", i, err, out)
		}
		requireSingleInternalStateIgnore(t, project)
		requireBaseShadow(t, project)
	}
}

// TestGoProjectDoesNotDuplicateInternalStateIgnore verifies the universal helper
// coordinates with the Go pack's existing .scbake/ entry instead of adding a
// duplicate line.
func TestGoProjectDoesNotDuplicateInternalStateIgnore(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}

	work := t.TempDir()
	t.Chdir(work)

	out, err := runCLI("new", "goapp", "--lang", "go", "--with", "makefile")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	project := filepath.Join(work, "goapp")
	requireSingleInternalStateIgnore(t, project)
	requireBaseShadow(t, project)

	manifest := decodeManifest(t, project)
	requireInternalStateNotManaged(t, manifest)

	// Re-applying must not duplicate the Go pack's pre-existing entry either.
	out, err = runCLI("apply", "--with", "makefile", project)
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}
	requireSingleInternalStateIgnore(t, project)
}

// requireNoDrift fails when drift scans the freshly reconciled project.
func requireNoDrift(t *testing.T, project string) {
	t.Helper()

	out, err := runCLI("drift", project)
	if err != nil {
		t.Fatalf("drift reported changes after reconciliation: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No drift detected") {
		t.Fatalf("unexpected drift output:\n%s", out)
	}
}

// TestManagedRootGitIgnoreHashMatchesDisk is the integration regression test for
// the MEDIUM review finding: a .gitignore-writing template override without
// .scbake/ gets the entry injected before hashing. The managed_files ledger must
// match the on-disk file, and neither drift nor a later update may report
// scbake's own append as user drift.
func TestManagedRootGitIgnoreHashMatchesDisk(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}

	work := t.TempDir()
	t.Chdir(work)

	overrideDir := filepath.Join(work, "overrides")
	if err := os.MkdirAll(overrideDir, 0o750); err != nil {
		t.Fatalf("create override dir: %v", err)
	}
	mustWrite(t, filepath.Join(overrideDir, "gitignore.tpl"), "vendor/\n")

	out, err := runCLI("new", "managed-ignore", "--lang", "go", "--with", "makefile", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	project := filepath.Join(work, "managed-ignore")
	onDisk := mustRead(t, filepath.Join(project, fileutil.GitIgnore))
	if !strings.Contains(onDisk, ".scbake/\n") {
		t.Fatalf("managed .gitignore does not contain .scbake/:\n%q", onDisk)
	}

	manifest := decodeManifest(t, project)
	wantHash := tasks.HashContent([]byte(onDisk))
	if got := manifest.ManagedFiles[fileutil.GitIgnore]; got != wantHash {
		t.Fatalf("managed_files[%q] = %q, want on-disk hash %q", fileutil.GitIgnore, got, wantHash)
	}
	requireNoDrift(t, project)

	// A subsequent reconciliation must keep the ledger and disk in agreement.
	out, err = runCLI("update", "--template-dir", overrideDir, project)
	if err != nil {
		t.Fatalf("update failed: %v\n%s", err, out)
	}
	updated := mustRead(t, filepath.Join(project, fileutil.GitIgnore))
	if updated != onDisk {
		t.Fatalf("update changed .gitignore: got %q, want %q", updated, onDisk)
	}
	manifest = decodeManifest(t, project)
	if got := manifest.ManagedFiles[fileutil.GitIgnore]; got != tasks.HashContent([]byte(updated)) {
		t.Fatalf("update desynchronized managed_files[%q]: got %q", fileutil.GitIgnore, got)
	}
	requireNoDrift(t, project)
}
