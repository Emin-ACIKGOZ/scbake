// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/util/fileutil"
)

// TestDriftCleanExitsZero: a freshly generated project has no drift.
func TestDriftCleanExitsZero(t *testing.T) {
	setupUpdateProject(t, "alpha\nshared\nomega\n")

	out, err := runCLI("drift")
	if err != nil {
		t.Fatalf("drift on a clean project failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No drift detected") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestDriftIgnoresEOLOnlyChurn pins the EOL-insensitive contract end to end:
// a text file rewritten with CRLF is not drift, so an autocrlf clone does not
// fail CI.
func TestDriftIgnoresEOLOnlyChurn(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")
	mustWrite(t, filepath.Join(projectPath, "Makefile"), "alpha\r\nshared\r\nomega\r\n")

	out, err := runCLI("drift")
	if err != nil {
		t.Fatalf("drift reported EOL-only churn: %v\n%s", err, out)
	}
	if !strings.Contains(out, "No drift detected") {
		t.Errorf("unexpected output: %s", out)
	}
}

// TestDriftModifiedExitsNonZero: a local edit is listed and the exit is non-zero
// so it can gate CI.
func TestDriftModifiedExitsNonZero(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")
	mustWrite(t, filepath.Join(projectPath, "Makefile"), "alpha\nLOCAL\nomega\n")

	out, err := runCLI("drift")
	if err == nil {
		t.Fatalf("drift reported success on a modified file:\n%s", out)
	}
	if !strings.Contains(out, "modified") || !strings.Contains(out, "Makefile") {
		t.Errorf("drift did not report the modified file: %s", out)
	}
}

// TestDriftMissingExitsNonZero: a deleted managed file is reported as missing.
func TestDriftMissingExitsNonZero(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")
	if err := os.Remove(filepath.Join(projectPath, "Makefile")); err != nil {
		t.Fatalf("remove Makefile: %v", err)
	}

	out, err := runCLI("drift")
	if err == nil {
		t.Fatalf("drift reported success on a deleted file:\n%s", out)
	}
	if !strings.Contains(out, "missing") || !strings.Contains(out, "Makefile") {
		t.Errorf("drift did not report the missing file: %s", out)
	}
}

// TestDriftRepairRestoresGeneratedContent: --repair reuses the update pipeline,
// so overwrite restores the pristine render.
func TestDriftRepairRestoresGeneratedContent(t *testing.T) {
	projectPath, overrideDir := setupUpdateProject(t, "alpha\nshared\nomega\n")
	mustWrite(t, filepath.Join(projectPath, "Makefile"), "clobbered\n")

	out, err := runCLI("drift", "--repair", "--conflict-strategy", "overwrite", "--template-dir", overrideDir)
	if err != nil {
		t.Fatalf("drift --repair failed: %v\n%s", err, out)
	}
	if got := mustRead(t, filepath.Join(projectPath, "Makefile")); got != "alpha\nshared\nomega\n" {
		t.Fatalf("repair did not restore generated content: %q", got)
	}
	if out, err := runCLI("drift"); err != nil {
		t.Fatalf("project still drifts after repair: %v\n%s", err, out)
	}
}

// removeCommittedBase deletes the committed base for a ledger key so a test can
// exercise the missing-base policy.
func removeCommittedBase(t *testing.T, project, key string) {
	t.Helper()
	if err := os.Remove(filepath.Join(project, fileutil.CommittedBaseDir, filepath.FromSlash(key))); err != nil {
		t.Fatalf("remove committed base for %s: %v", key, err)
	}
}

// TestDriftMissingBaseIsAdvisory: a missing base alone is reported but exits 0
// by default, because a clean project may legitimately have no committed base.
func TestDriftMissingBaseIsAdvisory(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")
	removeCommittedBase(t, projectPath, "Makefile")

	out, err := runCLI("drift")
	if err != nil {
		t.Fatalf("drift on a missing base alone exited non-zero: %v\n%s", err, out)
	}
	for _, want := range []string{"advisory", "base-missing", "Makefile", ".scbake-base/Makefile"} {
		if !strings.Contains(out, want) {
			t.Errorf("drift output missing %q: %s", want, out)
		}
	}
	if strings.Contains(out, "No drift detected") {
		t.Errorf("missing base was reported as a fully clean result: %s", out)
	}
}

// TestDriftRequireBaseMissingExitsOne: --require-base promotes the advisory
// missing-base finding to a CI failure.
func TestDriftRequireBaseMissingExitsOne(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")
	removeCommittedBase(t, projectPath, "Makefile")

	out, err := runCLI("drift", "--require-base")
	requireNonZeroExit(t, out, err)
	if code, ok := exitCodeOf(err); !ok || code != fileutil.ExitError {
		t.Errorf("drift --require-base exit code = %d, want %d (output=%s)", code, fileutil.ExitError, out)
	}
	for _, want := range []string{"base-missing", "Missing committed merge base", "Makefile"} {
		if !strings.Contains(out, want) {
			t.Errorf("drift --require-base output missing %q: %s", want, out)
		}
	}
}

// TestDriftInvalidBaseExitsOne: a stale committed base blocks and is reported
// with its expected and actual hashes.
func TestDriftInvalidBaseExitsOne(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")
	basePath := filepath.Join(projectPath, fileutil.CommittedBaseDir, "Makefile")
	mustWrite(t, basePath, "stale base\n")
	before := mustRead(t, basePath)

	out, err := runCLI("drift")
	requireNonZeroExit(t, out, err)
	if code, ok := exitCodeOf(err); !ok || code != fileutil.ExitError {
		t.Errorf("drift invalid base exit code = %d, want %d (output=%s)", code, fileutil.ExitError, out)
	}
	for _, want := range []string{"base-invalid", "Invalid committed merge base", ".scbake-base/Makefile"} {
		if !strings.Contains(out, want) {
			t.Errorf("drift invalid base output missing %q: %s", want, out)
		}
	}
	if after := mustRead(t, basePath); after != before {
		t.Errorf("drift repaired the invalid base:\n%s\n---\n%s", before, after)
	}
}

// TestDriftRequireBaseRejectsRepair: combining the CI gate with repair is a
// usage error rather than a silently ignored flag.
func TestDriftRequireBaseRejectsRepair(t *testing.T) {
	setupUpdateProject(t, "alpha\nshared\nomega\n")

	out, err := runCLI("drift", "--require-base", "--repair")
	requireNonZeroExit(t, out, err)
	if code, ok := exitCodeOf(err); !ok || code != fileutil.ExitError {
		t.Errorf("drift --require-base --repair exit code = %d, want %d (output=%s)", code, fileutil.ExitError, out)
	}
	if !strings.Contains(out, "--require-base cannot be combined with --repair") {
		t.Errorf("drift did not explain the conflicting flags: %s", out)
	}
}

// TestDriftRepairBusyExits75 pins the unchanged lock contract: repair delegates
// to update, so a contended root still exits 75.
func TestDriftRepairBusyExits75(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")

	lock, err := transaction.AcquireRootLock(projectPath)
	if err != nil {
		t.Fatalf("acquire root lock: %v", err)
	}
	defer func() { _ = lock.Release() }()

	out, err := runCLI("drift", "--repair")
	requireNonZeroExit(t, out, err)
	if code, ok := exitCodeOf(err); !ok || code != fileutil.ExitBusy {
		t.Errorf("busy drift --repair exit code = %d, want %d (output=%s)", code, fileutil.ExitBusy, out)
	}
	if !strings.Contains(out, "another scbake process is operating on this project") {
		t.Errorf("busy drift --repair output does not explain contention: %s", out)
	}
}

// TestDriftIsReadOnly pins the safety property the CI gate depends on: running
// drift (without --repair) must not rewrite files or churn the manifest.
func TestDriftIsReadOnly(t *testing.T) {
	projectPath, _ := setupUpdateProject(t, "alpha\nshared\nomega\n")
	makefile := filepath.Join(projectPath, "Makefile")
	manifest := filepath.Join(projectPath, "scbake.toml")

	mustWrite(t, makefile, "alpha\nLOCAL\nomega\n")
	freezeMtime(t, makefile)
	freezeMtime(t, manifest)
	before := mustRead(t, manifest)

	if out, err := runCLI("drift"); err == nil {
		t.Fatalf("drift reported success on a modified file:\n%s", out)
	}

	assertNotRewritten(t, makefile)
	assertNotRewritten(t, manifest)
	if after := mustRead(t, manifest); after != before {
		t.Errorf("drift rewrote the manifest:\n%s\n---\n%s", before, after)
	}
}
