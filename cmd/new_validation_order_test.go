// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"scbake/internal/core"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/templates"
	"strings"
	"testing"
)

// resetNewFlags clears every global flag the 'new' command reads, including the
// ones resetFlags does not touch, so cases in this file cannot leak state into
// one another.
func resetNewFlags() {
	resetFlags()
	newSetFlag = nil
	newConflictStrategyFlag = "fail"
	newLicenseFlag = ""
	newCopyrightHolderFlag = ""
	templateDirFlag = ""
}

// TestRunNew_ValidatesBeforeCreatingDirectory is a regression test for SCB-012:
// an invalid --set pair, an unknown --set key, or a missing --lang/--with must
// fail before the project directory or its manifest is materialized, so a
// failed 'new' leaves no residue behind.
func TestRunNew_ValidatesBeforeCreatingDirectory(t *testing.T) {
	cases := []struct {
		name    string
		project string
		set     []string
		wantErr error
	}{
		{
			name:    "malformed set pair",
			project: "residue-bad-set",
			set:     []string{"invalidkey"},
		},
		{
			name:    "unknown set key with no selection",
			project: "residue-unknown-key",
			set:     []string{"bogus=1"},
		},
		{
			name:    "no language or template",
			project: "residue-no-selection",
			wantErr: core.ErrNoSelection,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetNewFlags()
			work := t.TempDir()
			// t.Chdir restores the working directory before the TempDir
			// cleanup runs; removing the current directory fails on Windows.
			t.Chdir(work)

			newSetFlag = tc.set

			created := false
			err := runNew(tc.project, &created)
			if err == nil {
				t.Fatalf("runNew(%q) = nil, want an error", tc.project)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("runNew(%q) error = %v, want errors.Is(_, %v)", tc.project, err, tc.wantErr)
			}
			if created {
				t.Errorf("runNew(%q) reported creating a directory despite invalid flags", tc.project)
			}
			if _, statErr := os.Stat(filepath.Join(work, tc.project)); !os.IsNotExist(statErr) {
				t.Errorf("runNew(%q) materialized the project directory before validating flags (stat err: %v)", tc.project, statErr)
			}
		})
	}
}

// TestRunNew_ChecksToolsBeforeCreatingDirectory is a regression test for #147:
// when the selected language's toolchain is missing, 'new' must fail before the
// project directory is created rather than creating it and cleaning it up.
func TestRunNew_ChecksToolsBeforeCreatingDirectory(t *testing.T) {
	resetNewFlags()

	work := t.TempDir()
	// t.Chdir restores the working directory before the TempDir cleanup runs;
	// removing the current directory fails on Windows.
	t.Chdir(work)
	// An empty PATH entry hides the go binary from exec.LookPath.
	t.Setenv("PATH", t.TempDir())

	const project = "residue-no-go"
	newLangFlag = "go"

	created := false
	if err := runNew(project, &created); err == nil {
		t.Fatalf("runNew(%q) = nil, want an error for missing go", project)
	}
	if created {
		t.Errorf("runNew(%q) created the project directory before checking language tools", project)
	}
	if _, statErr := os.Stat(filepath.Join(work, project)); !os.IsNotExist(statErr) {
		t.Errorf("runNew(%q) left a project directory behind (stat err: %v)", project, statErr)
	}
}

// TestRunNew_DryRunWarnsOnMissingTool guards the dry-run half of the same fix:
// a missing language toolchain is reported as a warning, not an error, and the
// dry run writes no project directory.
func TestRunNew_DryRunWarnsOnMissingTool(t *testing.T) {
	resetNewFlags()

	work := t.TempDir()
	t.Chdir(work)
	t.Setenv("PATH", t.TempDir())
	dryRun = true

	const project = "residue-dry-no-go"
	newLangFlag = "go"

	created := false
	var runErr error
	stderr := captureStderr(t, func() {
		runErr = runNew(project, &created)
	})
	if runErr != nil {
		t.Fatalf("runNew(%q) dry run = %v, want nil (missing tool is a warning)", project, runErr)
	}
	if !strings.Contains(stderr, "⚠️  'go' command not found") {
		t.Errorf("runNew(%q) dry run stderr = %q, want the missing-go warning", project, stderr)
	}
	if _, statErr := os.Stat(filepath.Join(work, project)); !os.IsNotExist(statErr) {
		t.Errorf("runNew(%q) dry run created a project directory (stat err: %v)", project, statErr)
	}
}

// captureStderr redirects os.Stderr for the duration of fn and returns what
// was written.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe failed: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer failed: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stderr failed: %v", err)
	}
	_ = r.Close()
	return string(data)
}

// TestRunNew_ValidSelectionStillCreatesProject guards the happy path after the
// validation reordering: a valid selection still creates the directory, writes
// the manifest, runs the tasks, and returns no error.
func TestRunNew_ValidSelectionStillCreatesProject(t *testing.T) {
	resetNewFlags()

	work := t.TempDir()
	// t.Chdir restores the working directory before the TempDir cleanup runs;
	// removing the current directory fails on Windows.
	t.Chdir(work)

	const project = "valid-project"
	target := filepath.Join(work, project, "generated.txt")
	templates.Register("scb012-ok", &MockHandlerGeneric{
		Tasks: []types.Task{&MockCreateTask{TargetFile: target}},
	})
	newWithFlag = []string{"scb012-ok"}

	created := false
	if err := runNew(project, &created); err != nil {
		t.Fatalf("runNew(%q) = %v, want nil", project, err)
	}
	if !created {
		t.Errorf("runNew(%q) dirCreated = false, want true", project)
	}
	if info, statErr := os.Stat(filepath.Join(work, project)); statErr != nil || !info.IsDir() {
		t.Errorf("project directory missing after a successful run: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(work, project, fileutil.ManifestFileName)); statErr != nil {
		t.Errorf("manifest missing after a successful run: %v", statErr)
	}
	if _, statErr := os.Stat(target); statErr != nil {
		t.Errorf("template task output missing after a successful run: %v", statErr)
	}
}
