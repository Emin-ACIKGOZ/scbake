// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"os"
	"path/filepath"
	"scbake/internal/manifest"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"scbake/pkg/tasks"
	"testing"
	"time"
)

// applyIdenticalFrozenTime marks a fixture file so an unnecessary rewrite is
// observable through its mtime.
var applyIdenticalFrozenTime = time.Unix(1_600_000_000, 0)

// renderEditorconfig applies the built-in editorconfig template to a throwaway
// project and returns the canonical render.
func renderEditorconfig(t *testing.T) []byte {
	t.Helper()

	seed := t.TempDir()
	if err := os.WriteFile(filepath.Join(seed, fileutil.ManifestFileName), nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("write seed manifest: %v", err)
	}
	t.Chdir(seed)
	conflictStrategyFlag = types.ConflictFail
	if err := executeCLI("apply", "--with", "editorconfig", "."); err != nil {
		t.Fatalf("seed apply: %v", err)
	}
	rendered, err := os.ReadFile(filepath.Join(seed, ".editorconfig")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("read seed .editorconfig: %v", err)
	}
	return rendered
}

// newEditorconfigRepo creates an unmanaged repository whose .editorconfig is a
// byte-for-byte copy of the canonical render.
func newEditorconfigRepo(t *testing.T, rendered []byte) string {
	t.Helper()

	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, fileutil.ManifestFileName), nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	path := filepath.Join(repo, ".editorconfig")
	if err := os.WriteFile(path, rendered, fileutil.FilePerms); err != nil {
		t.Fatalf("write identical .editorconfig: %v", err)
	}
	if err := os.Chtimes(path, applyIdenticalFrozenTime, applyIdenticalFrozenTime); err != nil {
		t.Fatalf("chtimes .editorconfig: %v", err)
	}
	return repo
}

// assertEditorconfigAdopted checks that the existing file kept its bytes, was
// not rewritten, produced no artifact, and is recorded as managed.
func assertEditorconfigAdopted(t *testing.T, repo string, rendered []byte) {
	t.Helper()

	path := filepath.Join(repo, ".editorconfig")
	got, err := os.ReadFile(path) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("read .editorconfig: %v", err)
	}
	if string(got) != string(rendered) {
		t.Errorf(".editorconfig bytes changed")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat .editorconfig: %v", err)
	}
	if !info.ModTime().Equal(applyIdenticalFrozenTime) {
		t.Errorf(".editorconfig was rewritten")
	}
	if _, err := os.Stat(path + ".scbake-new"); !os.IsNotExist(err) {
		t.Errorf(".editorconfig.scbake-new exists (stat err: %v), want it absent", err)
	}

	m, root, err := manifest.Load(repo)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if recorded := m.ManagedFiles[".editorconfig"]; recorded != tasks.HashContent(rendered) {
		t.Errorf("managed_files[.editorconfig] = %q, want %q", recorded, tasks.HashContent(rendered))
	}
	base, err := os.ReadFile(filepath.Join(root, ".scbake-base", ".editorconfig")) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatalf("read pristine base: %v", err)
	}
	if string(base) != string(rendered) {
		t.Errorf("pristine base does not match the render")
	}
}

// TestApplyAdoptsByteIdenticalExistingFile reproduces #220 end to end: a copied
// canonical .editorconfig is adopted by apply instead of failing as drift, the
// artifact strategy writes no identical side file, and dry-run reports success.
//
//nolint:cyclop // apply, artifact, and dry-run subtest matrix
func TestApplyAdoptsByteIdenticalExistingFile(t *testing.T) {
	originalStrategy := conflictStrategyFlag
	rendered := renderEditorconfig(t)
	t.Cleanup(func() {
		conflictStrategyFlag = originalStrategy
		resetFlags()
	})

	t.Run("apply adopts and records", func(t *testing.T) {
		repo := newEditorconfigRepo(t, rendered)
		t.Chdir(repo)
		conflictStrategyFlag = types.ConflictFail

		if err := executeCLI("apply", "--with", "editorconfig", "."); err != nil {
			t.Fatalf("apply on an identical unmanaged file = %v, want nil", err)
		}
		assertEditorconfigAdopted(t, repo, rendered)
	})

	t.Run("artifact writes no artifact", func(t *testing.T) {
		repo := newEditorconfigRepo(t, rendered)
		t.Chdir(repo)

		if err := executeCLI("apply", "--with", "editorconfig", "--conflict-strategy", "artifact", "."); err != nil {
			t.Fatalf("apply --conflict-strategy artifact on identical bytes = %v, want nil", err)
		}
		assertEditorconfigAdopted(t, repo, rendered)
	})

	t.Run("dry run reports no conflict", func(t *testing.T) {
		repo := newEditorconfigRepo(t, rendered)
		t.Chdir(repo)
		manifestPath := filepath.Join(repo, fileutil.ManifestFileName)
		before, err := os.ReadFile(manifestPath) //nolint:gosec // test fixture path
		if err != nil {
			t.Fatalf("read manifest before dry run: %v", err)
		}
		conflictStrategyFlag = types.ConflictFail

		if err := executeCLI("apply", "--dry-run", "--with", "editorconfig", "."); err != nil {
			t.Fatalf("apply --dry-run on identical bytes = %v, want nil", err)
		}
		info, err := os.Stat(filepath.Join(repo, ".editorconfig"))
		if err != nil {
			t.Fatalf("stat .editorconfig: %v", err)
		}
		if !info.ModTime().Equal(applyIdenticalFrozenTime) {
			t.Errorf("dry run rewrote .editorconfig")
		}
		if _, err := os.Stat(filepath.Join(repo, ".editorconfig.scbake-new")); !os.IsNotExist(err) {
			t.Errorf("dry run created .editorconfig.scbake-new (stat err: %v)", err)
		}
		if _, err := os.Stat(filepath.Join(repo, ".scbake-base", ".editorconfig")); !os.IsNotExist(err) {
			t.Errorf("dry run wrote a pristine base (stat err: %v)", err)
		}
		after, err := os.ReadFile(manifestPath) //nolint:gosec // test fixture path
		if err != nil {
			t.Fatalf("read manifest after dry run: %v", err)
		}
		if string(after) != string(before) {
			t.Errorf("dry run modified the manifest")
		}
	})
}
