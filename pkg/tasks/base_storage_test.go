// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"scbake/internal/util/fileutil"
)

// rawSHA256 is the pre-normalization hash contract. Tests use it to prove that
// binary content still hashes byte-exactly.
func rawSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// RequireNoLegacyBase fails if the pre-#143 ignored base path exists.
func requireNoLegacyBase(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir, fileutil.BaseDir)); !os.IsNotExist(err) {
		t.Fatalf("legacy .scbake/base must not exist (stat err: %v)", err)
	}
}

// TestCommittedBasePathLayout pins the storage move: the pristine base lives at
// <root>/.scbake-base/<ledgerKey>, never under the ignored .scbake state.
func TestCommittedBasePathLayout(t *testing.T) {
	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	if err := newMergeTask("alpha\nshared\nomega\n").Execute(tc); err != nil {
		t.Fatalf("initial execute: %v", err)
	}

	basePath := filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")
	got, err := os.ReadFile(basePath) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("committed base missing at %s: %v", basePath, err)
	}
	if string(got) != "alpha\nshared\nomega\n" {
		t.Fatalf("base = %q, want the pristine render", got)
	}
	requireNoLegacyBase(t, root)

	// A nested ledger key mirrors its slash-separated path under the base root.
	if err := WriteManagedFile(tc, filepath.Join("sub", "Makefile"), []byte("nested\n"), 0o600); err != nil {
		t.Fatalf("WriteManagedFile nested: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir, "sub", "Makefile")); err != nil {
		t.Fatalf("nested committed base missing: %v", err)
	}
	requireNoLegacyBase(t, root)
}

// TestBaseShadowKeyMapping pins the Git-special shadow mapping at every depth.
func TestBaseShadowKeyMapping(t *testing.T) {
	const suffix = fileutil.BaseShadowSuffix
	cases := map[string]string{
		"Makefile":                "Makefile",
		".gitignore":              ".gitignore" + suffix,
		".gitattributes":          ".gitattributes" + suffix,
		"sub/.gitignore":          "sub/.gitignore" + suffix,
		"sub/deep/.gitattributes": "sub/deep/.gitattributes" + suffix,
		"sub/normal.txt":          "sub/normal.txt",
		".editorconfig":           ".editorconfig",
		// Only whole path components are Git-special; a filename that merely
		// contains the marker is not renamed.
		"sub/my.gitignore": "sub/my.gitignore",
	}
	for in, want := range cases {
		if got := baseShadowKey(in); got != want {
			t.Errorf("baseShadowKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBaseShadowReadWriteRoundTrip proves the write and read paths share the
// sanitized mapping for root and nested Git-special names, so an update can
// always find the base it wrote.
func TestBaseShadowReadWriteRoundTrip(t *testing.T) {
	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	cases := []struct {
		key     string
		shadow  string
		content []byte
	}{
		{key: ".gitignore", shadow: ".gitignore" + fileutil.BaseShadowSuffix, content: []byte("*.out\n")},
		{key: ".gitattributes", shadow: ".gitattributes" + fileutil.BaseShadowSuffix, content: []byte("* text eol=crlf\n")},
		{key: "sub/.gitignore", shadow: "sub/.gitignore" + fileutil.BaseShadowSuffix, content: []byte("dist/\n")},
		{key: "sub/deep/.gitattributes", shadow: "sub/deep/.gitattributes" + fileutil.BaseShadowSuffix, content: []byte("*.txt -text\n")},
		{key: "sub/normal.txt", shadow: "sub/normal.txt", content: []byte("plain\n")},
	}

	for _, tt := range cases {
		if err := writeBaseShadow(tc, tt.key, tt.content); err != nil {
			t.Fatalf("writeBaseShadow(%q): %v", tt.key, err)
		}
		got, ok := readBaseShadow(tc, tt.key)
		if !ok {
			t.Fatalf("readBaseShadow(%q) did not find the written base", tt.key)
		}
		if !bytes.Equal(got, tt.content) {
			t.Fatalf("readBaseShadow(%q) = %q, want %q", tt.key, got, tt.content)
		}
		if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir, filepath.FromSlash(tt.shadow))); err != nil {
			t.Fatalf("expected shadow %q under committed base: %v", tt.shadow, err)
		}
		if tt.shadow != tt.key {
			if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir, filepath.FromSlash(tt.key))); !os.IsNotExist(err) {
				t.Fatalf("raw Git-special name %q must not exist in the base tree (stat err: %v)", tt.key, err)
			}
		}
	}
	requireNoLegacyBase(t, root)
}

// TestWriteBaseShadowPreservesPristineBytes pins the split contract: the base
// stores the rendered bytes exactly, while hashing normalizes text EOLs.
func TestWriteBaseShadowPreservesPristineBytes(t *testing.T) {
	root, tx, tc := mergeTestContext(t)
	defer func() { _ = tx.Rollback() }()

	crlf := []byte("alpha\r\nshared\r\nomega\r\n")
	if err := writeBaseShadow(tc, "Makefile", crlf); err != nil {
		t.Fatalf("writeBaseShadow: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("read base: %v", err)
	}
	if !bytes.Equal(got, crlf) {
		t.Fatalf("base bytes = %q, want pristine CRLF %q", got, crlf)
	}
	if HashContent(got) != HashContent([]byte("alpha\nshared\nomega\n")) {
		t.Fatal("CRLF base must hash like its LF form")
	}
	requireNoLegacyBase(t, root)
}

// TestCommittedBaseRollback proves a failed transaction removes the base files
// and the .scbake-base tree it created, without deleting pre-existing .scbake
// state (the transaction journal and temp backups live there).
func TestCommittedBaseRollback(t *testing.T) { //nolint:cyclop // assertion-heavy rollback regression test
	root, tx, tc := mergeTestContext(t)

	sentinel := filepath.Join(root, fileutil.InternalDir, "sentinel")
	if err := os.MkdirAll(filepath.Dir(sentinel), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sentinel, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
		t.Fatalf("writeBaseShadow Makefile: %v", err)
	}
	if err := writeBaseShadow(tc, "sub/Go.mod", []byte("module x\n")); err != nil {
		t.Fatalf("writeBaseShadow sub/Go.mod: %v", err)
	}
	for _, rel := range []string{
		filepath.Join(fileutil.CommittedBaseDir, "Makefile"),
		filepath.Join(fileutil.CommittedBaseDir, "sub", "Go.mod"),
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected base file %s: %v", rel, err)
		}
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir)); !os.IsNotExist(err) {
		t.Fatalf("rollback left committed base tree (stat err: %v)", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("rollback removed pre-existing .scbake state: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir)); err != nil {
		t.Fatalf("rollback removed the existing .scbake tree: %v", err)
	}
}

// TestHashContentEOLContract pins the manifest hash contract: NUL-free text is
// EOL-insensitive, a run of CR bytes before LF projects to one LF, NUL-bearing
// binary content is byte-exact, and repeated normalization is idempotent.
func TestHashContentEOLContract(t *testing.T) { //nolint:cyclop // assertion-heavy contract test
	lf := []byte("alpha\nshared\nomega\n")
	crlf := []byte("alpha\r\nshared\r\nomega\r\n")
	mixed := []byte("alpha\r\nshared\nomega\r\n")

	if HashContent(lf) != HashContent(crlf) {
		t.Fatal("text CRLF must hash like LF")
	}
	if HashContent(lf) != HashContent(mixed) {
		t.Fatal("mixed CRLF/LF text must hash like LF")
	}
	if HashContent([]byte("")) != HashContent(nil) {
		t.Fatal("empty and nil content must hash identically")
	}
	if HashContent([]byte("a\rb\n")) == HashContent([]byte("a\nb\n")) {
		t.Fatal("a lone CR is content and must remain distinct")
	}

	// CR runs immediately before LF are one line ending and project to a single
	// LF. Runs that are not followed by LF are literal CR content.
	crRuns := map[string]string{
		"\r\r\n":     "\n",
		"\r\r\r\n":   "\n",
		"x\r\r\ny\r": "x\ny\r",
		"\r\r":       "\r\r",
		"\r":         "\r",
	}
	for in, want := range crRuns {
		if got := string(normalizeEOL([]byte(in))); got != want {
			t.Errorf("normalizeEOL(%q) = %q, want %q", in, got, want)
		}
	}
	if HashContent([]byte("a\r\r\nb\r\r\r\n")) != HashContent([]byte("a\nb\n")) {
		t.Fatal("a CR run before LF must hash like a single LF")
	}

	// NUL-free binary that happens to contain CRLF is treated as text by the
	// documented contract: only NUL is the binary marker.
	nulFreeBinary := []byte("\x89PNG\r\n\x1a\npayload\r\n")
	nulFreeBinaryLF := []byte("\x89PNG\n\x1a\npayload\n")
	if got, want := string(normalizeEOL(nulFreeBinary)), string(nulFreeBinaryLF); got != want {
		t.Fatalf("NUL-free binary normalization = %q, want %q", got, want)
	}
	if HashContent(nulFreeBinary) != HashContent(nulFreeBinaryLF) {
		t.Fatal("NUL-free binary containing CRLF must follow the text hash contract")
	}

	binary := []byte("head\x00body\r\n")
	if got, want := HashContent(binary), rawSHA256(binary); got != want {
		t.Fatalf("NUL content hash = %q, want byte-exact %q", got, want)
	}
	if HashContent(binary) == HashContent([]byte("head\x00body\n")) {
		t.Fatal("NUL content must not be EOL-normalized")
	}

	unicode := []byte("héllo\r\n世界\r\n")
	if HashContent(unicode) != HashContent([]byte("héllo\n世界\n")) {
		t.Fatal("non-ASCII text CRLF must hash like LF")
	}

	for _, content := range [][]byte{
		lf, crlf, mixed, nulFreeBinary, binary, unicode,
		[]byte("\r\r\n"), []byte("x\r\r\ny\r"), nil,
	} {
		normalized := normalizeEOL(content)
		if !bytes.Equal(normalized, normalizeEOL(normalized)) {
			t.Fatalf("normalizeEOL is not idempotent for %q", content)
		}
		if got, want := HashContent(content), HashContent(normalized); got != want {
			t.Fatalf("hash changed after normalization for %q: %q != %q", content, got, want)
		}
	}
}
