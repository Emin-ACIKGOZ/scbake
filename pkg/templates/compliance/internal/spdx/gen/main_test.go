// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Golden hashes for the fixture built by TestRunProducesDeterministicOutput.
// They cover sorted archive/index order and the pinned ZIP timestamp; regenerate
// them only when the fixture or generator format changes intentionally.
const (
	fixtureArchiveSHA256 = "730c6b89e5746e51a2df14ab1b0f1020b73411b79ff12eff5277e8ada1f547c2"
	fixtureIndexSHA256   = "43ff5c5cd96f025d2f0ee63d816aa6b8daa3381e203414f502ac42c1eb73e607"
)

func assertGoldenHash(t *testing.T, name, path, want string) {
	t.Helper()

	sum := sha256.Sum256(readFile(t, path))
	if got := hex.EncodeToString(sum[:]); got != want {
		t.Errorf("%s sha256 = %s, want %s", name, got, want)
	}
}

func assertArchiveOrderAndTimestamp(t *testing.T, path string) {
	t.Helper()

	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("open generated archive: %v", err)
	}
	defer func() { _ = zr.Close() }()

	var names []string
	for _, file := range zr.File {
		names = append(names, file.Name)
		if got := file.Modified.UTC().Format(time.RFC3339); got != "1980-01-01T00:00:00Z" {
			t.Errorf("archive entry %s timestamp = %s, want pinned 1980-01-01T00:00:00Z", file.Name, got)
		}
	}
	if got, want := strings.Join(names, ","), "licenses/Apache-2.0.txt,licenses/MIT.txt"; got != want {
		t.Errorf("archive entry order = %s, want %s", got, want)
	}
}

func assertIndexSorted(t *testing.T, path string) {
	t.Helper()

	index := string(readFile(t, path))
	apache := strings.Index(index, `"apache-2.0"`)
	mit := strings.Index(index, `"mit"`)
	if apache < 0 || mit < 0 || apache > mit {
		t.Errorf("index entries are not in sorted order (apache-2.0 at %d, mit at %d):\n%s", apache, mit, index)
	}
}

// TestRunProducesDeterministicOutput guards sorted output, the pinned archive
// timestamp, and byte stability: the same local source must produce identical
// bytes, and the fixture must match its golden hashes and entry order.
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

	for _, name := range []string{"licenses.zip", "index.go"} {
		first := readFile(t, filepath.Join(dirs[0], name))
		second := readFile(t, filepath.Join(dirs[1], name))
		if !bytes.Equal(first, second) {
			t.Errorf("%s differs between identical runs", name)
		}
	}

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"licenses.zip", filepath.Join(dirs[0], "licenses.zip"), fixtureArchiveSHA256},
		{"index.go", filepath.Join(dirs[0], "index.go"), fixtureIndexSHA256},
	} {
		assertGoldenHash(t, tc.name, tc.path, tc.want)
	}

	assertArchiveOrderAndTimestamp(t, filepath.Join(dirs[0], "licenses.zip"))
	assertIndexSorted(t, filepath.Join(dirs[0], "index.go"))
}
