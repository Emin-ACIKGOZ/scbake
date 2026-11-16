// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"scbake/pkg/templates/compliance/internal/spdx/version"
	"slices"
	"strconv"
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

// writeLicenseSource creates a local license-list-data checkout for -source
// whose licenseListVersion matches the pinned SPDX version. Detail files omit
// licenseId so the fixture also works on case-insensitive filesystems, where
// "MIT.json" and "mit.json" cannot coexist.
func writeLicenseSource(t *testing.T, texts map[string]string) string {
	t.Helper()

	return writeLicenseSourceWithVersion(t, texts, version.Version)
}

// writeLicenseSourceWithVersion is writeLicenseSource with an explicit
// licenseListVersion, so tests can simulate a local checkout or an upstream
// bump that does not match the pinned version.
func writeLicenseSourceWithVersion(t *testing.T, texts map[string]string, listVersion string) string {
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

	list := fixtureList{Version: listVersion}
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
	source := writeRawLicenseList(t, `{"licenseListVersion":`+strconv.Quote(version.Version)+`,"licenses":[]}`)

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

	want := `// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

// Code generated by go generate; DO NOT EDIT.

package spdx

import "scbake/pkg/templates/compliance/internal/spdx/version"

// Version is the SPDX License List version embedded in this package.
const Version = version.Version

// canonicalIDs maps lowercase SPDX identifiers to their canonical casing.
var canonicalIDs = map[string]string{
	"apache-2.0": "Apache-2.0",
	"mit":        "MIT",
}
`
	if !bytes.Equal(index, []byte(want)) {
		t.Errorf("generated index mismatch:\ngot:\n%s\nwant:\n%s", index, want)
	}
}

// TestDefaultTagMatchesPinnedVersion keeps the generator's default -tag tied
// to version.Tag. A re-hardcoded stale tag would otherwise only surface when
// someone runs the maintainer-only download path.
func TestDefaultTagMatchesPinnedVersion(t *testing.T) {
	tagFlag := flag.Lookup("tag")
	if tagFlag == nil {
		t.Fatal(`flag "tag" is not registered`)
	}
	if tagFlag.DefValue != version.Tag {
		t.Errorf("default -tag = %q, want version.Tag %q", tagFlag.DefValue, version.Tag)
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

// TestRunRejectsVersionMismatchWithoutWriting guards against refreshing the
// archive from a local checkout whose licenseListVersion differs from the
// pinned version. The mismatch must fail before either output is touched.
func TestRunRejectsVersionMismatchWithoutWriting(t *testing.T) {
	const listVersion = "9.9.9"
	source := writeLicenseSourceWithVersion(t, map[string]string{
		"MIT": "MIT License\n",
	}, listVersion)

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
		t.Fatal("run succeeded with a mismatched licenseListVersion; want an error")
	}
	for _, want := range []string{
		"license list version " + listVersion + " does not match pinned SPDX version " + version.Version,
		"update pkg/templates/compliance/internal/spdx/version/version.go first",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("run error = %v, want it to contain %q", err, want)
		}
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
		t.Fatal("run succeeded with a mismatched licenseListVersion; want an error")
	}
	entries, err := os.ReadDir(freshDir)
	if err != nil {
		t.Fatalf("read fresh output dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("version mismatch created %d entries in a fresh output dir: %v", len(entries), entries)
	}
}

// TestNewLoaderTagValidation keeps the download path strict about the tag that
// is concatenated into a URL, while a local -source checkout bypasses tag
// validation because no URL is built from it.
func TestNewLoaderTagValidation(t *testing.T) {
	// The guard is a URL-shape check: it requires a "v" prefix and rejects
	// path, query, and fragment characters. It does not parse the semantic
	// version, which is why only tags that fail those rules are listed here.
	malformed := []string{
		"",
		"3.29.0",
		"release-3.29.0",
		"v3.29.0/../evil",
		`v3.29.0\evil`,
		"v3.29.0?ref=main",
		"v3.29.0#fragment",
	}

	t.Run("remote rejects malformed tags", func(t *testing.T) {
		for i, tag := range malformed {
			if _, err := newLoader("", tag); err == nil {
				t.Errorf("newLoader(\"\", tag %d %q) succeeded; want an invalid tag error", i, tag)
			}
		}
	})

	t.Run("remote accepts the pinned tag", func(t *testing.T) {
		l, err := newLoader("", version.Tag)
		if err != nil {
			t.Fatalf("newLoader(\"\", %q) failed: %v", version.Tag, err)
		}
		wantURL := licenseRepoURL + version.Tag + "/"
		if l.baseURL != wantURL {
			t.Errorf("loader baseURL = %q, want %q", l.baseURL, wantURL)
		}
		if l.sourceDir != "" {
			t.Errorf("remote loader sourceDir = %q, want empty", l.sourceDir)
		}
	})

	t.Run("local source bypasses tag validation", func(t *testing.T) {
		source := filepath.Join(t.TempDir(), "license-list-data")
		tags := append([]string{version.Tag}, malformed...)
		for i, tag := range tags {
			l, err := newLoader(source, tag)
			if err != nil {
				t.Errorf("newLoader(source, tag %d %q) failed: %v", i, tag, err)
				continue
			}
			if l.sourceDir != source {
				t.Errorf("local loader sourceDir = %q, want %q", l.sourceDir, source)
			}
			if l.baseURL != "" {
				t.Errorf("local loader baseURL = %q, want empty", l.baseURL)
			}
		}
	})
}

// TestRunRefreshSimulation models the documented refresh procedure: a local
// checkout at the pinned version succeeds, while an upstream bump without
// editing version/version.go fails with the message that explains the fix.
func TestRunRefreshSimulation(t *testing.T) {
	texts := map[string]string{
		"Apache-2.0": "Apache License\n",
		"MIT":        "MIT License\n",
	}

	t.Run("local source at pinned version succeeds", func(t *testing.T) {
		source := writeLicenseSourceWithVersion(t, texts, version.Version)
		outDir := t.TempDir()
		cfg := config{
			sourceDir: source,
			outPath:   filepath.Join(outDir, "licenses.zip"),
			indexPath: filepath.Join(outDir, "index.go"),
		}
		if err := run(context.Background(), cfg); err != nil {
			t.Fatalf("run with pinned version: %v", err)
		}

		index := readFile(t, cfg.indexPath)
		if !strings.Contains(string(index), "const Version = version.Version") {
			t.Errorf("generated index does not re-export the pinned version:\n%s", index)
		}
		assertArchiveEntries(t, readArchiveEntries(t, cfg.outPath))
	})

	t.Run("upstream bump without pin update fails", func(t *testing.T) {
		const bumped = "9.9.9"
		source := writeLicenseSourceWithVersion(t, texts, bumped)

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
			t.Fatal("run succeeded with an upstream bump; want an error")
		}
		want := "license list version " + bumped + " does not match pinned SPDX version " + version.Version +
			"; update pkg/templates/compliance/internal/spdx/version/version.go first"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("run error = %v, want it to contain %q", err, want)
		}

		assertFileContents(t, archivePath, archiveSentinel)
		assertFileContents(t, indexPath, indexSentinel)
	})
}
