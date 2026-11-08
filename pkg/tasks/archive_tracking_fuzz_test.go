// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build go1.18

package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzSafeArchiveEntryName checks the containment invariant of the zip entry
// validator: a successful result is a local relative path that cannot escape
// the extraction directory, and a rejected entry never returns a path.
func FuzzSafeArchiveEntryName(f *testing.F) { //nolint:cyclop // invariant checks are intentionally explicit
	seeds := []string{
		"file.txt",
		"dir/sub/file.txt",
		"a//b",
		"./a",
		"a/./b",
		"src/new/deep/file.txt",
		"/etc/passwd",
		"../escape.txt",
		"../../escape.txt",
		"dir/../../escape.txt",
		"..",
		".",
		"",
		`nested\escape.txt`,
		`..\..\escape.txt`,
		`\\server\share\file.txt`,
		"C:escape.txt",
		"C:/escape.txt",
		`C:\escape.txt`,
		"//server/share/file.txt",
		"日本語/ファイル.txt",
		"é/ñ.txt",
		"foo/",
		"hello world/file name.txt",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	// A fixed lexical root; the validator must never produce a path that
	// resolves outside it once joined.
	root := filepath.Join(os.TempDir(), "scbake-safe-entry-fuzz-root")

	f.Fuzz(func(t *testing.T, name string) {
		got, err := safeArchiveEntryName(name)
		if err != nil {
			if got != "" {
				t.Fatalf("rejected entry %q returned escaping path %q", name, got)
			}
			return
		}
		if got == "" {
			t.Fatalf("accepted entry %q returned an empty path", name)
		}

		// Normalize to slashes so the checks are identical on every platform:
		// on Windows the OS separator itself is a backslash.
		slash := filepath.ToSlash(got)

		if filepath.IsAbs(got) || strings.HasPrefix(slash, "/") {
			t.Fatalf("accepted entry %q produced absolute path %q", name, got)
		}
		if filepath.VolumeName(got) != "" || hasWindowsDrivePrefix(slash) {
			t.Fatalf("accepted entry %q produced a drive-prefixed path %q", name, got)
		}
		if strings.Contains(slash, `\`) {
			t.Fatalf("accepted entry %q produced a backslash path %q", name, got)
		}
		for _, part := range strings.Split(slash, "/") {
			if part == ".." {
				t.Fatalf("accepted entry %q produced a parent traversal %q", name, got)
			}
		}
		if slash == ".." || strings.HasPrefix(slash, "../") {
			t.Fatalf("accepted entry %q produced an escaping path %q", name, got)
		}

		joined := filepath.Join(root, got)
		rel, relErr := filepath.Rel(root, joined)
		if relErr != nil {
			t.Fatalf("entry %q result %q is not relative to root: %v", name, got, relErr)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("entry %q result %q escapes the extraction root", name, got)
		}
	})
}

// hasWindowsDrivePrefix reports whether a slash-normalized path starts with a
// Windows drive prefix such as "C:". The production validator must reject these
// on every platform, not only on Windows.
func hasWindowsDrivePrefix(path string) bool {
	if len(path) < 2 || path[1] != ':' {
		return false
	}
	c := path[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}
