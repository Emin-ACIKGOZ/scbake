// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"scbake/internal/core"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/templates"
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
