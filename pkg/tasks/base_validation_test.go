// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"os"
	"path/filepath"
	"testing"

	"scbake/internal/util/fileutil"
)

// TestMergeRejectsInvalidBaseShadow is a regression guard for a stale or
// corrupt base shadow. The base must match the pristine hash recorded in
// managed_files; otherwise merge.Merge can treat one side as unchanged and
// silently keep the local file or drop the template change.
func TestMergeRejectsInvalidBaseShadow(t *testing.T) { //nolint:cyclop // table-driven regression test
	const (
		original = "alpha\nshared\nomega\n"
		local    = "alpha-local\nshared\nomega\n"
		updated  = "alpha\nshared\nomega-template\n"
	)

	for _, tt := range []struct {
		name string
		base string
	}{
		{name: "base equals local edit", base: local},
		{name: "base equals new template", base: updated},
		{name: "corrupt base", base: "not the pristine render\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root, tx, tc := mergeTestContext(t)
			defer func() { _ = tx.Rollback() }()

			if err := newMergeTask(original).Execute(tc); err != nil {
				t.Fatalf("initial execute: %v", err)
			}
			recorded := tc.Manifest.ManagedFiles["Makefile"]

			basePath := filepath.Join(root, fileutil.InternalDir, fileutil.BaseDir, "Makefile")
			if err := os.WriteFile(basePath, []byte(tt.base), 0o600); err != nil {
				t.Fatalf("write stale base: %v", err)
			}
			if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(local), 0o600); err != nil {
				t.Fatalf("write local edit: %v", err)
			}

			if err := newMergeTask(updated).Execute(tc); err != nil {
				t.Fatalf("merge execute: %v", err)
			}

			gotLocal, err := os.ReadFile(filepath.Join(root, "Makefile")) //nolint:gosec // test temp path
			if err != nil {
				t.Fatalf("read local file: %v", err)
			}
			if string(gotLocal) != local {
				t.Fatalf("local file was modified: got %q, want %q", gotLocal, local)
			}

			artifact, err := os.ReadFile(filepath.Join(root, "Makefile.scbake-new")) //nolint:gosec // test temp path
			if err != nil {
				t.Fatalf("expected artifact for an invalid base: %v", err)
			}
			if string(artifact) != updated {
				t.Fatalf("artifact = %q, want %q", artifact, updated)
			}

			if got := tc.Manifest.ManagedFiles["Makefile"]; got != recorded {
				t.Fatalf("invalid base advanced the ledger: got %s, want %s", got, recorded)
			}
		})
	}
}

// TestMergeUsesValidBaseShadow is the negative control: a base matching the
// recorded pristine hash still merges disjoint edits cleanly.
func TestMergeUsesValidBaseShadow(t *testing.T) {
	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	if err := newMergeTask("alpha\nshared\nomega\n").Execute(tc); err != nil {
		t.Fatalf("initial execute: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte("alpha-local\nshared\nomega\n"), 0o600); err != nil {
		t.Fatalf("write local edit: %v", err)
	}

	if err := newMergeTask("alpha\nshared\nomega-template\n").Execute(tc); err != nil {
		t.Fatalf("merge execute: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "Makefile")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read merged file: %v", err)
	}
	if want := "alpha-local\nshared\nomega-template\n"; string(got) != want {
		t.Fatalf("merged = %q, want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(root, "Makefile.scbake-new")); !os.IsNotExist(err) {
		t.Fatalf("valid base should merge without an artifact; stat err = %v", err)
	}
}
