// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package fileutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"scbake/internal/testutil"
	"strings"
	"testing"
	"unicode/utf8"
)

type tarEntry struct {
	header  tar.Header
	content string
}

// buildTarGz writes the given entries into an in-memory gzipped tarball.
func buildTarGz(t testing.TB, entries ...tarEntry) []byte {
	t.Helper()
	return buildTarGzWithRawPrefix(t, nil, entries...)
}

// buildTarGzWithRawPrefix is buildTarGz with arbitrary raw tar blocks written
// before the first entry. It is needed for headers that archive/tar rewrites,
// such as a global header with a truly empty name.
func buildTarGzWithRawPrefix(t testing.TB, prefix []byte, entries ...tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if len(prefix) > 0 {
		if _, err := gz.Write(prefix); err != nil {
			t.Fatalf("writing raw tar prefix: %v", err)
		}
	}
	tw := tar.NewWriter(gz)
	for i := range entries {
		h := entries[i].header
		if entries[i].content != "" {
			h.Size = int64(len(entries[i].content))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("writing tar header %q: %v", h.Name, err)
		}
		if entries[i].content != "" {
			if _, err := tw.Write([]byte(entries[i].content)); err != nil {
				t.Fatalf("writing tar content %q: %v", h.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("closing tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("closing gzip writer: %v", err)
	}
	return buf.Bytes()
}

// tarBlockSize is the fixed size of a tar header or data block.
const tarBlockSize = 512

// rawGlobalHeaderBlock builds a single ustar global header (type 'g') block
// with the given name and a valid checksum. archive/tar's writer replaces an
// empty global name with "GlobalHead.0.0", so a raw block is required to test
// the true empty-name case.
func rawGlobalHeaderBlock(name string) []byte {
	block := make([]byte, tarBlockSize)
	copy(block[0:100], name)                // name
	copy(block[100:108], "0000644\x00")     // mode
	copy(block[108:116], "0000000\x00")     // uid
	copy(block[116:124], "0000000\x00")     // gid
	copy(block[124:136], "00000000000\x00") // size
	copy(block[136:148], "00000000000\x00") // modification time
	copy(block[148:156], "        ")        // checksum placeholder (spaces)
	block[156] = tar.TypeXGlobalHeader
	copy(block[257:263], "ustar\x00")
	copy(block[263:265], "00")

	var checksum int
	for _, b := range block {
		checksum += int(b)
	}
	copy(block[148:156], fmt.Sprintf("%06o\x00 ", checksum))
	return block
}

func TestExtractTarGz_Basic(t *testing.T) {
	destDir := t.TempDir()
	archive := testutil.CreateTarGz(map[string]string{
		"templates/main.go.tpl": "package main",
		"templates/handler.tpl": "func handler() {}",
	})

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
		t.Fatalf("ExtractTarGz failed: %v", err)
	}

	content, err := SafeReadFile(destDir, "templates/main.go.tpl")
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "package main" {
		t.Errorf("expected 'package main', got %q", string(content))
	}
}

func TestExtractTarGz_PathTraversalRejection(t *testing.T) {
	destDir := t.TempDir()
	archive := testutil.CreateTarGz(map[string]string{
		"../escape.txt": "secret data",
	})

	err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20)
	if err == nil {
		t.Error("expected ExtractTarGz to reject path traversal attempt")
	}
	if !strings.Contains(err.Error(), "path traversal rejected") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestExtractTarGz_DecompressionBombProtection(t *testing.T) {
	destDir := t.TempDir()
	const maxLimit = 100

	oversizedPayload := bytes.Repeat([]byte("A"), 500)
	archive := testutil.CreateTarGz(map[string]string{
		"large.txt": string(oversizedPayload),
	})

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, maxLimit); err != nil {
		t.Fatalf("ExtractTarGz failed unexpectedly: %v", err)
	}

	info, err := os.Stat(filepath.Join(destDir, "large.txt"))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if info.Size() > maxLimit {
		t.Errorf("decompression limit failed: extracted size %d exceeds maxLimit %d", info.Size(), maxLimit)
	}
}

func TestExtractTarGz_RejectsLinkEntries(t *testing.T) {
	tests := []struct {
		name   string
		header tar.Header
	}{
		{
			name: "symlink relative target",
			header: tar.Header{
				Name: "link.txt", Typeflag: tar.TypeSymlink, Linkname: "target.txt", Mode: 0o777,
			},
		},
		{
			name: "symlink absolute target",
			header: tar.Header{
				Name: "link.txt", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd", Mode: 0o777,
			},
		},
		{
			name: "symlink parent target",
			header: tar.Header{
				Name: "link.txt", Typeflag: tar.TypeSymlink, Linkname: "../outside.txt", Mode: 0o777,
			},
		},
		{
			name: "hardlink relative target",
			header: tar.Header{
				Name: "hard.txt", Typeflag: tar.TypeLink, Linkname: "target.txt", Mode: 0o644,
			},
		},
		{
			name: "hardlink parent target",
			header: tar.Header{
				Name: "hard.txt", Typeflag: tar.TypeLink, Linkname: "../outside.txt", Mode: 0o644,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir := t.TempDir()

			err := ExtractTarGz(bytes.NewReader(buildTarGz(t, tarEntry{header: tc.header})), destDir, 10<<20)
			if err == nil {
				t.Fatal("expected ExtractTarGz to reject link entry")
			}
			if !strings.Contains(err.Error(), "unsupported tar entry") {
				t.Errorf("error does not name the unsupported entry: %v", err)
			}
			if !strings.Contains(err.Error(), tc.header.Name) {
				t.Errorf("error does not contain entry name %q: %v", tc.header.Name, err)
			}
			if !strings.Contains(err.Error(), "links are not extracted") {
				t.Errorf("error does not explain link rejection: %v", err)
			}

			_, statErr := os.Lstat(filepath.Join(destDir, tc.header.Name))
			if !os.IsNotExist(statErr) {
				t.Errorf("expected %q not to exist, got err=%v", tc.header.Name, statErr)
			}
		})
	}
}

func TestExtractTarGz_RejectsOtherSpecialEntries(t *testing.T) {
	tests := []struct {
		name   string
		header tar.Header
	}{
		{
			name:   "fifo",
			header: tar.Header{Name: "fifo", Typeflag: tar.TypeFifo, Mode: 0o644},
		},
		{
			name:   "char device",
			header: tar.Header{Name: "chardev", Typeflag: tar.TypeChar, Mode: 0o644, Devmajor: 1, Devminor: 2},
		},
		{
			name:   "block device",
			header: tar.Header{Name: "blockdev", Typeflag: tar.TypeBlock, Mode: 0o644, Devmajor: 1, Devminor: 2},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir := t.TempDir()

			err := ExtractTarGz(bytes.NewReader(buildTarGz(t, tarEntry{header: tc.header})), destDir, 10<<20)
			if err == nil {
				t.Fatal("expected ExtractTarGz to reject special entry")
			}
			if !strings.Contains(err.Error(), "unsupported tar entry") {
				t.Errorf("error does not name the unsupported entry: %v", err)
			}
			if !strings.Contains(err.Error(), "only regular files and directories are extracted") {
				t.Errorf("error does not explain supported entries: %v", err)
			}
			if !strings.Contains(err.Error(), tc.header.Name) {
				t.Errorf("error does not contain entry name %q: %v", tc.header.Name, err)
			}
		})
	}
}

func TestExtractTarGz_IgnoresPAXGlobalHeader(t *testing.T) {
	destDir := t.TempDir()
	archive := buildTarGz(t,
		tarEntry{header: tar.Header{
			Name:       "pax_global_header",
			Typeflag:   tar.TypeXGlobalHeader,
			Format:     tar.FormatPAX,
			PAXRecords: map[string]string{"comment": "test"},
		}},
		tarEntry{
			header:  tar.Header{Name: "main.tpl", Typeflag: tar.TypeReg, Mode: 0o644},
			content: "package main",
		},
	)

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
		t.Fatalf("ExtractTarGz failed: %v", err)
	}

	content, err := SafeReadFile(destDir, "main.tpl")
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "package main" {
		t.Errorf("expected 'package main', got %q", string(content))
	}

	_, statErr := os.Lstat(filepath.Join(destDir, "pax_global_header"))
	if !os.IsNotExist(statErr) {
		t.Errorf("expected pax_global_header not to exist, got err=%v", statErr)
	}
}

// TestExtractTarGz_RejectsNonLocalEntryNames covers entry names that are not
// lexically local to the extraction directory. They must be rejected by the
// name guard before SafePath or any filesystem operation runs.
func TestExtractTarGz_RejectsNonLocalEntryNames(t *testing.T) {
	tests := []struct {
		name       string
		makeHeader func(destDir string) tar.Header
		content    string
	}{
		{
			name: "empty regular file name",
			makeHeader: func(string) tar.Header {
				return tar.Header{Typeflag: tar.TypeReg, Mode: 0o644}
			},
			content: "payload",
		},
		{
			name: "empty directory name",
			makeHeader: func(string) tar.Header {
				return tar.Header{Typeflag: tar.TypeDir, Mode: 0o750}
			},
		},
		{
			name: "parent directory",
			makeHeader: func(string) tar.Header {
				return tar.Header{Name: "..", Typeflag: tar.TypeDir, Mode: 0o750}
			},
		},
		{
			name: "parent relative file",
			makeHeader: func(string) tar.Header {
				return tar.Header{Name: "../x", Typeflag: tar.TypeReg, Mode: 0o644}
			},
			content: "payload",
		},
		{
			name: "absolute path inside destination",
			makeHeader: func(destDir string) tar.Header {
				return tar.Header{
					Name:     filepath.Join(destDir, "absolute-inside.txt"),
					Typeflag: tar.TypeReg,
					Mode:     0o644,
				}
			},
			content: "payload",
		},
		{
			name: "nested parent escape",
			makeHeader: func(string) tar.Header {
				return tar.Header{Name: "a/../../b", Typeflag: tar.TypeReg, Mode: 0o644}
			},
			content: "payload",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir := t.TempDir()
			header := tc.makeHeader(destDir)

			archive := buildTarGz(t, tarEntry{header: header, content: tc.content})
			err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20)
			if err == nil {
				t.Fatalf("expected ExtractTarGz to reject non-local name %q", header.Name)
			}
			if !strings.Contains(err.Error(), "path traversal rejected for") {
				t.Errorf("error does not use the traversal rejection prefix: %v", err)
			}
			if header.Name != "" && !strings.Contains(err.Error(), header.Name) {
				t.Errorf("error does not contain entry name %q: %v", header.Name, err)
			}
			if header.Name == "" && !strings.Contains(err.Error(), "non-local archive entry name") {
				t.Errorf("empty name was not rejected by the local-name guard: %v", err)
			}

			entries, readErr := os.ReadDir(destDir)
			if readErr != nil {
				t.Fatalf("reading destination directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Errorf("rejected entry %q left %d entries in destination", header.Name, len(entries))
			}
		})
	}
}

// TestExtractTarGz_AcceptsLocalEntryNames is the negative control: names that
// are local after cleaning must still extract, including the "./" root that
// `tar -C dir .` produces.
func TestExtractTarGz_AcceptsLocalEntryNames(t *testing.T) {
	tests := []struct {
		name        string
		entries     []tarEntry
		wantPath    string
		wantContent string
	}{
		{
			name: "dot root directory",
			entries: []tarEntry{
				{header: tar.Header{Name: "./", Typeflag: tar.TypeDir, Mode: 0o750}},
				{
					header:  tar.Header{Name: "./main.tpl", Typeflag: tar.TypeReg, Mode: 0o644},
					content: "package main",
				},
			},
			wantPath:    "main.tpl",
			wantContent: "package main",
		},
		{
			name: "dot prefixed nested file",
			entries: []tarEntry{
				{
					header:  tar.Header{Name: "./a/b.txt", Typeflag: tar.TypeReg, Mode: 0o644},
					content: "dotted",
				},
			},
			wantPath:    filepath.Join("a", "b.txt"),
			wantContent: "dotted",
		},
		{
			name: "duplicated separator",
			entries: []tarEntry{
				{
					header:  tar.Header{Name: "a//b", Typeflag: tar.TypeReg, Mode: 0o644},
					content: "double slash",
				},
			},
			wantPath:    filepath.Join("a", "b"),
			wantContent: "double slash",
		},
		{
			name: "internal parent reference",
			entries: []tarEntry{
				{
					header:  tar.Header{Name: "foo/../bar", Typeflag: tar.TypeReg, Mode: 0o644},
					content: "stays inside",
				},
			},
			wantPath:    "bar",
			wantContent: "stays inside",
		},
		{
			name: "unicode and spaces",
			entries: []tarEntry{
				{
					header:  tar.Header{Name: "dir with space/üñï.txt", Typeflag: tar.TypeReg, Mode: 0o644},
					content: "unicode",
				},
			},
			wantPath:    filepath.Join("dir with space", "üñï.txt"),
			wantContent: "unicode",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir := t.TempDir()

			archive := buildTarGz(t, tc.entries...)
			if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
				t.Fatalf("ExtractTarGz rejected a local entry: %v", err)
			}

			content, err := SafeReadFile(destDir, tc.wantPath)
			if err != nil {
				t.Fatalf("reading extracted file %q: %v", tc.wantPath, err)
			}
			if string(content) != tc.wantContent {
				t.Errorf("expected %q, got %q", tc.wantContent, string(content))
			}
		})
	}
}

// TestExtractTarGz_InternalParentDoesNotCreateSiblingDir verifies that an
// accepted "foo/../bar" entry cleans to "bar" and never creates "foo".
func TestExtractTarGz_InternalParentDoesNotCreateSiblingDir(t *testing.T) {
	destDir := t.TempDir()
	archive := buildTarGz(t, tarEntry{
		header:  tar.Header{Name: "foo/../bar", Typeflag: tar.TypeReg, Mode: 0o644},
		content: "stays inside",
	})

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
		t.Fatalf("ExtractTarGz failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destDir, "foo")); !os.IsNotExist(err) {
		t.Errorf("expected intermediate %q not to be created, got err=%v", "foo", err)
	}
}

// TestExtractTarGz_IgnoresGlobalHeaderWithEmptyName covers global PAX headers
// that carry no filesystem entry and must be ignored before name validation.
// The headers are emitted as raw ustar blocks because archive/tar's writer
// rewrites an empty global name to "GlobalHead.0.0", which would hide the
// ordering bug. A non-local global name must be ignored too, proving the
// TypeXGlobalHeader early return precedes the local-name guard.
func TestExtractTarGz_IgnoresGlobalHeaderWithEmptyName(t *testing.T) {
	tests := []struct {
		name       string
		globalName string
	}{
		{name: "empty name", globalName: ""},
		{name: "parent relative name", globalName: "../evil"},
		{name: "absolute name", globalName: "/global/evil"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			destDir := t.TempDir()
			archive := buildTarGzWithRawPrefix(t,
				rawGlobalHeaderBlock(tc.globalName),
				tarEntry{
					header:  tar.Header{Name: "main.tpl", Typeflag: tar.TypeReg, Mode: 0o644},
					content: "package main",
				},
			)

			if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
				t.Fatalf("ExtractTarGz failed on global header %q: %v", tc.globalName, err)
			}

			content, err := SafeReadFile(destDir, "main.tpl")
			if err != nil {
				t.Fatalf("failed to read extracted file: %v", err)
			}
			if string(content) != "package main" {
				t.Errorf("expected 'package main', got %q", string(content))
			}

			entries, err := os.ReadDir(destDir)
			if err != nil {
				t.Fatalf("reading destination directory: %v", err)
			}
			if len(entries) != 1 || entries[0].Name() != "main.tpl" {
				t.Errorf("expected only main.tpl in destination, got %v", entries)
			}
		})
	}
}

// FuzzExtractTarGz fuzzes archive entry names and types and asserts the core
// Zip Slip invariant: extraction never creates anything outside destDir.
func FuzzExtractTarGz(f *testing.F) {
	seeds := []struct {
		name     string
		typeflag byte
	}{
		{name: "../x", typeflag: tar.TypeReg},
		{name: "..", typeflag: tar.TypeDir},
		{name: "/absolute/escape.txt", typeflag: tar.TypeReg},
		{name: "", typeflag: tar.TypeReg},
		{name: "a/../../b", typeflag: tar.TypeReg},
		{name: "./a/b.txt", typeflag: tar.TypeReg},
		{name: "foo/../bar", typeflag: tar.TypeReg},
		{name: "dir with space/üñï.txt", typeflag: tar.TypeDir},
	}
	for _, seed := range seeds {
		f.Add(seed.name, seed.typeflag)
	}

	f.Fuzz(func(t *testing.T, name string, typeflag byte) {
		if len(name) > 512 {
			t.Skip("oversized name slows the deterministic corpus")
		}
		// tar cannot encode an entry name containing a NUL byte or invalid
		// UTF-8, so those are fixture limitations rather than extraction cases.
		if strings.ContainsRune(name, 0) || !utf8.ValidString(name) {
			t.Skip("name is not representable in a tar header")
		}

		entryType, content := fuzzEntry(name, typeflag)

		sandbox := t.TempDir()
		destDir := filepath.Join(sandbox, "dest")
		if err := os.Mkdir(destDir, 0o750); err != nil {
			t.Fatalf("creating destination directory: %v", err)
		}

		archive := buildTarGz(t, tarEntry{
			header:  tar.Header{Name: name, Typeflag: entryType, Mode: 0o644},
			content: content,
		})
		_ = ExtractTarGz(bytes.NewReader(archive), destDir, 1<<20)

		outside, walkErr := findEntryOutsideDest(sandbox, destDir)
		if walkErr != nil {
			t.Fatalf("walking sandbox: %v", walkErr)
		}
		if outside != "" {
			t.Errorf("entry name %q created %q outside destination %q", name, outside, destDir)
		}
	})
}

// fuzzEntry maps a fuzzed byte onto a supported archive entry type. Regular
// files cannot be encoded with a trailing separator, so those become dirs.
func fuzzEntry(name string, typeflag byte) (entryType byte, content string) {
	if typeflag%2 == 0 && !strings.HasSuffix(name, "/") {
		return tar.TypeReg, "fuzz"
	}
	return tar.TypeDir, ""
}

// findEntryOutsideDest walks sandbox and returns the first path that is not
// contained in destDir. An empty result means every entry stayed inside.
func findEntryOutsideDest(sandbox, destDir string) (string, error) {
	var outside string

	walkErr := filepath.WalkDir(sandbox, func(path string, _ os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == sandbox {
			return nil
		}
		rel, err := filepath.Rel(destDir, path)
		if err != nil {
			return err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if outside == "" {
				outside = path
			}
		}
		return nil
	})
	return outside, walkErr
}

// TestFindEntryOutsideDest is the negative control for the fuzz invariant: it
// proves an entry written next to destDir is detected, and that contained
// entries are not.
func TestFindEntryOutsideDest(t *testing.T) {
	t.Run("detects sibling escape", func(t *testing.T) {
		sandbox := t.TempDir()
		destDir := filepath.Join(sandbox, "dest")
		if err := os.MkdirAll(destDir, DirPerms); err != nil {
			t.Fatalf("creating destination directory: %v", err)
		}
		escaped := filepath.Join(destDir, "..", "escaped")
		if err := os.WriteFile(escaped, []byte("escaped"), PrivateFilePerms); err != nil {
			t.Fatalf("writing escaping file: %v", err)
		}

		outside, err := findEntryOutsideDest(sandbox, destDir)
		if err != nil {
			t.Fatalf("findEntryOutsideDest failed: %v", err)
		}
		if outside != escaped {
			t.Errorf("expected escape %q to be detected, got %q", escaped, outside)
		}
	})

	t.Run("accepts contained entries", func(t *testing.T) {
		sandbox := t.TempDir()
		destDir := filepath.Join(sandbox, "dest")
		if err := os.MkdirAll(filepath.Join(destDir, "a"), DirPerms); err != nil {
			t.Fatalf("creating destination tree: %v", err)
		}
		if err := os.WriteFile(filepath.Join(destDir, "a", "b.txt"), []byte("ok"), PrivateFilePerms); err != nil {
			t.Fatalf("writing contained file: %v", err)
		}

		outside, err := findEntryOutsideDest(sandbox, destDir)
		if err != nil {
			t.Fatalf("findEntryOutsideDest failed: %v", err)
		}
		if outside != "" {
			t.Errorf("expected no escape, got %q", outside)
		}
	})
}

func TestExtractTarGz_DirectoryEntry(t *testing.T) {
	destDir := t.TempDir()
	archive := buildTarGz(t,
		tarEntry{header: tar.Header{Name: "templates/", Typeflag: tar.TypeDir, Mode: 0o750}},
		tarEntry{
			header:  tar.Header{Name: "templates/main.tpl", Typeflag: tar.TypeReg, Mode: 0o644},
			content: "package main",
		},
	)

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
		t.Fatalf("ExtractTarGz failed: %v", err)
	}

	content, err := SafeReadFile(destDir, "templates/main.tpl")
	if err != nil {
		t.Fatalf("failed to read extracted file: %v", err)
	}
	if string(content) != "package main" {
		t.Errorf("expected 'package main', got %q", string(content))
	}

	info, err := os.Stat(filepath.Join(destDir, "templates"))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected templates to be a directory, got mode %v", info.Mode())
	}
}
