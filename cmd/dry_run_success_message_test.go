// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

const dryRunCompleteMessage = "✅ Dry run complete; no changes written."

// TestNewDryRunSuccessMessage is a regression test for #214: a dry run writes
// nothing, so it must not claim the project was created.
func TestNewDryRunSuccessMessage(t *testing.T) {
	resetNewDryRunFlags()
	t.Cleanup(resetNewDryRunFlags)

	parent := t.TempDir()
	t.Chdir(parent)

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = executeCLI("new", "demo", "--lang", "go", "--dry-run")
	})
	if runErr != nil {
		t.Fatalf("dry run = %v, want nil; stdout:\n%s", runErr, stdout)
	}

	if !strings.Contains(stdout, dryRunCompleteMessage) {
		t.Errorf("dry-run output does not state that nothing was written; stdout:\n%s", stdout)
	}
	if strings.Contains(stdout, "created") {
		t.Errorf("dry-run output claims the project was created; stdout:\n%s", stdout)
	}
	if strings.Contains(stdout, "Success!") {
		t.Errorf("dry-run output claims success; stdout:\n%s", stdout)
	}
	if strings.Contains(stdout, "Creating directory") {
		t.Errorf("dry-run output says it is creating the directory; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Would create directory") {
		t.Errorf("dry-run output does not describe the directory as a planned action; stdout:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(parent, "demo")); !os.IsNotExist(err) {
		t.Errorf("dry run created the project directory (stat err: %v)", err)
	}
}

// writeEditorconfigManifest seeds a manifest that records the editorconfig
// template at the repo root, so both apply and update can replay it.
func writeEditorconfigManifest(t *testing.T, root string) {
	t.Helper()

	const manifest = `scbake_version = "v1.0.0"

[[templates]]
name = "editorconfig"
path = "."
`
	path := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(path, []byte(manifest), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing manifest: %v", err)
	}
}

// TestApplyUpdateDryRunSuccessMessage is a regression test for #214: apply and
// update dry runs write nothing, so they must use the dry-run wording instead
// of claiming the command finished.
func TestApplyUpdateDryRunSuccessMessage(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "apply", args: []string{"apply", "--with", "editorconfig", "--dry-run"}},
		{name: "update", args: []string{"update", "--dry-run"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()
			t.Cleanup(resetFlags)

			root := t.TempDir()
			t.Chdir(root)
			writeEditorconfigManifest(t, root)

			var runErr error
			stdout := captureStdout(t, func() {
				runErr = executeCLI(tc.args...)
			})
			if runErr != nil {
				t.Fatalf("%s dry run = %v, want nil; stdout:\n%s", tc.name, runErr, stdout)
			}

			if !strings.Contains(stdout, dryRunCompleteMessage) {
				t.Errorf("%s dry-run output does not state that nothing was written; stdout:\n%s", tc.name, stdout)
			}
			if strings.Contains(stdout, "finished") {
				t.Errorf("%s dry-run output claims the command finished; stdout:\n%s", tc.name, stdout)
			}
			if strings.Contains(stdout, "Success!") {
				t.Errorf("%s dry-run output claims success; stdout:\n%s", tc.name, stdout)
			}
			if _, err := os.Stat(filepath.Join(root, ".editorconfig")); !os.IsNotExist(err) {
				t.Errorf("%s dry run created .editorconfig (stat err: %v)", tc.name, err)
			}
		})
	}
}
