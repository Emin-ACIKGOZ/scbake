// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/pkg/tasks"
)

// mustWrite writes body to path, failing the test on error.
func mustWrite(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// hashOf is a shorthand for the hash the ledger stores.
func hashOf(b []byte) string { return tasks.HashContent(b) }

// TestDetect_Clean: a file whose bytes hash to the recorded value is not drift.
func TestDetect_Clean(t *testing.T) {
	root := t.TempDir()
	content := []byte("generated\n")
	mustWrite(t, filepath.Join(root, "a.txt"), content)

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(content)}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("clean file reported as drift: %+v", got)
	}
}

// TestDetect_Modified is the acceptance mutation guard: the test fails if the
// hash comparison in inspect is mutated to always match, because a changed file
// would then be reported clean.
func TestDetect_Modified(t *testing.T) {
	root := t.TempDir()
	base := []byte("original\n")
	edited := []byte("hand-edited\n")
	mustWrite(t, filepath.Join(root, "a.txt"), edited)

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(base)}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("modified file not reported: %+v", got)
	}
	if got[0].Kind != Modified {
		t.Errorf("Kind = %q, want %q", got[0].Kind, Modified)
	}
	if got[0].Recorded != hashOf(base) {
		t.Errorf("Recorded = %q, want the recorded hash", got[0].Recorded)
	}
	if got[0].Actual != hashOf(edited) {
		t.Errorf("Actual = %q, want the on-disk hash", got[0].Actual)
	}
}

// TestDetect_Missing: a deleted managed file is reported Missing, not clean.
func TestDetect_Missing(t *testing.T) {
	root := t.TempDir()
	// A root project (".") exercises the legacy probe's skip of the
	// already-probed root path.
	m := &types.Manifest{
		Projects:     []types.Project{{Name: "root", Path: ".", Language: "go"}},
		ManagedFiles: map[string]string{"gone.txt": hashOf([]byte("x"))},
	}

	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Missing {
		t.Fatalf("deleted file = %+v, want one Missing finding", got)
	}
	if got[0].Actual != "" {
		t.Errorf("Missing finding carries an Actual hash: %q", got[0].Actual)
	}
}

// TestDetect_EmptyManifest: no ledger means no drift, and no error.
func TestDetect_EmptyManifest(t *testing.T) {
	for name, m := range map[string]*types.Manifest{
		"nil":   nil,
		"empty": {},
	} {
		got, err := Detect(m, t.TempDir())
		if err != nil || got != nil {
			t.Errorf("%s: Detect = (%v, %v), want (nil, nil)", name, got, err)
		}
	}
}

// TestDetect_Invariants pins I1 (completeness) and I7 (deterministic order):
// every ledger entry yields exactly one finding or is clean, and findings come
// back sorted so output and CI diffs are stable.
func TestDetect_Invariants(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "z.txt"), []byte("z\n"))
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("changed\n"))
	// m.txt is absent on purpose.

	m := &types.Manifest{ManagedFiles: map[string]string{
		"z.txt": hashOf([]byte("z\n")),       // clean
		"a.txt": hashOf([]byte("orig\n")),    // modified
		"m.txt": hashOf([]byte("missing\n")), // missing
	}}

	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("findings = %d, want 2 (clean entries excluded)", len(got))
	}
	if got[0].Path != "a.txt" || got[1].Path != "m.txt" {
		t.Errorf("findings not sorted by path: %+v", got)
	}
}

// TestDetect_ScopedSubprojects folds in SCB-057: two subprojects that generate
// the same relative filename get distinct ledger entries, so drift in one is
// reported for that key alone and never for its twin.
func TestDetect_ScopedSubprojects(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}

	contentA := []byte("license for alice\n")
	contentB := []byte("license for bob\n")
	m := &types.Manifest{}
	write := func(sub string, content []byte) {
		t.Helper()
		tc := types.TaskContext{TargetPath: filepath.Join(root, sub), Manifest: m, Tx: tx}
		if err := tasks.WriteManagedFile(tc, "LICENSE", content, 0o600); err != nil {
			t.Fatalf("WriteManagedFile(%s): %v", sub, err)
		}
	}
	write("services/a", contentA)
	write("services/b", contentB)

	// Pristine project: no drift.
	if got, _ := Detect(m, root); len(got) != 0 {
		t.Fatalf("freshly generated tree reports drift: %+v", got)
	}

	// Drift only subproject A.
	mustWrite(t, filepath.Join(root, "services", "a", "LICENSE"), []byte("alice edited\n"))
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Path != "services/a/LICENSE" {
		t.Fatalf("cross-project drift: %+v", got)
	}
}

// legacyProject builds a pre-SCB-057 manifest whose flat key points at a
// project-relative file, returning the root, manifest and pristine content.
func legacyProject(t *testing.T, projects ...types.Project) (string, *types.Manifest, []byte) {
	t.Helper()
	root := t.TempDir()
	content := []byte("legacy license\n")
	for _, p := range projects {
		mustWrite(t, filepath.Join(root, filepath.FromSlash(p.Path), "LICENSE"), content)
	}
	m := &types.Manifest{
		Projects:     projects,
		ManagedFiles: map[string]string{"LICENSE": hashOf(content)},
	}
	return root, m, content
}

// TestDetect_LegacyFlatKeyNotFalseMissing: a pre-SCB-057 manifest stores the
// project-relative key. The write path still honors it, so detection must too,
// or `drift` would report drift for a file `apply` considers clean.
func TestDetect_LegacyFlatKeyNotFalseMissing(t *testing.T) {
	root, m, _ := legacyProject(t, types.Project{Name: "a", Path: "services/a", Language: "go"})

	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("legacy managed file reported as drift: %+v", got)
	}
}

// TestDetect_LegacyFlatKeyEditedThenDeleted: an edited legacy file is Modified
// (not falsely Missing), and a deleted one is Missing.
func TestDetect_LegacyFlatKeyEditedThenDeleted(t *testing.T) {
	root, m, _ := legacyProject(t, types.Project{Name: "a", Path: "services/a", Language: "go"})
	license := filepath.Join(root, "services", "a", "LICENSE")

	mustWrite(t, license, []byte("alice edited\n"))
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect after edit: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Modified || got[0].Path != "LICENSE" {
		t.Fatalf("edited legacy file = %+v, want Modified", got)
	}

	if err := os.Remove(license); err != nil {
		t.Fatalf("remove: %v", err)
	}
	got, err = Detect(m, root)
	if err != nil {
		t.Fatalf("Detect after delete: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Missing {
		t.Fatalf("deleted legacy file = %+v, want Missing", got)
	}
}

// TestDetect_LegacyDivergenceInAnyProjectIsDrift: a legacy flat key is the
// baseline for every project that holds that filename, so a divergence in one
// project must not be masked by a pristine copy in another.
func TestDetect_LegacyDivergenceInAnyProjectIsDrift(t *testing.T) {
	root, m, _ := legacyProject(t,
		types.Project{Name: "a", Path: "services/a", Language: "go"},
		types.Project{Name: "b", Path: "services/b", Language: "go"},
	)
	mustWrite(t, filepath.Join(root, "services", "a", "LICENSE"), []byte("alice edited\n"))

	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Modified {
		t.Fatalf("divergence masked by a pristine sibling: %+v", got)
	}
}

// TestDetect_RejectsEscapingKey: a ledger key that escapes the root must fail
// closed, never read a file outside the project.
func TestDetect_RejectsEscapingKey(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "project")
	mustWrite(t, filepath.Join(root, "keep"), []byte("x"))
	outside := filepath.Join(parent, "outside.txt")
	mustWrite(t, outside, []byte("secret\n"))

	m := &types.Manifest{ManagedFiles: map[string]string{"../outside.txt": hashOf([]byte("secret\n"))}}
	got, err := Detect(m, root)
	if err == nil {
		t.Fatalf("escaping key was not rejected; got %+v", got)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("escaping key produced a NotExist error instead of a containment error: %v", err)
	}
}

// TestDetect_ReadOnly pins I3: detection performs no writes. The whole tree,
// including the manifest, must be byte-identical afterward.
func TestDetect_ReadOnly(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "scbake.toml"), []byte("scbake_version = \"v0.3.0\"\n"))
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("drifted\n"))

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf([]byte("orig\n"))}}
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

// TestDetect_AgreesWithApply pins I5: with an unchanged template, Detect calls
// a file clean exactly when the write path's `fail` strategy accepts it. Any
// divergence would be a new fault (drift and apply disagreeing).
func TestDetect_AgreesWithApply(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	content := []byte("shared\n")
	m := &types.Manifest{}
	tc := types.TaskContext{
		TargetPath:       root,
		Manifest:         m,
		Tx:               tx,
		ConflictStrategy: types.ConflictFail,
	}
	if err := tasks.WriteManagedFile(tc, "file.txt", content, 0o600); err != nil {
		t.Fatalf("WriteManagedFile: %v", err)
	}

	// Clean -> Detect silent, apply accepts.
	if got, _ := Detect(m, root); len(got) != 0 {
		t.Fatalf("clean tree reports drift: %+v", got)
	}
	if err := tasks.WriteManagedFile(tc, "file.txt", content, 0o600); err != nil {
		t.Fatalf("apply rejected a clean file: %v", err)
	}

	// Drifted -> Detect reports, apply fails.
	mustWrite(t, filepath.Join(root, "file.txt"), []byte("edited\n"))
	got, err := Detect(m, root)
	if err != nil || len(got) != 1 || got[0].Kind != Modified {
		t.Fatalf("Detect on drifted file = (%+v, %v), want one Modified", got, err)
	}
	if err := tasks.WriteManagedFile(tc, "file.txt", content, 0o600); err == nil {
		t.Fatal("apply accepted a drifted file under the fail strategy")
	}
}

// snapshot maps every path under root to a stable fingerprint (size+mtime).
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[path] = fmt.Sprintf("%d|%s", info.Size(), info.ModTime())
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return out
}

// FuzzDetect_RoundTrip is a property/invariant test: for arbitrary content, a
// fresh managed write is always clean, and changing a single byte always makes
// it Modified. It searches the input space instead of enumerating cases.
func FuzzDetect_RoundTrip(f *testing.F) {
	f.Add([]byte("seed\n"))
	f.Add([]byte{})
	f.Add([]byte("\x00\xff\n"))
	f.Fuzz(func(t *testing.T, content []byte) {
		if len(content) > 1<<16 {
			t.Skip()
		}
		root := t.TempDir()
		mustWrite(t, filepath.Join(root, "f.txt"), content)
		m := &types.Manifest{ManagedFiles: map[string]string{"f.txt": hashOf(content)}}

		if got, err := Detect(m, root); err != nil || len(got) != 0 {
			t.Fatalf("clean content reported drift: %+v (err %v)", got, err)
		}

		mutated := append(append([]byte{}, content...), 'x')
		mustWrite(t, filepath.Join(root, "f.txt"), mutated)
		got, err := Detect(m, root)
		if err != nil {
			t.Fatalf("Detect: %v", err)
		}
		if len(got) != 1 || got[0].Kind != Modified {
			t.Fatalf("mutated content = %+v, want one Modified", got)
		}
	})
}
