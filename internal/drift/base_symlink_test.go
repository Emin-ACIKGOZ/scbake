// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package drift

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

// TestDetect_SymlinkedBaseFailsClosed is the #222 drift guard: a committed base
// root that is a symlink to an in-root source directory must abort the scan,
// never produce a false clean or a harmless BaseMissing finding.
func TestDetect_SymlinkedBaseFailsClosed(t *testing.T) {
	root := t.TempDir()
	content := []byte("generated\n")
	mustWrite(t, filepath.Join(root, "a.txt"), content)
	mustWrite(t, filepath.Join(root, "src", "a.txt"), []byte("local\n"))

	baseRoot := filepath.Join(root, fileutil.CommittedBaseDir)
	if err := os.Symlink("src", baseRoot); err != nil {
		t.Skipf("symlink creation requires privileges: %v", err)
	}

	m := &types.Manifest{ManagedFiles: map[string]string{"a.txt": hashOf(content)}}
	got, err := Detect(m, root)
	if err == nil {
		t.Fatalf("symlinked base was not surfaced; findings = %+v", got)
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("symlinked base was reported as NotExist: %v", err)
	}
	if !strings.Contains(err.Error(), baseRoot) {
		t.Errorf("error %q does not name the offending path %q", err, baseRoot)
	}
	if !strings.Contains(err.Error(), "symlinked base directory is not allowed") {
		t.Errorf("error %q does not explain the rejected symlinked base directory", err)
	}
}
