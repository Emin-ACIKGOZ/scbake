// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	list := fixtureList{Version: "test-1.0"}
	for id := range texts {
		list.Licenses = append(list.Licenses, fixtureLicense{ID: id})
	}
	listBytes, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal fixture license list: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "json", "licenses.json"), listBytes, 0o600); err != nil {
		t.Fatalf("write fixture license list: %v", err)
	}

	for id, text := range texts {
		detailBytes, err := json.Marshal(map[string]string{"licenseText": text})
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

// TestRunProducesDeterministicOutput guards the pinned archive header and
// sorted index ordering: the same local source must produce identical bytes.
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
}
