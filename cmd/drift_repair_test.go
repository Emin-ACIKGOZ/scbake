// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

const driftRepairFixtureManifest = `scbake_version = "v1.0.0"

[[projects]]
name = "demo"
path = "."
language = "go"
`

// newDriftRepairProject creates a Go project with a recorded managed main.go and
// .gitignore. It drives `update` from a hand-written manifest so the fixture
// needs no Go toolchain: update filters the language pack's side-effecting
// go/mod tasks and replays only the managed template writes.
func newDriftRepairProject(t *testing.T) string {
	t.Helper()

	resetFlags()
	updateConflictStrategyFlag = types.ConflictMerge
	driftStrategyFlag = types.ConflictMerge

	root := t.TempDir()
	t.Chdir(root)

	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(driftRepairFixtureManifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing %s: %v", fileutil.ManifestFileName, err)
	}
	if err := executeCLI("update", "."); err != nil {
		t.Fatalf("seeding fixture with update failed: %v", err)
	}
	return root
}

const driftRepairMonorepoManifest = `scbake_version = "v1.0.0"

[[projects]]
name = "a"
path = "services/a"
language = "go"

[[projects]]
name = "b"
path = "services/b"
language = "go"
`

// newDriftRepairMonorepo creates two Go projects under services/. A root .git
// marker lets a scoped update from a subdirectory discover the manifest root
// without --allow-ancestor-search.
func newDriftRepairMonorepo(t *testing.T) string {
	t.Helper()

	resetFlags()
	updateConflictStrategyFlag = types.ConflictMerge
	driftStrategyFlag = types.ConflictMerge

	root := t.TempDir()
	t.Chdir(root)

	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(driftRepairMonorepoManifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing %s: %v", fileutil.ManifestFileName, err)
	}
	if err := os.Mkdir(filepath.Join(root, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatalf("creating Git marker: %v", err)
	}
	for _, target := range []string{"services/a", "services/b"} {
		if err := executeCLI("update", target); err != nil {
			t.Fatalf("seeding %s with update failed: %v", target, err)
		}
	}
	return root
}

// addLocalEditAndDeleteGitignore reproduces the issue fixture: main.go gains a
// local edit that merge preserves, and .gitignore is deleted so repair has one
// restorable change.
func addLocalEditAndDeleteGitignore(t *testing.T, root string) {
	t.Helper()

	mainPath := filepath.Join(root, "main.go")
	f, err := os.OpenFile(mainPath, os.O_APPEND|os.O_WRONLY, fileutil.FilePerms) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("opening main.go: %v", err)
	}
	if _, err := f.WriteString("\n// local edit\n"); err != nil {
		_ = f.Close()
		t.Fatalf("appending local edit: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing main.go: %v", err)
	}

	if err := os.Remove(filepath.Join(root, fileutil.GitIgnore)); err != nil {
		t.Fatalf("removing %s: %v", fileutil.GitIgnore, err)
	}
}

// TestDriftRepairReportsResidualDrift pins the reporting half of #226: merge
// restores .gitignore but preserves the local main.go edit, so the repair must
// say that drift remains even though it still exits 0.
func TestDriftRepairReportsResidualDrift(t *testing.T) {
	root := newDriftRepairProject(t)
	addLocalEditAndDeleteGitignore(t, root)

	var repairErr error
	out := captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair")
	})
	if repairErr != nil {
		t.Fatalf("drift --repair = %v, want nil for the documented merge exit contract", repairErr)
	}

	if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); err != nil {
		t.Errorf("repair did not restore %s: %v", fileutil.GitIgnore, err)
	}
	mainContent, err := os.ReadFile(filepath.Join(root, "main.go")) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	if !strings.Contains(string(mainContent), "// local edit") {
		t.Error("repair did not preserve the local main.go edit")
	}

	if !strings.Contains(out, "still drifted") {
		t.Errorf("repair output does not report residual drift: %q", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("repair output does not name the residual file: %q", out)
	}

	// Reset the shared flag state so this invocation is a plain drift scan
	// rather than a replay of the previous --repair flags.
	resetFlags()
	if err := executeCLI("drift"); !errors.Is(err, errDriftDetected) {
		t.Errorf("next drift = %v, want errDriftDetected", err)
	}
}

// TestDriftRepairDryRunAnnotatesPreservedFile pins the preview half of #226: the
// dry run must disclose main.go's reconciliation outcome, predict the residual
// drift, and leave the tree untouched.
//
//nolint:cyclop // end-to-end dry-run assertions
func TestDriftRepairDryRunAnnotatesPreservedFile(t *testing.T) {
	root := newDriftRepairProject(t)
	addLocalEditAndDeleteGitignore(t, root)

	mainPath := filepath.Join(root, "main.go")
	before, err := os.ReadFile(mainPath) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("reading main.go before dry run: %v", err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if dryRunErr != nil {
		t.Fatalf("drift --repair --dry-run = %v, want nil", dryRunErr)
	}

	const wantHeader = "Dry run: 1 managed file(s) may remain drifted:"
	planAndHeader, residual, ok := strings.Cut(out, wantHeader)
	if !ok {
		t.Fatalf("dry run does not predict exactly one residual file: %q", out)
	}
	if !strings.Contains(residual, "main.go") {
		t.Errorf("dry run residual section does not name main.go: %q", residual)
	}
	if strings.Contains(residual, ".gitignore") {
		t.Errorf("dry run residual section lists the restorable .gitignore: %q", residual)
	}
	// This fixture's main.go template is unchanged, so the shared reconciliation
	// decision is a no-op ("no change"); "local edits preserved" is the note for
	// a merge override that equals the existing bytes. Either wording discloses
	// that no write would happen.
	if !strings.Contains(out, "(no change)") && !strings.Contains(out, "(local edits preserved)") {
		t.Errorf("dry run does not annotate the preserved main.go: %q", out)
	}
	if !strings.Contains(out, "[DRY RUN] Skip main.go") {
		t.Errorf("dry run does not use the Skip verb for the preserved main.go: %q", out)
	}
	if strings.Contains(planAndHeader, ".gitignore (no change)") {
		t.Errorf("restorable .gitignore should be an ordinary Create, got plan: %q", planAndHeader)
	}

	after, err := os.ReadFile(mainPath) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("reading main.go after dry run: %v", err)
	}
	if string(before) != string(after) {
		t.Error("dry run modified main.go")
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); !os.IsNotExist(err) {
		t.Errorf("dry run recreated %s (stat error: %v)", fileutil.GitIgnore, err)
	}
}

// TestDriftRepairDryRunScopedMonorepoPredictsOtherProject pins the scoped-plan
// false clean: repairing services/a must not claim that services/b's missing
// file will be restored, because the plan only targets services/a.
//
//nolint:cyclop // end-to-end scoped dry-run assertions
func TestDriftRepairDryRunScopedMonorepoPredictsOtherProject(t *testing.T) {
	root := newDriftRepairMonorepo(t)
	for _, rel := range []string{"services/a/main.go", "services/b/main.go"} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("removing %s: %v", rel, err)
		}
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run", "services/a")
	})
	if dryRunErr != nil {
		t.Fatalf("scoped dry-run repair = %v, want nil", dryRunErr)
	}
	_, residual, ok := strings.Cut(out, "may remain drifted")
	if !ok {
		t.Fatalf("scoped dry run predicted no residual: %q", out)
	}
	if !strings.Contains(residual, "services/b/main.go") {
		t.Errorf("scoped residual section omits services/b/main.go: %q", residual)
	}
	if strings.Contains(residual, "services/a/main.go") {
		t.Errorf("scoped residual section lists the restored services/a/main.go: %q", residual)
	}
	if strings.Contains(residual, ".gitignore") {
		t.Errorf("scoped residual section lists a restored .gitignore: %q", residual)
	}

	// The real scoped repair must leave the same residual set.
	resetFlags()
	var repairErr error
	realOut := captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair", "services/a")
	})
	if repairErr != nil {
		t.Fatalf("scoped real repair = %v, want nil", repairErr)
	}
	if !strings.Contains(realOut, "still drifted") || !strings.Contains(realOut, "services/b/main.go") {
		t.Errorf("real scoped repair summary does not match the prediction: %q", realOut)
	}
	if strings.Contains(realOut, "services/a/main.go") {
		t.Errorf("real scoped repair summary lists the restored services/a/main.go: %q", realOut)
	}

	resetFlags()
	if err := executeCLI("drift"); !errors.Is(err, errDriftDetected) {
		t.Errorf("drift after scoped repair = %v, want errDriftDetected", err)
	}
}

// TestDriftRepairDryRunStaleLedgerKeyIsResidual pins the other scope hole: a
// managed_files key with no planned writer is not restorable and must stay in
// the prediction.
func TestDriftRepairDryRunStaleLedgerKeyIsResidual(t *testing.T) {
	root := newDriftRepairProject(t)

	m, _, err := manifest.Load(root)
	if err != nil {
		t.Fatalf("loading manifest: %v", err)
	}
	m.ManagedFiles["stale.txt"] = strings.Repeat("a", 64)
	if err := manifest.Save(m, root); err != nil {
		t.Fatalf("saving stale ledger key: %v", err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if dryRunErr != nil {
		t.Fatalf("dry-run repair with stale key = %v, want nil", dryRunErr)
	}
	_, residual, ok := strings.Cut(out, "may remain drifted")
	if !ok {
		t.Fatalf("stale-key dry run predicted no residual: %q", out)
	}
	if !strings.Contains(residual, "stale.txt") {
		t.Errorf("residual section omits the stale ledger key: %q", residual)
	}
}

// TestDriftRepairDryRunOverwritePredictsClean is the dry-run control: overwrite
// would restore both the local edit and the deleted file, so the preview must
// not predict residual drift.
func TestDriftRepairDryRunOverwritePredictsClean(t *testing.T) {
	root := newDriftRepairProject(t)
	addLocalEditAndDeleteGitignore(t, root)

	mainPath := filepath.Join(root, "main.go")
	before, err := os.ReadFile(mainPath) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("reading main.go before dry run: %v", err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run", "--conflict-strategy", "overwrite")
	})
	if dryRunErr != nil {
		t.Fatalf("drift --repair --dry-run --conflict-strategy overwrite = %v, want nil", dryRunErr)
	}
	if strings.Contains(out, "may remain drifted") {
		t.Errorf("overwrite dry run predicted residual drift: %q", out)
	}

	after, err := os.ReadFile(mainPath) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("reading main.go after dry run: %v", err)
	}
	if string(before) != string(after) {
		t.Error("overwrite dry run modified main.go")
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); !os.IsNotExist(err) {
		t.Errorf("overwrite dry run recreated %s (stat error: %v)", fileutil.GitIgnore, err)
	}
}

// TestDriftRepairDryRunForcePredictsClean covers --force with merge: a forced
// repair canonically rewrites the modified file, so the preview must predict no
// residual and the real repair must clear the drift.
func TestDriftRepairDryRunForcePredictsClean(t *testing.T) {
	root := newDriftRepairProject(t)
	addLocalEditAndDeleteGitignore(t, root)

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run", "--force")
	})
	if dryRunErr != nil {
		t.Fatalf("drift --repair --dry-run --force = %v, want nil", dryRunErr)
	}
	if strings.Contains(out, "may remain drifted") {
		t.Errorf("forced dry run predicted residual drift: %q", out)
	}

	resetFlags()
	var repairErr error
	captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair", "--force")
	})
	if repairErr != nil {
		t.Fatalf("drift --repair --force = %v, want nil", repairErr)
	}
	mainContent, err := os.ReadFile(filepath.Join(root, "main.go")) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	if strings.Contains(string(mainContent), "// local edit") {
		t.Error("forced repair did not restore the managed main.go")
	}

	resetFlags()
	if err := executeCLI("drift"); err != nil {
		t.Errorf("drift after forced repair = %v, want nil", err)
	}
}

// TestDriftRepairDryRunCorruptBasePrediction ensures a corrupt base is predicted
// conservatively under merge (which does not rewrite a clean file) and that the
// read-only prediction cannot panic.
//
// The prediction reports base-invalid residual even though a real merge run
// repairs the base, because Validate does not plan the real-run-only base
// write. This conservative over-report is accepted; it must never under-report.
func TestDriftRepairDryRunCorruptBasePrediction(t *testing.T) {
	root := newDriftRepairProject(t)

	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "main.go")
	if err := os.WriteFile(basePath, []byte("corrupt base\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("corrupting %s: %v", basePath, err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if dryRunErr != nil {
		t.Fatalf("drift --repair --dry-run = %v, want nil", dryRunErr)
	}
	if !strings.Contains(out, "may remain drifted") {
		t.Errorf("corrupt-base dry run did not predict residual drift: %q", out)
	}
	if !strings.Contains(out, "main.go") {
		t.Errorf("corrupt-base dry run does not name main.go: %q", out)
	}
}

// TestDriftRepairDryRunCorruptBaseCleanWriteNoResidual covers a corrupt base
// whose canonical file still matches the recorded hash: overwrite plans a clean
// canonical write, so the base is repaired and the preview is quiet.
func TestDriftRepairDryRunCorruptBaseCleanWriteNoResidual(t *testing.T) {
	root := newDriftRepairProject(t)

	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "main.go")
	if err := os.WriteFile(basePath, []byte("corrupt base\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("corrupting %s: %v", basePath, err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run", "--conflict-strategy", "overwrite")
	})
	if dryRunErr != nil {
		t.Fatalf("overwrite dry-run repair = %v, want nil", dryRunErr)
	}
	if strings.Contains(out, "may remain drifted") {
		t.Errorf("clean-write dry run predicted residual drift: %q", out)
	}
}

// TestDriftRepairAdvisoryBaseOnlyDoesNotWarn ensures a missing committed base,
// which drift treats as advisory and merge does not backfill, is not reported as
// residual drift.
func TestDriftRepairAdvisoryBaseOnlyDoesNotWarn(t *testing.T) {
	root := newDriftRepairProject(t)

	// Remove the pristine base for a still-clean managed file. The working tree
	// is untouched, so the only finding is an advisory BaseMissing.
	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "main.go")
	if err := os.Remove(basePath); err != nil {
		t.Fatalf("removing %s: %v", basePath, err)
	}

	var repairErr error
	out := captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair")
	})
	if repairErr != nil {
		t.Fatalf("advisory-only drift --repair = %v, want nil", repairErr)
	}
	if strings.Contains(out, "still drifted") {
		t.Errorf("advisory-only repair printed a residual warning: %q", out)
	}

	resetFlags()
	if err := executeCLI("drift"); err != nil {
		t.Errorf("drift with only an advisory missing base = %v, want nil", err)
	}
}

// TestDriftRepairCleanDoesNotWarn ensures a repair that leaves no findings does
// not print a residual summary.
func TestDriftRepairCleanDoesNotWarn(t *testing.T) {
	_ = newDriftRepairProject(t)

	var repairErr error
	out := captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair")
	})
	if repairErr != nil {
		t.Fatalf("clean drift --repair = %v, want nil", repairErr)
	}
	if strings.Contains(out, "still drifted") {
		t.Errorf("clean repair printed a residual warning: %q", out)
	}
}

// TestDriftRepairOverwriteClearsDrift is the control: the explicit overwrite
// strategy still restores main.go and leaves no drift.
func TestDriftRepairOverwriteClearsDrift(t *testing.T) {
	root := newDriftRepairProject(t)
	addLocalEditAndDeleteGitignore(t, root)

	var repairErr error
	out := captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair", "--conflict-strategy", "overwrite")
	})
	if repairErr != nil {
		t.Fatalf("overwrite drift --repair = %v, want nil", repairErr)
	}
	if strings.Contains(out, "still drifted") {
		t.Errorf("overwrite repair printed a residual warning: %q", out)
	}

	mainContent, err := os.ReadFile(filepath.Join(root, "main.go")) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	if strings.Contains(string(mainContent), "// local edit") {
		t.Error("overwrite repair did not restore the managed main.go")
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); err != nil {
		t.Errorf("overwrite repair did not restore %s: %v", fileutil.GitIgnore, err)
	}

	resetFlags()
	if err := executeCLI("drift"); err != nil {
		t.Errorf("drift after overwrite repair = %v, want nil", err)
	}
}

const driftRepairLicenseManifest = `scbake_version = "v1.0.0"

[[projects]]
name = "demo"
path = "."
language = "go"

[[templates]]
name = "compliance"
path = "."

[metadata]
license = "MIT"
copyright_holder = "Test Holder"
`

// newDriftRepairLicenseProject seeds a Go project whose manifest also selects
// the compliance template, so update/repair replay the managed LICENSE writer.
func newDriftRepairLicenseProject(t *testing.T) string {
	t.Helper()

	resetFlags()
	updateConflictStrategyFlag = types.ConflictMerge
	driftStrategyFlag = types.ConflictMerge

	root := t.TempDir()
	t.Chdir(root)

	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(manifestPath, []byte(driftRepairLicenseManifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing %s: %v", fileutil.ManifestFileName, err)
	}
	if err := executeCLI("update", "."); err != nil {
		t.Fatalf("seeding license fixture with update failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "LICENSE")); err != nil {
		t.Fatalf("fixture update did not create LICENSE: %v", err)
	}
	return root
}

// TestDriftRepairDryRunPredictsDeletedLicenseRestorable pins the compliance half
// of #226: LICENSE is a managed writer too, so a dry-run repair must predict its
// canonical write instead of listing the deleted file as residual drift.
func TestDriftRepairDryRunPredictsDeletedLicenseRestorable(t *testing.T) {
	root := newDriftRepairLicenseProject(t)
	licensePath := filepath.Join(root, "LICENSE")
	if err := os.Remove(licensePath); err != nil {
		t.Fatalf("removing LICENSE: %v", err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if dryRunErr != nil {
		t.Fatalf("drift --repair --dry-run = %v, want nil", dryRunErr)
	}
	if strings.Contains(out, "may remain drifted") {
		t.Errorf("dry run predicted residual drift for the restorable LICENSE: %q", out)
	}

	resetFlags()
	var repairErr error
	captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair")
	})
	if repairErr != nil {
		t.Fatalf("drift --repair = %v, want nil", repairErr)
	}
	content, err := os.ReadFile(licensePath) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("repair did not restore LICENSE: %v", err)
	}
	if !strings.Contains(string(content), "MIT License") {
		t.Errorf("restored LICENSE is not MIT: %q", content)
	}

	resetFlags()
	if err := executeCLI("drift"); err != nil {
		t.Errorf("drift after LICENSE repair = %v, want nil", err)
	}
}

// TestDriftRepairDryRunLicenseCorruptBasePrediction mirrors the main.go base
// cases: overwrite plans a canonical write so the base is repaired, while merge
// keeps its conservative prediction because it does not rewrite a clean file.
//
// The merge prediction is deliberately conservative: Validate plans no
// canonical write, so the pre-repair base-invalid finding survives and the dry
// run reports LICENSE as residual even though the real merge run repairs the
// base from the clean working file. That over-report is accepted: a preview may
// claim more drift than remains, never less, and the real run still clears it.
func TestDriftRepairDryRunLicenseCorruptBasePrediction(t *testing.T) {
	root := newDriftRepairLicenseProject(t)

	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "LICENSE")
	if err := os.WriteFile(basePath, []byte("corrupt base\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("corrupting %s: %v", basePath, err)
	}

	var mergeErr error
	mergeOut := captureStdout(t, func() {
		mergeErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if mergeErr != nil {
		t.Fatalf("merge dry-run repair = %v, want nil", mergeErr)
	}
	if !strings.Contains(mergeOut, "may remain drifted") || !strings.Contains(mergeOut, "LICENSE") {
		t.Errorf("merge corrupt-base dry run was not conservative about LICENSE: %q", mergeOut)
	}

	resetFlags()
	var overwriteErr error
	overwriteOut := captureStdout(t, func() {
		overwriteErr = executeCLI("drift", "--repair", "--dry-run", "--conflict-strategy", "overwrite")
	})
	if overwriteErr != nil {
		t.Fatalf("overwrite dry-run repair = %v, want nil", overwriteErr)
	}
	if strings.Contains(overwriteOut, "may remain drifted") {
		t.Errorf("overwrite dry run predicted residual drift for a clean LICENSE: %q", overwriteOut)
	}
}

// TestDriftRepairDryRunLicenseCleanSkipAnnotated pins the annotation for the
// update skip path of an unchanged LICENSE: the preview must use the Skip verb
// and distinguish a base-only repair from a true no-op, while predicting no
// residual drift and leaving the real LICENSE bytes untouched.
func TestDriftRepairDryRunLicenseCleanSkipAnnotated(t *testing.T) { //nolint:cyclop // end-to-end annotation assertions
	root := newDriftRepairLicenseProject(t)
	licensePath := filepath.Join(root, "LICENSE")
	before, err := os.ReadFile(licensePath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("reading seeded LICENSE: %v", err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if dryRunErr != nil {
		t.Fatalf("clean dry-run repair = %v, want nil", dryRunErr)
	}
	if !strings.Contains(out, "[DRY RUN] Skip LICENSE file (no change)") {
		t.Errorf("clean LICENSE dry run = %q, want the no-change Skip annotation", out)
	}
	if strings.Contains(out, "may remain drifted") {
		t.Errorf("clean LICENSE dry run predicted residual drift: %q", out)
	}

	// Remove the committed base so only the real-run repair has work to do.
	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "LICENSE")
	if err := os.Remove(basePath); err != nil {
		t.Fatalf("removing %s: %v", basePath, err)
	}
	resetFlags()
	out = captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if dryRunErr != nil {
		t.Fatalf("base-repair dry-run = %v, want nil", dryRunErr)
	}
	if !strings.Contains(out, "[DRY RUN] Skip LICENSE file (base repair only)") {
		t.Errorf("base-repair dry run = %q, want the base-repair Skip annotation", out)
	}
	if strings.Contains(out, "may remain drifted") {
		t.Errorf("base-repair dry run predicted residual drift: %q", out)
	}
	if _, err := os.Stat(basePath); !os.IsNotExist(err) {
		t.Errorf("dry run repaired the base shadow (stat err: %v)", err)
	}

	resetFlags()
	var repairErr error
	captureStdout(t, func() {
		repairErr = executeCLI("drift", "--repair")
	})
	if repairErr != nil {
		t.Fatalf("base-repair real run = %v, want nil", repairErr)
	}
	if _, err := os.Stat(basePath); err != nil {
		t.Errorf("real repair did not restore the base shadow: %v", err)
	}
	after, err := os.ReadFile(licensePath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("reading LICENSE after repair: %v", err)
	}
	if string(after) != string(before) {
		t.Error("base-only repair rewrote the LICENSE content")
	}
}

// TestDriftRepairDryRunLicenseMergeSkipsModifiedLicense pins the preview for a
// locally modified LICENSE: merge cannot apply the unchanged template, so the
// file remains residual and the plan discloses the skip/preserve decision.
func TestDriftRepairDryRunLicenseMergeSkipsModifiedLicense(t *testing.T) {
	root := newDriftRepairLicenseProject(t)
	licensePath := filepath.Join(root, "LICENSE")
	f, err := os.OpenFile(licensePath, os.O_APPEND|os.O_WRONLY, fileutil.FilePerms) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("opening LICENSE: %v", err)
	}
	if _, err := f.WriteString("\nlocal license edit\n"); err != nil {
		_ = f.Close()
		t.Fatalf("appending local edit: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing LICENSE: %v", err)
	}

	var dryRunErr error
	out := captureStdout(t, func() {
		dryRunErr = executeCLI("drift", "--repair", "--dry-run")
	})
	if dryRunErr != nil {
		t.Fatalf("drift --repair --dry-run = %v, want nil", dryRunErr)
	}
	_, residual, ok := strings.Cut(out, "may remain drifted")
	if !ok {
		t.Fatalf("modified LICENSE dry run predicted no residual: %q", out)
	}
	if !strings.Contains(residual, "LICENSE") {
		t.Errorf("residual section omits the modified LICENSE: %q", residual)
	}
	if !strings.Contains(out, "[DRY RUN] Skip LICENSE file") {
		t.Errorf("dry run does not use the Skip verb for the preserved LICENSE: %q", out)
	}
	if !strings.Contains(out, "(no change)") && !strings.Contains(out, "(local edits preserved)") {
		t.Errorf("dry run does not annotate the preserved LICENSE: %q", out)
	}
}
