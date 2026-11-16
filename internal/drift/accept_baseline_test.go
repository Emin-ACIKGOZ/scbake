// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"os"
	"path/filepath"
	"testing"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// TestDetectAcceptedBaselineClean: accepted bytes are clean even though they
// differ from the pristine managed hash; the committed base stays pristine.
func TestDetectAcceptedBaselineClean(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	edited := []byte("local edit\n")
	mustWrite(t, filepath.Join(root, "a.txt"), edited)
	writeBase(t, root, "a.txt", pristine)

	m := &types.Manifest{
		ManagedFiles:  map[string]string{"a.txt": hashOf(pristine)},
		AcceptedFiles: map[string]string{"a.txt": hashOf(edited)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("accepted baseline reported drift: %+v", got)
	}
}

// TestDetectAcceptedBaselineModifiedAfterAccept: a later edit drifts against the
// accepted hash and reports the pristine hash separately.
func TestDetectAcceptedBaselineModifiedAfterAccept(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	accepted := []byte("accepted\n")
	second := []byte("second edit\n")
	mustWrite(t, filepath.Join(root, "a.txt"), second)
	writeBase(t, root, "a.txt", pristine)

	m := &types.Manifest{
		ManagedFiles:  map[string]string{"a.txt": hashOf(pristine)},
		AcceptedFiles: map[string]string{"a.txt": hashOf(accepted)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("post-accept edit not reported: %+v", got)
	}
	f := got[0]
	if f.Kind != Modified {
		t.Errorf("Kind = %q, want %q", f.Kind, Modified)
	}
	if !f.Accepted {
		t.Errorf("Accepted = false, want true")
	}
	if f.Recorded != hashOf(pristine) {
		t.Errorf("Recorded = %q, want the pristine hash", f.Recorded)
	}
	if f.Baseline != hashOf(accepted) {
		t.Errorf("Baseline = %q, want the accepted hash", f.Baseline)
	}
	if f.Actual != hashOf(second) {
		t.Errorf("Actual = %q, want the second edit hash", f.Actual)
	}
}

// TestDetectAcceptedDoesNotHideBaseInvalid: an invalid committed base remains
// blocking even when the working tree matches the accepted baseline.
func TestDetectAcceptedDoesNotHideBaseInvalid(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	edited := []byte("accepted\n")
	mustWrite(t, filepath.Join(root, "a.txt"), edited)
	mustWrite(t, filepath.Join(root, committedBasePath("a.txt")), []byte("corrupt\n"))

	m := &types.Manifest{
		ManagedFiles:  map[string]string{"a.txt": hashOf(pristine)},
		AcceptedFiles: map[string]string{"a.txt": hashOf(edited)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != BaseInvalid {
		t.Fatalf("base-invalid masked by acceptance: %+v", got)
	}
	if got[0].Recorded != hashOf(pristine) {
		t.Errorf("base finding recorded = %q, want pristine", got[0].Recorded)
	}
}

// TestDetectAcceptedMissingFile: deleting an accepted file is drift and the
// finding reports the accepted baseline.
func TestDetectAcceptedMissingFile(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	accepted := []byte("accepted\n")
	writeBase(t, root, "a.txt", pristine)

	m := &types.Manifest{
		ManagedFiles:  map[string]string{"a.txt": hashOf(pristine)},
		AcceptedFiles: map[string]string{"a.txt": hashOf(accepted)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Missing {
		t.Fatalf("missing accepted file not reported: %+v", got)
	}
	if !got[0].Accepted || got[0].Baseline != hashOf(accepted) {
		t.Errorf("missing finding lost the accepted baseline: %+v", got[0])
	}
}

// TestDetectAcceptedWithMissingBase: a matching accepted tree is clean by
// default but still reports the advisory missing base.
func TestDetectAcceptedWithMissingBase(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	edited := []byte("accepted\n")
	mustWrite(t, filepath.Join(root, "a.txt"), edited)

	m := &types.Manifest{
		ManagedFiles:  map[string]string{"a.txt": hashOf(pristine)},
		AcceptedFiles: map[string]string{"a.txt": hashOf(edited)},
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != BaseMissing {
		t.Fatalf("missing base not reported: %+v", got)
	}
}

// TestDetectOrphanAcceptedKeyIgnored: accepted state for a key that is no longer
// managed cannot make an untracked file drift-gated.
func TestDetectOrphanAcceptedKeyIgnored(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "orphan.txt"), []byte("x"))

	m := &types.Manifest{AcceptedFiles: map[string]string{"orphan.txt": hashOf([]byte("x"))}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("orphan accepted entry produced findings: %+v", got)
	}
}

// TestDetectReadOnlyWithAcceptedState pins that accepted baselines do not turn
// detection into a mutation or lock-taking path.
func TestDetectReadOnlyWithAcceptedState(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	edited := []byte("accepted\n")
	mustWrite(t, filepath.Join(root, "scbake.toml"), []byte("scbake_version = \"v0.3.0\"\n"))
	mustWrite(t, filepath.Join(root, "a.txt"), edited)
	writeBase(t, root, "a.txt", pristine)

	m := &types.Manifest{
		ManagedFiles:   map[string]string{"a.txt": hashOf(pristine)},
		AcceptedFiles:  map[string]string{"a.txt": hashOf(staleAcceptedContent())},
		UnmanagedFiles: []string{"old.txt"},
	}
	// The accepted hash deliberately does not match, so detection reports drift
	// and the read-only guarantee is exercised on the dirty path too.
	mustWrite(t, filepath.Join(root, "old.txt"), []byte("detached\n"))

	before := snapshot(t, root)
	if _, err := Detect(m, root); err != nil {
		t.Fatalf("Detect: %v", err)
	}
	after := snapshot(t, root)

	if len(before) != len(after) {
		t.Fatalf("tree changed: %d entries before, %d after", len(before), len(after))
	}
	for path, want := range before {
		if got := after[path]; got != want {
			t.Errorf("%s changed: %q -> %q", path, want, got)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("Detect created .scbake state")
	}
}

// staleAcceptedContent returns bytes whose hash differs from both the pristine and
// edited fixtures, keeping the read-only test on the drift path.
func staleAcceptedContent() []byte { return []byte("stale accepted\n") }

// committedBasePath keeps the committed-base location in one place for the
// drift fixtures without aliasing the production helper name.
func committedBasePath(key string) string {
	return filepath.Join(fileutil.CommittedBaseDir, filepath.FromSlash(key))
}
