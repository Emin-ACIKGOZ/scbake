// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
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
// dry run must disclose that main.go will not be written, predict the residual
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
