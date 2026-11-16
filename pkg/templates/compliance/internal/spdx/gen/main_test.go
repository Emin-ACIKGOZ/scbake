// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type fixtureLicense struct {
	ID string `json:"licenseId"`
}

type fixtureList struct {
	Version  string           `json:"licenseListVersion"`
	Licenses []fixtureLicense `json:"licenses"`
}

// writeLicenseSource creates a local license-list-data checkout for -source.
// Detail files omit licenseId so the fixture also works on case-insensitive
// filesystems, where "MIT.json" and "mit.json" cannot coexist.
func writeLicenseSource(t *testing.T, texts map[string]string) string {
	t.Helper()

	root := t.TempDir()
	detailDir := filepath.Join(root, "json", "details")
	if err := os.MkdirAll(detailDir, 0o750); err != nil {
		t.Fatalf("create fixture details dir: %v", err)
	}

	ids := make([]string, 0, len(texts))
	for id := range texts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	// Write the fixture list in reverse order so the generator's sort is
	// observable in the output.
	slices.Reverse(ids)

	list := fixtureList{Version: "test-1.0"}
	for _, id := range ids {
		list.Licenses = append(list.Licenses, fixtureLicense{ID: id})
	}
	listBytes, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal fixture license list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "json", "licenses.json"), listBytes, 0o600); err != nil {
		t.Fatalf("write fixture license list: %v", err)
	}

	for _, id := range ids {
		detailBytes, err := json.Marshal(map[string]string{"licenseText": texts[id]})
		if err != nil {
			t.Fatalf("marshal fixture license %q: %v", id, err)
		}
		if err := os.WriteFile(filepath.Join(detailDir, id+".json"), detailBytes, 0o600); err != nil {
			t.Fatalf("write fixture license %q: %v", id, err)
		}
	}

	return root
}

// writeRawLicenseList creates a local -source checkout from a literal
// licenses.json body, so tests can exercise shapes the helper never emits.
func writeRawLicenseList(t *testing.T, listJSON string) string {
	t.Helper()

	root := t.TempDir()
	jsonDir := filepath.Join(root, "json")
	if err := os.MkdirAll(jsonDir, 0o750); err != nil {
		t.Fatalf("create fixture json dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(jsonDir, "licenses.json"), []byte(listJSON), 0o600); err != nil {
		t.Fatalf("write fixture license list: %v", err)
	}
	return root
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // test-controlled fixture path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func assertFileContents(t *testing.T, path string, want []byte) {
	t.Helper()

	got := readFile(t, path)
	if !bytes.Equal(got, want) {
		t.Errorf("%s = %q, want %q", path, got, want)
	}
}

// TestRunLeavesOutputsUntouchedOnCaseInsensitiveCollision is the regression for
// the generator writing licenses.zip before index rendering rejects colliding
// IDs. Both outputs must be unchanged, whether they already exist or not.
func TestRunLeavesOutputsUntouchedOnCaseInsensitiveCollision(t *testing.T) {
	source := writeLicenseSource(t, map[string]string{
		"MIT": "MIT License\n",
		"mit": "MIT License\n",
	})

	outDir := t.TempDir()
	archivePath := filepath.Join(outDir, "licenses.zip")
	indexPath := filepath.Join(outDir, "index.go")
	archiveSentinel := []byte("existing archive\n")
	indexSentinel := []byte("existing index\n")
	if err := os.WriteFile(archivePath, archiveSentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, indexSentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config{sourceDir: source, outPath: archivePath, indexPath: indexPath}
	err := run(context.Background(), cfg)
	if err == nil {
		t.Fatal("run succeeded with colliding license IDs; want an error")
	}
	if !strings.Contains(err.Error(), "collide case-insensitively") {
		t.Errorf("run error = %v, want a case-insensitive collision", err)
	}

	assertFileContents(t, archivePath, archiveSentinel)
	assertFileContents(t, indexPath, indexSentinel)

	freshDir := t.TempDir()
	freshArchive := filepath.Join(freshDir, "licenses.zip")
	freshIndex := filepath.Join(freshDir, "index.go")
	cfg = config{sourceDir: source, outPath: freshArchive, indexPath: freshIndex}
	if err := run(context.Background(), cfg); err == nil {
		t.Fatal("run succeeded with colliding license IDs; want an error")
	}
	for _, path := range []string{freshArchive, freshIndex} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Errorf("collision created %s (stat err: %v)", path, statErr)
		}
	}
}

// TestRunRejectsEmptyLicenseListWithoutWriting guards against generating an
// empty archive and index from a raw local source with "licenses": [].
func TestRunRejectsEmptyLicenseListWithoutWriting(t *testing.T) {
	source := writeRawLicenseList(t, `{"licenseListVersion":"test-1.0","licenses":[]}`)

	outDir := t.TempDir()
	archivePath := filepath.Join(outDir, "licenses.zip")
	indexPath := filepath.Join(outDir, "index.go")
	archiveSentinel := []byte("existing archive\n")
	indexSentinel := []byte("existing index\n")
	if err := os.WriteFile(archivePath, archiveSentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, indexSentinel, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config{sourceDir: source, outPath: archivePath, indexPath: indexPath}
	err := run(context.Background(), cfg)
	if err == nil {
		t.Fatal("run succeeded with an empty license list; want an error")
	}
	if !strings.Contains(err.Error(), "no licenses") {
		t.Errorf("run error = %v, want it to report no licenses", err)
	}
	assertFileContents(t, archivePath, archiveSentinel)
	assertFileContents(t, indexPath, indexSentinel)

	freshDir := t.TempDir()
	cfg = config{
		sourceDir: source,
		outPath:   filepath.Join(freshDir, "licenses.zip"),
		indexPath: filepath.Join(freshDir, "index.go"),
	}
	if err := run(context.Background(), cfg); err == nil {
		t.Fatal("run succeeded with an empty license list; want an error")
	}
	entries, err := os.ReadDir(freshDir)
	if err != nil {
		t.Fatalf("read fresh output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("empty license list created %d entries in a fresh output dir: %v", len(entries), entries)
	}
}

// archiveEntry is the version-independent view of a ZIP entry. Comparing these
// avoids compressed bytes, whose deflate output can differ across Go toolchains.
type archiveEntry struct {
	Name     string
	Method   uint16
	Modified string
	Mode     os.FileMode
	Content  string
}

func readArchiveEntries(t *testing.T, path string) []archiveEntry {
	t.Helper()

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open generated archive: %v", err)
	}
	defer func() { _ = zr.Close() }()

	entries := make([]archiveEntry, 0, len(zr.File))
	for _, file := range zr.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open archive entry %s: %v", file.Name, err)
		}
		content, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			t.Fatalf("read archive entry %s: %v", file.Name, readErr)
		}
		if closeErr != nil {
			t.Fatalf("close archive entry %s: %v", file.Name, closeErr)
		}
		entries = append(entries, archiveEntry{
			Name:     file.Name,
			Method:   file.Method,
			Modified: file.Modified.UTC().Format(time.RFC3339),
			Mode:     file.Mode(),
			Content:  string(content),
		})
	}
	return entries
}

func assertArchiveEntries(t *testing.T, entries []archiveEntry) {
	t.Helper()

	want := []struct {
		name    string
		content string
	}{
		{"licenses/Apache-2.0.txt", "Apache License\n"},
		{"licenses/MIT.txt", "MIT License\n"},
	}
	if len(entries) != len(want) {
		t.Fatalf("archive has %d entries, want %d: %+v", len(entries), len(want), entries)
	}
	for i, expected := range want {
		entry := entries[i]
		if entry.Name != expected.name || entry.Content != expected.content {
			t.Errorf("archive entry %d = %q with %q, want %q with %q",
				i, entry.Name, entry.Content, expected.name, expected.content)
		}
		if entry.Modified != "1980-01-01T00:00:00Z" {
			t.Errorf("archive entry %s timestamp = %s, want pinned 1980-01-01T00:00:00Z",
				entry.Name, entry.Modified)
		}
		if entry.Method != zip.Deflate {
			t.Errorf("archive entry %s method = %d, want %d", entry.Name, entry.Method, zip.Deflate)
		}
	}
}

func assertIndexExact(t *testing.T, index []byte) {
	t.Helper()

	want := `const Version = "test-1.0"

// canonicalIDs maps lowercase SPDX identifiers to their canonical casing.
var canonicalIDs = map[string]string{
	"apache-2.0": "Apache-2.0",
	"mit":        "MIT",
}
`
	if !strings.Contains(string(index), want) {
		t.Errorf("generated index does not match the expected sorted keys:\n%s", index)
	}
}

// TestRunProducesDeterministicOutput guards version-independent output: the
// fixture is listed in reverse order, so entry order proves the sort; entry
// contents/metadata and the index text must match across runs.
func TestRunProducesDeterministicOutput(t *testing.T) {
	source := writeLicenseSource(t, map[string]string{
		"Apache-2.0": "Apache License\n",
		"MIT":        "MIT License\n",
	})

	dirs := []string{t.TempDir(), t.TempDir()}
	for _, dir := range dirs {
		cfg := config{
			sourceDir: source,
			outPath:   filepath.Join(dir, "licenses.zip"),
			indexPath: filepath.Join(dir, "index.go"),
		}
		if err := run(context.Background(), cfg); err != nil {
			t.Fatalf("run into %s: %v", dir, err)
		}
	}

	firstArchive := readArchiveEntries(t, filepath.Join(dirs[0], "licenses.zip"))
	secondArchive := readArchiveEntries(t, filepath.Join(dirs[1], "licenses.zip"))
	if !slices.Equal(firstArchive, secondArchive) {
		t.Errorf("archive entries differ between identical runs:\nfirst: %+v\nsecond: %+v", firstArchive, secondArchive)
	}
	assertArchiveEntries(t, firstArchive)

	firstIndex := readFile(t, filepath.Join(dirs[0], "index.go"))
	secondIndex := readFile(t, filepath.Join(dirs[1], "index.go"))
	if !bytes.Equal(firstIndex, secondIndex) {
		t.Errorf("index.go differs between identical runs")
	}
	assertIndexExact(t, firstIndex)
}
