// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package fileutil

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"scbake/internal/testutil"
	"strings"
	"testing"
)

type tarEntry struct {
	header  tar.Header
	content string
}

// buildTarGz writes the given entries into an in-memory gzipped tarball.
func buildTarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
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
