// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
