// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"scbake/internal/types"
)

// TestNormalizeRecordedPaths pins the deliberately narrow load-time repair:
// only absolute paths that lexically resolve inside the root are rebased.
// Relative paths are ambiguous (their authoring cwd is unrecoverable) and must
// stay byte-identical, and outside-root absolute paths must not be corrupted.
func TestNormalizeRecordedPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	outside := filepath.Join(filepath.Dir(root), "other")

	tests := []struct {
		name    string
		root    string
		project string
		want    string
	}{
		{"absolute subproject", root, filepath.Join(root, "svc"), "svc"},
		{"absolute nested", root, filepath.Join(root, "services", "api"), "services/api"},
		{"absolute root", root, root, "."},
		{"absolute outside", root, filepath.Join(outside, "svc"), filepath.Join(outside, "svc")},
		{"relative dot-slash", root, "./svc", "./svc"},
		{"relative trailing slash", root, "svc/", "svc/"},
		{"relative dot dot inside", root, "a/../b", "a/../b"},
		{"relative dot", root, ".", "."},
		{"relative parent", root, "../svc", "../svc"},
		{"relative parent-prefixed", root, "repo/svc", "repo/svc"},
		{"empty path", root, "", ""},
		{"empty root", "", filepath.Join(root, "svc"), filepath.Join(root, "svc")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &types.Manifest{
				Projects:  []types.Project{{Name: "svc", Path: tt.project, Language: "go"}},
				Templates: []types.Template{{Name: "makefile", Path: tt.project}},
			}
			normalizeRecordedPaths(m, tt.root)
			if m.Projects[0].Path != tt.want {
				t.Errorf("project path = %q, want %q", m.Projects[0].Path, tt.want)
			}
			if m.Templates[0].Path != tt.want {
				t.Errorf("template path = %q, want %q", m.Templates[0].Path, tt.want)
			}
		})
	}
}

func TestNormalizeRecordedPathsNilManifest(t *testing.T) {
	// Must not panic.
	normalizeRecordedPaths(nil, t.TempDir())
}

// TestNormalizeRecordedPathsSymlinkAlias documents the lexical limit: an
// absolute path that reaches the root through a symlink alias is not inside the
// root according to filepath.Rel, so it is left unchanged rather than rewritten
// to a wrong relative path.
func TestNormalizeRecordedPathsSymlinkAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available on Windows CI")
	}
	base := t.TempDir()
	root := filepath.Join(base, "repo")
	if err := os.MkdirAll(filepath.Join(root, "svc"), 0o750); err != nil {
		t.Fatalf("creating root: %v", err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	alias := filepath.Join(link, "svc")
	m := &types.Manifest{
		Projects:  []types.Project{{Name: "svc", Path: alias, Language: "go"}},
		Templates: []types.Template{{Name: "makefile", Path: alias}},
	}
	normalizeRecordedPaths(m, root)
	if m.Projects[0].Path != alias || m.Templates[0].Path != alias {
		t.Errorf("symlink alias was rewritten: project=%q template=%q want %q",
			m.Projects[0].Path, m.Templates[0].Path, alias)
	}
}
