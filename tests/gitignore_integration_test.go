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

// requireCommittedBaseBlock fails unless the root .gitignore contains the
// .scbake/ ignore and all three committed-base re-include lines exactly once.
func requireCommittedBaseBlock(t *testing.T, project string) {
	t.Helper()

	content := mustRead(t, filepath.Join(project, fileutil.GitIgnore))
	for _, entry := range []string{".scbake/", "!.scbake-base/", "!.scbake-base/**", "!.gitignore"} {
		count := 0
		for _, line := range strings.Split(content, "\n") {
			if strings.TrimRight(line, " \t\r") == entry {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("%s has %d %q entries, want 1:\n%s", fileutil.GitIgnore, count, entry, content)
		}
	}
}

// requireBaseShadow fails unless .scbake-base exists and contains a shadow
// file, and the legacy .scbake/base tree is absent.
func requireBaseShadow(t *testing.T, project string) {
	t.Helper()

	base := filepath.Join(project, fileutil.CommittedBaseDir)
	info, err := os.Stat(base)
	if err != nil {
		t.Fatalf("committed base shadow directory is missing: %v", err)
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
		t.Fatalf("walking committed base shadow directory: %v", err)
	}
	if !found {
		t.Fatalf("committed base shadow directory %s contains no shadow files", base)
	}
	if _, err := os.Stat(filepath.Join(project, fileutil.InternalDir, fileutil.BaseDir)); !os.IsNotExist(err) {
		t.Fatalf("legacy .scbake/base must not exist (stat err: %v)", err)
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
// helper must create one, keep the private .scbake state ignored, and re-include
// the committed .scbake-base tree.
func TestNewWithMakefileIgnoresInternalState(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)

	out, err := runCLI("new", "tooling", "--with", "makefile")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	project := filepath.Join(work, "tooling")
	requireSingleInternalStateIgnore(t, project)
	requireCommittedBaseBlock(t, project)
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
		requireCommittedBaseBlock(t, project)
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
	requireCommittedBaseBlock(t, project)
	requireBaseShadow(t, project)

	manifest := decodeManifest(t, project)
	requireInternalStateNotManaged(t, manifest)

	// Re-applying must not duplicate the Go pack's pre-existing entry either.
	out, err = runCLI("apply", "--with", "makefile", project)
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}
	requireSingleInternalStateIgnore(t, project)
	requireCommittedBaseBlock(t, project)
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

// TestCommittedBaseSanitizesGitSpecialNames proves the base tree never keeps a
// raw .gitignore or .gitattributes basename: a copied ignore rule must not hide
// sibling base payloads, and a copied attribute rule must not transform them.
// The Go pack's root .gitignore exercises the root mapping; nested mapping is
// covered by the unit round-trip test.
func TestCommittedBaseSanitizesGitSpecialNames(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go not installed")
	}

	work := t.TempDir()
	t.Chdir(work)

	out, err := runCLI("new", "sanitized-base", "--lang", "go")
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	project := filepath.Join(work, "sanitized-base")
	raw := filepath.Join(project, fileutil.CommittedBaseDir, fileutil.GitIgnore)
	if _, err := os.Stat(raw); !os.IsNotExist(err) {
		t.Fatalf("raw %s must not exist in the base tree (stat err: %v)", raw, err)
	}
	shadow := filepath.Join(project, fileutil.CommittedBaseDir, fileutil.GitIgnore+fileutil.BaseShadowSuffix)
	if _, err := os.Stat(shadow); err != nil {
		t.Fatalf("sanitized base shadow missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, fileutil.InternalDir, fileutil.BaseDir)); !os.IsNotExist(err) {
		t.Fatalf("legacy .scbake/base must not exist (stat err: %v)", err)
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

// TestManagedRootGitIgnoreStaleEntryDoesNotCreateDrift covers a legacy managed
// root .gitignore whose recorded hash matches a render without .scbake/. The
// helper must warn with regeneration guidance and leave the file untouched, so
// the existing ledger remains clean instead of being desynchronized by an
// out-of-band append.
func TestManagedRootGitIgnoreStaleEntryDoesNotCreateDrift(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
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

	out, err := runCLI("new", "stale-ignore", "--lang", "go", "--with", "makefile", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("new failed: %v\n%s", err, out)
	}

	project := filepath.Join(work, "stale-ignore")
	manifestPath := filepath.Join(project, fileutil.ManifestFileName)
	manifest := decodeManifest(t, project)
	oldHash := manifest.ManagedFiles[fileutil.GitIgnore]
	if oldHash == "" {
		t.Fatalf("new did not record managed_files[%q]", fileutil.GitIgnore)
	}

	// Simulate a managed render recorded before the injection hardening.
	const staleIgnore = "vendor/\n"
	mustWrite(t, filepath.Join(project, fileutil.GitIgnore), staleIgnore)
	staleHash := tasks.HashContent([]byte(staleIgnore))
	body := mustRead(t, manifestPath)
	if !strings.Contains(body, oldHash) {
		t.Fatalf("manifest does not contain the old .gitignore hash %q:\n%s", oldHash, body)
	}
	mustWrite(t, manifestPath, strings.Replace(body, oldHash, staleHash, 1))
	// Refresh the committed base to the same stale render so the fixture models
	// a coherent legacy entry rather than an intentionally invalid base.
	mustWrite(t, filepath.Join(project, tasks.BaseShadowRelPath(fileutil.GitIgnore)), staleIgnore)

	// Applying another managed file invokes the helper for the stale .gitignore.
	out, err = runCLI("apply", "--with", "makefile", project)
	if err != nil {
		t.Fatalf("apply failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Managed .gitignore") ||
		!strings.Contains(out, "regenerate it with 'scbake apply' or 'scbake update'") {
		t.Fatalf("apply did not warn about the stale managed .gitignore:\n%s", out)
	}
	if got := mustRead(t, filepath.Join(project, fileutil.GitIgnore)); got != staleIgnore {
		t.Fatalf("helper mutated the stale managed .gitignore: got %q, want %q", got, staleIgnore)
	}
	if got := decodeManifest(t, project).ManagedFiles[fileutil.GitIgnore]; got != staleHash {
		t.Fatalf("helper changed the recorded hash: got %q, want %q", got, staleHash)
	}
	requireNoDrift(t, project)
}
