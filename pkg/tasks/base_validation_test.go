// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"os"
	"path/filepath"
	"strings"
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

			basePath := filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")
			if err := os.WriteFile(basePath, []byte(tt.base), 0o600); err != nil {
				t.Fatalf("write stale base: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir, fileutil.BaseDir)); !os.IsNotExist(err) {
				t.Fatalf("legacy .scbake/base must not exist (stat err: %v)", err)
			}
			if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(local), 0o600); err != nil {
				t.Fatalf("write local edit: %v", err)
			}

			out := captureStderr(t, func() {
				if err := newMergeTask(updated).Execute(tc); err != nil {
					t.Fatalf("merge execute: %v", err)
				}
			})
			if !strings.Contains(out, "No valid pristine base for Makefile (hash mismatch)") {
				t.Fatalf("missing invalid-base warning:\n%s", out)
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

// TestMergeWithoutBaseWarnsAndWritesArtifact covers a manifest whose base is
// absent (for example a legacy project or a clone that lacks .scbake-base).
// The update must warn about the specific missing-base cause, preserve the
// local file, and leave the template render in an artifact without advancing
// the ledger.
func TestMergeWithoutBaseWarnsAndWritesArtifact(t *testing.T) { //nolint:cyclop // assertion-heavy artifact regression test
	const (
		original = "alpha\nshared\nomega\n"
		local    = "alpha-local\nshared\nomega\n"
		updated  = "alpha\nshared\nomega-template\n"
	)

	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	if err := newMergeTask(original).Execute(tc); err != nil {
		t.Fatalf("initial execute: %v", err)
	}
	recorded := tc.Manifest.ManagedFiles["Makefile"]
	if err := os.Remove(filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")); err != nil {
		t.Fatalf("remove base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(local), 0o600); err != nil {
		t.Fatalf("write local edit: %v", err)
	}

	out := captureStderr(t, func() {
		if err := newMergeTask(updated).Execute(tc); err != nil {
			t.Fatalf("merge execute: %v", err)
		}
	})
	if !strings.Contains(out, "No valid pristine base for Makefile (missing)") {
		t.Fatalf("missing missing-base warning:\n%s", out)
	}

	gotLocal, err := os.ReadFile(filepath.Join(root, "Makefile")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read local file: %v", err)
	}
	if string(gotLocal) != local {
		t.Fatalf("missing base clobbered the local file: got %q, want %q", gotLocal, local)
	}
	artifact, err := os.ReadFile(filepath.Join(root, "Makefile.scbake-new")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("expected artifact for a missing base: %v", err)
	}
	if string(artifact) != updated {
		t.Fatalf("artifact = %q, want %q", artifact, updated)
	}
	if got := tc.Manifest.ManagedFiles["Makefile"]; got != recorded {
		t.Fatalf("missing base advanced the ledger: got %s, want %s", got, recorded)
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
	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")
	if got, err := os.ReadFile(basePath); err != nil { //nolint:gosec // test temp path
		t.Fatalf("read base: %v", err)
	} else if string(got) != "alpha\nshared\nomega\n" {
		t.Fatalf("base = %q, want the pristine render", got)
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

// TestMergeAcceptsLegacyCRLFBase covers a migrated pre-EOL-normalization
// project: the ledger records the raw SHA-256 of CRLF-rendered bytes and the
// committed base is byte-exact CRLF. The production change accepts
// rawContentHash(base) as well as HashContent(base), so a disjoint local edit
// plus template change must still merge cleanly instead of writing an artifact.
func TestMergeAcceptsLegacyCRLFBase(t *testing.T) {
	const (
		originalLF = "alpha\nshared\nomega\n"
		baseCRLF   = "alpha\r\nshared\r\nomega\r\n"
		localCRLF  = "alpha-local\r\nshared\r\nomega\r\n"
		updatedLF  = "alpha\nshared\nomega-template\n"
		wantMerged = "alpha-local\nshared\nomega-template\n"
	)

	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	if err := newMergeTask(originalLF).Execute(tc); err != nil {
		t.Fatalf("initial execute: %v", err)
	}

	// Rewrite the base byte-exactly as CRLF and record the legacy raw hash.
	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")
	if err := os.WriteFile(basePath, []byte(baseCRLF), 0o600); err != nil {
		t.Fatalf("write legacy CRLF base: %v", err)
	}
	tc.Manifest.ManagedFiles["Makefile"] = rawContentHash([]byte(baseCRLF))
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(localCRLF), 0o600); err != nil {
		t.Fatalf("write local CRLF edit: %v", err)
	}

	out := captureStderr(t, func() {
		if err := newMergeTask(updatedLF).Execute(tc); err != nil {
			t.Fatalf("merge execute: %v", err)
		}
	})
	if strings.Contains(out, "No valid pristine base") {
		t.Fatalf("legacy CRLF base was rejected:\n%s", out)
	}

	got, err := os.ReadFile(filepath.Join(root, "Makefile")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read merged file: %v", err)
	}
	if string(got) != wantMerged {
		t.Fatalf("merged = %q, want %q", got, wantMerged)
	}
	if _, err := os.Stat(filepath.Join(root, "Makefile.scbake-new")); !os.IsNotExist(err) {
		t.Fatalf("legacy CRLF base should merge without an artifact; stat err = %v", err)
	}
	// The canonical write advances the ledger to the normalized template hash.
	if got, want := tc.Manifest.ManagedFiles["Makefile"], HashContent([]byte(updatedLF)); got != want {
		t.Fatalf("ledger = %q, want normalized new render %q", got, want)
	}
}

// TestMergeWholeFileCRLFEditIsClean covers the autocrlf/editor case: the local
// file was rewritten with CRLF and the template introduces a disjoint change.
// Normalizing all merge inputs must keep both edits instead of treating every
// line as modified.
func TestMergeWholeFileCRLFEditIsClean(t *testing.T) {
	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	if err := newMergeTask("alpha\nshared\nomega\n").Execute(tc); err != nil {
		t.Fatalf("initial execute: %v", err)
	}
	// Whole-file CRLF local edit with the first line changed.
	local := "alpha-local\r\nshared\r\nomega\r\n"
	if err := os.WriteFile(filepath.Join(root, "Makefile"), []byte(local), 0o600); err != nil {
		t.Fatalf("write local CRLF edit: %v", err)
	}
	// A CRLF template render as well, to exercise all three normalized inputs.
	if err := newMergeTask("alpha\r\nshared\r\nomega-template\r\n").Execute(tc); err != nil {
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
		t.Fatalf("whole-file CRLF edit should merge cleanly; stat err = %v", err)
	}
}
