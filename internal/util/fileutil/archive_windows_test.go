// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package fileutil

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractTarGz_RejectsNonLocalWindowsNames covers names that filepath.IsLocal
// rejects only on Windows: drive/UNC/rooted paths, backslash parent traversal,
// and reserved device names. Linux CI must not assert these semantics, so the
// test lives behind the windows build tag.
func TestExtractTarGz_RejectsNonLocalWindowsNames(t *testing.T) {
	names := []string{
		`../x`,
		`..\..\evil.txt`,
		`a\..\..\b`,
		`C:\Windows\evil.txt`,
		`C:evil.txt`,
		`\\server\share\evil.txt`,
		`\rooted\evil.txt`,
		`NUL`,
		`aux`,
	}

	// Since Windows 11, reserved device names with extensions (CON.txt, LPT9.log)
	// are valid file names, and filepath.IsLocal delegates that check to the OS.
	// Only assert rejection where the host still reports the name as non-local.
	for _, name := range []string{"con.txt", "COM1.txt", "LPT9.log"} {
		if !filepath.IsLocal(name) {
			names = append(names, name)
		}
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			destDir := t.TempDir()
			archive := buildTarGz(t, tarEntry{
				header:  tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644},
				content: "payload",
			})

			err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20)
			if err == nil {
				t.Fatalf("expected ExtractTarGz to reject non-local Windows name %q", name)
			}
			if !strings.Contains(err.Error(), "path traversal rejected for") {
				t.Errorf("error does not use the traversal rejection prefix: %v", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error does not contain entry name %q: %v", name, err)
			}

			entries, readErr := os.ReadDir(destDir)
			if readErr != nil {
				t.Fatalf("reading destination directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Errorf("rejected entry %q left %d entries in destination", name, len(entries))
			}
		})
	}
}

// TestExtractTarGz_AcceptsBackslashLocalNames proves the guard only rejects
// escaping Windows paths, not every name containing a backslash.
func TestExtractTarGz_AcceptsBackslashLocalNames(t *testing.T) {
	destDir := t.TempDir()
	archive := buildTarGz(t, tarEntry{
		header:  tar.Header{Name: `a\b.txt`, Typeflag: tar.TypeReg, Mode: 0o644},
		content: "backslash",
	})

	if err := ExtractTarGz(bytes.NewReader(archive), destDir, 10<<20); err != nil {
		t.Fatalf("ExtractTarGz rejected a local Windows name: %v", err)
	}

	content, err := SafeReadFile(destDir, filepath.Join("a", "b.txt"))
	if err != nil {
		t.Fatalf("reading extracted file: %v", err)
	}
	if string(content) != "backslash" {
		t.Errorf("expected %q, got %q", "backslash", string(content))
	}
}
