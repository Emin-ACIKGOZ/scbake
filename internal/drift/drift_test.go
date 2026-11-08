// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
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

// rawHashOf is the pre-normalization SHA-256 contract, used to model a legacy
// manifest recorded before EOL-insensitive hashing.
func rawHashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// writeBase writes the committed pristine base for a ledger key using the same
// shadow mapping as the write path.
func writeBase(t *testing.T, root, key string, content []byte) {
	t.Helper()
	mustWrite(t, filepath.Join(root, tasks.BaseShadowRelPath(key)), content)
}

// TestDetect_Clean: a file whose bytes hash to the recorded value is not drift.
func TestDetect_Clean(t *testing.T) {
	root := t.TempDir()
	content := []byte("generated\n")
	mustWrite(t, filepath.Join(root, "a.txt"), content)
	writeBase(t, root, "a.txt", content)

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
	writeBase(t, root, "a.txt", base)

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

// TestDetect_IgnoresEOLOnlyChurn pins the EOL-insensitive hash contract: a
// CRLF checkout or editor save of text is not drift when the recorded render is
// its LF form. This is what keeps an autocrlf clone from reporting every file.
func TestDetect_IgnoresEOLOnlyChurn(t *testing.T) {
	root := t.TempDir()
	lf := []byte("alpha\nshared\nomega\n")
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("alpha\r\nshared\r\nomega\r\n"))
	writeBase(t, root, "a.txt", lf)

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(lf)}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("EOL-only churn reported as drift: %+v", got)
	}
}

// TestDetect_LegacyCRLFHashIsOneTimeDrift documents the chosen compatibility
// behavior for manifests written before EOL-insensitive hashing: a recorded raw
// CRLF hash no longer matches the normalized hash and is reported once, then a
// canonical write records the normalized hash and the file is clean.
func TestDetect_LegacyCRLFHashIsOneTimeDrift(t *testing.T) {
	root := t.TempDir()
	crlf := []byte("alpha\r\nshared\r\nomega\r\n")
	mustWrite(t, filepath.Join(root, "a.txt"), crlf)
	writeBase(t, root, "a.txt", crlf)

	legacy := &types.Manifest{ManagedFiles: map[string]string{"a.txt": rawHashOf(crlf)}}
	got, err := Detect(legacy, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Modified {
		t.Fatalf("legacy CRLF hash = %+v, want one Modified finding", got)
	}
	if got[0].Recorded != rawHashOf(crlf) || got[0].Actual != hashOf(crlf) {
		t.Fatalf("legacy finding = %+v, want raw recorded and normalized actual", got[0])
	}

	// Re-recording with the normalizing hash (the next canonical write) clears
	// the one-time drift.
	canonical := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(crlf)}}
	if got, err := Detect(canonical, root); err != nil || len(got) != 0 {
		t.Fatalf("canonical re-record still drifts: %+v (err %v)", got, err)
	}
}

// TestDetect_LegacyLFHashIsClean is the negative control: a legacy raw hash of
// LF text is identical to the normalized hash, so ordinary manifests do not
// report a migration-time drift event.
func TestDetect_LegacyLFHashIsClean(t *testing.T) {
	root := t.TempDir()
	lf := []byte("alpha\nshared\nomega\n")
	mustWrite(t, filepath.Join(root, "a.txt"), lf)
	writeBase(t, root, "a.txt", lf)

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": rawHashOf(lf)}}
	if got, err := Detect(m, root); err != nil || len(got) != 0 {
		t.Fatalf("legacy LF manifest reported drift: %+v (err %v)", got, err)
	}
}

// TestDetect_CommittedBaseLocation refreshes the drift fixture for the #143
// storage move: the write path places pristine bases under .scbake-base and
// never re-creates the legacy ignored .scbake/base tree, and drift remains
// clean on that freshly generated layout.
func TestDetect_CommittedBaseLocation(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	m := &types.Manifest{}
	tc := types.TaskContext{
		TargetPath:       root,
		Manifest:         m,
		Tx:               tx,
		ConflictStrategy: types.ConflictFail,
	}
	content := []byte("generated\n")
	if err := tasks.WriteManagedFile(tc, "a.txt", content, 0o600); err != nil {
		t.Fatalf("WriteManagedFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir, "a.txt")); err != nil {
		t.Fatalf("committed base missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir, fileutil.BaseDir)); !os.IsNotExist(err) {
		t.Fatalf("legacy .scbake/base must not exist (stat err: %v)", err)
	}
	if got, err := Detect(m, root); err != nil || len(got) != 0 {
		t.Fatalf("fresh committed-base layout reports drift: %+v (err %v)", got, err)
	}
}

// TestDetect_BaseMissing: a clean working file with no committed base is
// reported as BaseMissing with the expected shadow path.
func TestDetect_BaseMissing(t *testing.T) {
	root := t.TempDir()
	content := []byte("generated\n")
	mustWrite(t, filepath.Join(root, "a.txt"), content)

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(content)}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != BaseMissing {
		t.Fatalf("missing base = %+v, want one BaseMissing finding", got)
	}
	if got[0].Path != "a.txt" {
		t.Errorf("Path = %q, want a.txt", got[0].Path)
	}
	if want := filepath.ToSlash(tasks.BaseShadowRelPath("a.txt")); got[0].BasePath != want {
		t.Errorf("BasePath = %q, want %q", got[0].BasePath, want)
	}
	if got[0].Recorded != hashOf(content) || got[0].Actual != "" {
		t.Errorf("finding hashes = %+v, want recorded hash and empty actual", got[0])
	}
}

// TestDetect_BaseMissingPerKeyWhenTreeAbsent: an absent .scbake-base tree still
// yields one BaseMissing finding per current ledger key, not a collapsed
// directory-level result.
func TestDetect_BaseMissingPerKeyWhenTreeAbsent(t *testing.T) {
	root := t.TempDir()
	m := &types.Manifest{ManagedFiles: map[string]string{}}
	for i := range 7 {
		key := fmt.Sprintf("file%d.txt", i)
		content := []byte(key + "\n")
		mustWrite(t, filepath.Join(root, key), content)
		m.ManagedFiles[key] = hashOf(content)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir)); !os.IsNotExist(err) {
		t.Fatalf("fixture unexpectedly has a committed base tree (stat err: %v)", err)
	}

	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 7 {
		t.Fatalf("findings = %d, want one per key (7): %+v", len(got), got)
	}
	for i, f := range got {
		wantPath := fmt.Sprintf("file%d.txt", i)
		if f.Kind != BaseMissing || f.Path != wantPath {
			t.Errorf("finding %d = (%q, %q), want (%q, base-missing)", i, f.Path, f.Kind, wantPath)
		}
	}
}

// TestDetect_BaseInvalid: a base whose neither normalized nor raw hash matches
// the ledger is reported as BaseInvalid with the base hash.
func TestDetect_BaseInvalid(t *testing.T) {
	root := t.TempDir()
	content := []byte("generated\n")
	stale := []byte("stale base\n")
	mustWrite(t, filepath.Join(root, "a.txt"), content)
	writeBase(t, root, "a.txt", stale)

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(content)}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != BaseInvalid {
		t.Fatalf("stale base = %+v, want one BaseInvalid finding", got)
	}
	if want := filepath.ToSlash(tasks.BaseShadowRelPath("a.txt")); got[0].BasePath != want {
		t.Errorf("BasePath = %q, want %q", got[0].BasePath, want)
	}
	if got[0].Recorded != hashOf(content) || got[0].Actual != hashOf(stale) {
		t.Errorf("finding hashes = %+v, want recorded and base hashes", got[0])
	}
}

// TestDetect_CRLFBaseIsValid pins the shared merge predicate: a CRLF base is
// accepted when its EOL-normalized hash matches the current ledger hash.
func TestDetect_CRLFBaseIsValid(t *testing.T) {
	root := t.TempDir()
	lf := []byte("alpha\nshared\nomega\n")
	crlf := []byte("alpha\r\nshared\r\nomega\r\n")
	mustWrite(t, filepath.Join(root, "a.txt"), lf)
	writeBase(t, root, "a.txt", crlf)

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(lf)}}
	if got, err := Detect(m, root); err != nil || len(got) != 0 {
		t.Fatalf("CRLF base reported drift: %+v (err %v)", got, err)
	}
}

// TestDetect_ShadowNameBaseIsValid verifies base validation uses the same
// shadow mapping as the write path for Git-special names at any depth.
func TestDetect_ShadowNameBaseIsValid(t *testing.T) {
	root := t.TempDir()
	content := []byte("dist/\n")
	mustWrite(t, filepath.Join(root, "sub", ".gitignore"), content)
	writeBase(t, root, "sub/.gitignore", content)

	m := &types.Manifest{ManagedFiles: map[string]string{"sub/.gitignore": hashOf(content)}}
	if got, err := Detect(m, root); err != nil || len(got) != 0 {
		t.Fatalf("shadow-name base reported drift: %+v (err %v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir, "sub", ".gitignore")); !os.IsNotExist(err) {
		t.Errorf("unshadowed base path exists (stat err: %v)", err)
	}
}

// TestDetect_MonorepoScopedBases validates scoped keys independently: a valid
// base is clean and a removed sibling base is BaseMissing for that key alone.
func TestDetect_MonorepoScopedBases(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	m := &types.Manifest{}
	write := func(sub, name string, content []byte) {
		t.Helper()
		tc := types.TaskContext{TargetPath: filepath.Join(root, sub), Manifest: m, Tx: tx}
		if err := tasks.WriteManagedFile(tc, name, content, 0o600); err != nil {
			t.Fatalf("WriteManagedFile(%s): %v", sub, err)
		}
	}
	write("services/a", "LICENSE", []byte("license for alice\n"))
	write("services/b", "LICENSE", []byte("license for bob\n"))

	if got, err := Detect(m, root); err != nil || len(got) != 0 {
		t.Fatalf("freshly generated tree reports drift: %+v (err %v)", got, err)
	}

	if err := os.Remove(filepath.Join(root, tasks.BaseShadowRelPath("services/b/LICENSE"))); err != nil {
		t.Fatalf("remove base for services/b: %v", err)
	}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != BaseMissing || got[0].Path != "services/b/LICENSE" {
		t.Fatalf("scoped missing base = %+v, want BaseMissing for services/b/LICENSE", got)
	}
}

// TestDetect_BaseReadErrorFailsClosed: any base read error other than ENOENT is
// an error, never BaseInvalid or BaseMissing.
func TestDetect_BaseReadErrorFailsClosed(t *testing.T) {
	root := t.TempDir()
	content := []byte("generated\n")
	mustWrite(t, filepath.Join(root, "a.txt"), content)
	if err := os.MkdirAll(filepath.Join(root, tasks.BaseShadowRelPath("a.txt")), 0o750); err != nil {
		t.Fatalf("create directory at base path: %v", err)
	}

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(content)}}
	got, err := Detect(m, root)
	if err == nil {
		t.Fatalf("base read error was not surfaced; findings = %+v", got)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("base read error was reported as NotExist: %v", err)
	}
	if !strings.Contains(err.Error(), "inspecting committed base") {
		t.Errorf("error = %v, want an inspecting-committed-base error", err)
	}
}

// TestDetect_SortsByPathThenKind pins deterministic ordering when one managed
// file has both working-tree and base findings.
func TestDetect_SortsByPathThenKind(t *testing.T) {
	root := t.TempDir()
	content := []byte("original\n")
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("edited\n"))
	writeBase(t, root, "a.txt", []byte("stale base\n"))
	mustWrite(t, filepath.Join(root, "b.txt"), content)

	m := &types.Manifest{ManagedFiles: map[string]string{
		"a.txt": hashOf(content),
		"b.txt": hashOf(content),
	}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	want := []struct {
		path string
		kind Kind
	}{
		{"a.txt", BaseInvalid},
		{"a.txt", Modified},
		{"b.txt", BaseMissing},
	}
	if len(got) != len(want) {
		t.Fatalf("findings = %+v, want %d", got, len(want))
	}
	for i := range want {
		if got[i].Path != want[i].path || got[i].Kind != want[i].kind {
			t.Errorf("finding %d = (%q, %q), want (%q, %q)",
				i, got[i].Path, got[i].Kind, want[i].path, want[i].kind)
		}
	}
}

// TestDetect_EmptyRecordedSkipsBase: a malformed empty ledger hash is not a
// base-invalid finding and no base read is attempted.
func TestDetect_EmptyRecordedSkipsBase(t *testing.T) {
	root := t.TempDir()
	content := []byte("generated\n")
	mustWrite(t, filepath.Join(root, "a.txt"), content)
	// A directory at the base path would make any base read fail, so a clean
	// result proves the base is not inspected for an empty recorded hash.
	if err := os.MkdirAll(filepath.Join(root, tasks.BaseShadowRelPath("a.txt")), 0o750); err != nil {
		t.Fatalf("create directory at base path: %v", err)
	}

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": ""}}
	got, err := Detect(m, root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(got) != 1 || got[0].Kind != Modified {
		t.Fatalf("empty recorded hash findings = %+v, want only one Modified", got)
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
	writeBase(t, root, "gone.txt", []byte("x"))

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
	writeBase(t, root, "z.txt", []byte("z\n"))
	writeBase(t, root, "a.txt", []byte("orig\n"))
	writeBase(t, root, "m.txt", []byte("missing\n"))

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
	// No version wrote a base at the flat key, so a corrupt base there must not
	// turn a legacy-resolved entry into a base finding.
	writeBase(t, root, "LICENSE", []byte("corrupt base\n"))

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
	root, m, content := legacyProject(t, types.Project{Name: "a", Path: "services/a", Language: "go"})
	writeBase(t, root, "LICENSE", content)
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
	corruptBase := []byte("corrupt base\n")
	writeBase(t, root, "a.txt", corruptBase)

	m := &types.Manifest{ManagedFiles: map[string]string{
		"a.txt": hashOf([]byte("orig\n")),
		"b.txt": hashOf([]byte("b\n")), // committed base is absent
	}}
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
	//nolint:gosec // test-controlled temp path
	if got, err := os.ReadFile(filepath.Join(root, tasks.BaseShadowRelPath("a.txt"))); err != nil {
		t.Fatalf("read corrupt base: %v", err)
	} else if string(got) != string(corruptBase) {
		t.Errorf("Detect repaired the corrupt base: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, tasks.BaseShadowRelPath("b.txt"))); !os.IsNotExist(err) {
		t.Errorf("Detect created a missing base (stat err: %v)", err)
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
		writeBase(t, root, "f.txt", content)
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
