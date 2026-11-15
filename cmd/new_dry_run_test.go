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

// resetNewDryRunFlags clears every global flag the 'new' command reads.
func resetNewDryRunFlags() {
	resetNewFlags()
}

// writePoisonedRootManifest writes a manifest whose project path escapes the
// repository and whose test_command contains a newline. Both fields are rejected
// by plan-time validation, so any run that loads this manifest must fail.
func writePoisonedRootManifest(t *testing.T, root string) {
	t.Helper()

	const poisoned = `scbake_version = "v0.3.0"

[[projects]]
name = "evil"
path = "../evil"
language = "go"

[metadata]
test_command = "echo safe\necho pwned"
`
	path := filepath.Join(root, fileutil.ManifestFileName)
	if err := os.WriteFile(path, []byte(poisoned), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing poisoned manifest: %v", err)
	}
}

// TestNewDryRunPlansInNamedProject is a regression test for #190: a dry-run
// 'new' must plan inside the project it would create. Before the fix it planned
// in the parent directory, so the preview named the parent and described
// creating '.' rather than the requested project directory.
func TestNewDryRunPlansInNamedProject(t *testing.T) {
	resetNewDryRunFlags()

	parent := t.TempDir()
	t.Chdir(parent)

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = executeCLI("new", "spring-dry", "--lang", "spring", "--dry-run")
	})
	if runErr != nil {
		t.Fatalf("dry run = %v, want nil; stdout:\n%s", runErr, stdout)
	}

	if !strings.Contains(stdout, "Create project directory 'spring-dry'") {
		t.Errorf("dry-run plan did not print the relative project directory; stdout:\n%s", stdout)
	}
	if strings.Contains(stdout, "Create project directory '"+parent+"'") {
		t.Errorf("dry-run plan used the absolute parent as the project directory; stdout:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Download Spring Boot starter for 'spring-dry'") {
		t.Errorf("dry-run plan did not derive the project name from the new project; stdout:\n%s", stdout)
	}
	if strings.Contains(stdout, filepath.Base(parent)) {
		t.Errorf("dry-run plan printed the parent directory name %q; stdout:\n%s", filepath.Base(parent), stdout)
	}
	if _, err := os.Stat(filepath.Join(parent, "spring-dry")); !os.IsNotExist(err) {
		t.Errorf("dry run created the project directory (stat err: %v)", err)
	}
}

// TestNewDryRunIgnoresPoisonedParentManifest is a regression test for #190: a
// 'new' run writes its own empty manifest in the new directory, so a dry run
// must not discover the parent repository's manifest instead. The real run is
// exercised too, pinning that both paths agree on the project name and path.
func TestNewDryRunIgnoresPoisonedParentManifest(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	resetNewDryRunFlags()

	root := t.TempDir()
	t.Chdir(root)
	if err := os.Mkdir(filepath.Join(root, fileutil.GitDir), fileutil.DirPerms); err != nil {
		t.Fatalf("creating .git marker: %v", err)
	}
	writePoisonedRootManifest(t, root)

	manifestPath := filepath.Join(root, fileutil.ManifestFileName)
	before, err := os.ReadFile(filepath.Clean(manifestPath)) // #nosec G304
	if err != nil {
		t.Fatalf("reading poisoned manifest: %v", err)
	}

	var dryErr error
	dryStdout := captureStdout(t, func() {
		dryErr = executeCLI("new", "spring-dry", "--lang", "go", "--dry-run")
	})
	if dryErr != nil {
		t.Fatalf("dry run against a poisoned parent manifest = %v, want nil; stdout:\n%s", dryErr, dryStdout)
	}
	if _, statErr := os.Stat(filepath.Join(root, "spring-dry")); !os.IsNotExist(statErr) {
		t.Errorf("dry run created the project directory (stat err: %v)", statErr)
	}
	after, err := os.ReadFile(filepath.Clean(manifestPath)) // #nosec G304
	if err != nil {
		t.Fatalf("re-reading poisoned manifest: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("dry run modified the parent manifest:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	var runErr error
	resetNewDryRunFlags()
	stdout := captureStdout(t, func() {
		runErr = executeCLI("new", "spring-dry", "--lang", "go")
	})
	if runErr != nil {
		t.Fatalf("real run against a poisoned parent manifest = %v, want nil; stdout:\n%s", runErr, stdout)
	}

	generated, err := os.ReadFile(filepath.Clean(filepath.Join(root, "spring-dry", fileutil.ManifestFileName))) // #nosec G304
	if err != nil {
		t.Fatalf("reading generated manifest: %v", err)
	}
	content := string(generated)
	if !strings.Contains(content, `name = "spring-dry"`) {
		t.Errorf("generated manifest does not record the project name:\n%s", content)
	}
	if !strings.Contains(content, `path = "."`) {
		t.Errorf("generated manifest does not record the project-relative path:\n%s", content)
	}
	if strings.Contains(content, "../evil") {
		t.Errorf("generated manifest inherited the poisoned path:\n%s", content)
	}
}

// TestApplyDryRunRejectsPoisonedRoot guards the scope of the #190 bypass: only
// 'new' dry runs get the synthetic manifest, so 'apply --dry-run' on the same
// poisoned root must still fail plan-time validation.
func TestApplyDryRunRejectsPoisonedRoot(t *testing.T) {
	resetNewDryRunFlags()

	root := t.TempDir()
	t.Chdir(root)
	writePoisonedRootManifest(t, root)

	var runErr error
	stdout := captureStdout(t, func() {
		runErr = executeCLI("apply", "--lang", "go", "--dry-run")
	})
	if runErr == nil {
		t.Fatalf("apply --dry-run on a poisoned root = nil, want an error; stdout:\n%s", stdout)
	}
	if !strings.Contains(runErr.Error(), "../evil") && !strings.Contains(runErr.Error(), "test_command") {
		t.Errorf("apply --dry-run error does not identify the poisoned manifest field: %v", runErr)
	}
}

// TestNewExistingTargetWithForceStillFailsBeforeWrite pins that #190 did not
// weaken the existing-directory guard: --force does not overwrite a directory,
// with or without --dry-run, and nothing is written into it.
func TestNewExistingTargetWithForceStillFailsBeforeWrite(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{name: "dry run", args: []string{"new", "spring-dry", "--lang", "spring", "--dry-run", "--force"}},
		{name: "real run", args: []string{"new", "spring-dry", "--lang", "spring", "--force"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetNewDryRunFlags()

			root := t.TempDir()
			t.Chdir(root)

			existing := filepath.Join(root, "spring-dry")
			if err := os.Mkdir(existing, fileutil.DirPerms); err != nil {
				t.Fatalf("creating existing project directory: %v", err)
			}
			sentinel := filepath.Join(existing, "sentinel.txt")
			if err := os.WriteFile(sentinel, []byte("keep"), fileutil.PrivateFilePerms); err != nil {
				t.Fatalf("writing sentinel: %v", err)
			}

			var runErr error
			stdout := captureStdout(t, func() {
				runErr = executeCLI(tc.args...)
			})
			if runErr == nil {
				t.Fatalf("new with --force on an existing directory = nil, want an error; stdout:\n%s", stdout)
			}
			if !strings.Contains(runErr.Error(), "already exists") {
				t.Errorf("error = %v, want the existing-directory error", runErr)
			}
			if content, err := os.ReadFile(filepath.Clean(sentinel)); err != nil || string(content) != "keep" {
				t.Errorf("sentinel changed (content %q, err %v)", content, err)
			}
			if _, err := os.Stat(filepath.Join(existing, fileutil.ManifestFileName)); !os.IsNotExist(err) {
				t.Errorf("failed run wrote a manifest into the existing directory (stat err: %v)", err)
			}
			assertNoFailedProjectStaging(t, root)
		})
	}
}
