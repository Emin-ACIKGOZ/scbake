// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
)

var acceptedTestFrozenTime = time.Unix(1_000_000_000, 0)

func newAcceptedTaskContext(t *testing.T, root string, m *types.Manifest) types.TaskContext {
	t.Helper()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return types.TaskContext{
		Ctx:              context.Background(),
		TargetPath:       root,
		Manifest:         m,
		Tx:               tx,
		ConflictStrategy: types.ConflictMerge,
	}
}

func freezeAcceptedFile(t *testing.T, path string) {
	t.Helper()
	if err := os.Chtimes(path, acceptedTestFrozenTime, acceptedTestFrozenTime); err != nil {
		t.Fatalf("chtimes %s: %v", path, err)
	}
}

func assertAcceptedFileFrozen(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if !info.ModTime().Equal(acceptedTestFrozenTime) {
		t.Errorf("%s was rewritten", path)
	}
}

// setupAcceptedFile writes a pristine managed file and records its baseline the
// same way a real run does, then installs an accepted local edit.
func setupAcceptedFile(t *testing.T, root string, m *types.Manifest, pristine, edited []byte) types.TaskContext {
	t.Helper()
	tc := newAcceptedTaskContext(t, root, m)
	if err := WriteManagedFile(tc, "file.txt", pristine, fileutil.FilePerms); err != nil {
		t.Fatalf("seed managed file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "file.txt"), edited, fileutil.FilePerms); err != nil {
		t.Fatalf("write accepted edit: %v", err)
	}
	if m.AcceptedFiles == nil {
		m.AcceptedFiles = make(map[string]string)
	}
	m.AcceptedFiles["file.txt"] = HashContent(edited)
	return tc
}

// TestWriteManagedFileAcceptedNoOpStrategies: with an unchanged template and a
// worktree equal to the accepted hash, every non-overwrite strategy leaves the
// file untouched.
func TestWriteManagedFileAcceptedNoOpStrategies(t *testing.T) {
	pristine := []byte("generated\n")
	edited := []byte("accepted edit\n")

	for _, strategy := range []string{
		types.ConflictMerge,
		types.ConflictFail,
		types.ConflictArtifact,
		types.ConflictKeepLocal,
	} {
		t.Run(strategy, func(t *testing.T) {
			root := t.TempDir()
			m := &types.Manifest{ManagedFiles: map[string]string{}}
			tc := setupAcceptedFile(t, root, m, pristine, edited)
			tc.Update = true
			tc.ConflictStrategy = strategy

			path := filepath.Join(root, "file.txt")
			freezeAcceptedFile(t, path)
			if err := WriteManagedFile(tc, "file.txt", pristine, fileutil.FilePerms); err != nil {
				t.Fatalf("WriteManagedFile: %v", err)
			}
			assertAcceptedFileFrozen(t, path)
			if got, _ := os.ReadFile(path); string(got) != string(edited) { //nolint:gosec // test-controlled fixture path under t.TempDir()
				t.Fatalf("accepted bytes changed: %q", got)
			}
			if got := m.ManagedFiles["file.txt"]; got != HashContent(pristine) {
				t.Errorf("managed hash moved: %q", got)
			}
			if got := m.AcceptedFiles["file.txt"]; got != HashContent(edited) {
				t.Errorf("accepted baseline dropped: %q", got)
			}
		})
	}
}

// TestWriteManagedFileAcceptedNoOpRequiresMatchingAcceptedHash is the
// mutation-sensitive guard: a second edit after accept must not be silently
// no-op'd under the fail strategy.
func TestWriteManagedFileAcceptedNoOpRequiresMatchingAcceptedHash(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	accepted := []byte("accepted edit\n")
	second := []byte("second edit\n")
	m := &types.Manifest{ManagedFiles: map[string]string{}}
	tc := setupAcceptedFile(t, root, m, pristine, accepted)
	tc.Update = true
	tc.ConflictStrategy = types.ConflictFail

	if err := os.WriteFile(filepath.Join(root, "file.txt"), second, fileutil.FilePerms); err != nil {
		t.Fatalf("second edit: %v", err)
	}
	err := WriteManagedFile(tc, "file.txt", pristine, fileutil.FilePerms)
	if err == nil {
		t.Fatalf("post-accept edit was silently accepted")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "file.txt")); string(got) != string(second) { //nolint:gosec // test-controlled fixture path under t.TempDir()
		t.Errorf("file changed despite the conflict: %q", got)
	}
	if got := m.AcceptedFiles["file.txt"]; got != HashContent(accepted) {
		t.Errorf("accepted baseline changed on conflict: %q", got)
	}
}

// TestWriteManagedFileOverwriteWritesAndClearsAccepted: overwrite still wins and
// a canonical write invalidates the accepted baseline.
func TestWriteManagedFileOverwriteWritesAndClearsAccepted(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("generated\n")
	edited := []byte("accepted edit\n")
	m := &types.Manifest{ManagedFiles: map[string]string{}}
	tc := setupAcceptedFile(t, root, m, pristine, edited)
	tc.Update = true
	tc.ConflictStrategy = types.ConflictOverwrite

	if err := WriteManagedFile(tc, "file.txt", pristine, fileutil.FilePerms); err != nil {
		t.Fatalf("WriteManagedFile: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(root, "file.txt")); string(got) != string(pristine) { //nolint:gosec // test-controlled fixture path under t.TempDir()
		t.Fatalf("overwrite did not restore pristine content: %q", got)
	}
	if got := m.AcceptedFiles["file.txt"]; got != "" {
		t.Errorf("overwrite left the accepted baseline: %q", got)
	}
	if got := m.ManagedFiles["file.txt"]; got != HashContent(pristine) {
		t.Errorf("overwrite ledger hash = %q", got)
	}
}

// TestWriteManagedFileTemplateChangeMergesAndClearsAccepted: a changed template
// bypasses the accepted no-op, merges against the pristine base, and requires
// re-acceptance because drift returns.
func TestWriteManagedFileTemplateChangeMergesAndClearsAccepted(t *testing.T) {
	root := t.TempDir()
	pristine := []byte("alpha\nbeta\ngamma\n")
	edited := []byte("ALPHA\nbeta\ngamma\n")
	newTemplate := []byte("alpha\nbeta\nGAMMA\n")
	m := &types.Manifest{ManagedFiles: map[string]string{}}
	tc := setupAcceptedFile(t, root, m, pristine, edited)
	tc.Update = true
	tc.ConflictStrategy = types.ConflictMerge

	if err := WriteManagedFile(tc, "file.txt", newTemplate, fileutil.FilePerms); err != nil {
		t.Fatalf("WriteManagedFile: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(root, "file.txt")) //nolint:gosec // test-controlled fixture path under t.TempDir()
	if string(got) != "ALPHA\nbeta\nGAMMA\n" {
		t.Fatalf("clean merge result = %q", got)
	}
	if got := m.AcceptedFiles["file.txt"]; got != "" {
		t.Errorf("canonical merge did not clear acceptance: %q", got)
	}
	if got := m.ManagedFiles["file.txt"]; got != HashContent(newTemplate) {
		t.Errorf("canonical merge recorded %q, want pristine new render", got)
	}
	if HashContent(got) == m.ManagedFiles["file.txt"] {
		t.Errorf("merged content unexpectedly equals the pristine render; test no longer proves re-accept")
	}
}

// TestWriteManagedFileUnmanagedSkipMatrix pins the update/apply/force policy at
// the execution level.
func TestWriteManagedFileUnmanagedSkipMatrix(t *testing.T) { //nolint:cyclop // table-driven skip/force matrix
	content := []byte("template\n")
	local := []byte("local\n")

	cases := []struct {
		name        string
		update      bool
		force       bool
		wantSkipped bool
	}{
		{"update skips", true, false, true},
		{"update with force still skips", true, true, true},
		{"apply skips without force", false, false, true},
		{"apply force re-manages", false, true, false},
	}
	for _, tcCase := range cases {
		t.Run(tcCase.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file.txt")
			if err := os.WriteFile(path, local, fileutil.FilePerms); err != nil {
				t.Fatalf("write local: %v", err)
			}
			m := &types.Manifest{UnmanagedFiles: []string{"file.txt"}}
			tc := newAcceptedTaskContext(t, root, m)
			tc.Update = tcCase.update
			tc.Force = tcCase.force
			tc.ConflictStrategy = types.ConflictFail

			freezeAcceptedFile(t, path)
			if err := WriteManagedFile(tc, "file.txt", content, fileutil.FilePerms); err != nil {
				t.Fatalf("WriteManagedFile: %v", err)
			}
			got, _ := os.ReadFile(path) //nolint:gosec // test-controlled fixture path under t.TempDir()
			if tcCase.wantSkipped {
				assertAcceptedFileFrozen(t, path)
				if string(got) != string(local) {
					t.Fatalf("unmanaged file was rewritten: %q", got)
				}
				if _, ok := m.ManagedFiles["file.txt"]; ok {
					t.Errorf("unmanaged path was re-recorded")
				}
				if !reflect.DeepEqual(m.UnmanagedFiles, []string{"file.txt"}) {
					t.Errorf("unmanaged list changed: %v", m.UnmanagedFiles)
				}
				return
			}
			if string(got) != string(content) {
				t.Fatalf("apply --force wrote %q, want template", got)
			}
			if m.ManagedFiles["file.txt"] != HashContent(content) {
				t.Errorf("apply --force did not record the hash")
			}
			if len(m.UnmanagedFiles) != 0 {
				t.Errorf("apply --force did not clear unmanaged_files: %v", m.UnmanagedFiles)
			}
		})
	}
}

// TestNormalizeUnmanagedFiles: the persisted list is sorted, deduplicated, and
// drops empty keys; an empty result is nil so omitempty applies.
func TestNormalizeUnmanagedFiles(t *testing.T) {
	input := []string{"z.txt", "", "a.txt", "z.txt", "m.txt", ""}
	got := NormalizeUnmanagedFiles(input)
	if !reflect.DeepEqual(got, []string{"a.txt", "m.txt", "z.txt"}) {
		t.Fatalf("NormalizeUnmanagedFiles = %v", got)
	}
	if !reflect.DeepEqual(input, []string{"z.txt", "", "a.txt", "z.txt", "m.txt", ""}) {
		t.Errorf("input slice was mutated: %v", input)
	}
	if got := NormalizeUnmanagedFiles(nil); got != nil {
		t.Errorf("empty normalization = %v, want nil", got)
	}
}

// TestBaseShadowRelPathNestedGitignore: nested .gitignore shadows are sanitized
// at every depth, matching the write path used by unmanage.
func TestBaseShadowRelPathNestedGitignore(t *testing.T) {
	cases := map[string]string{
		".gitignore":              filepath.Join(fileutil.CommittedBaseDir, ".gitignore.scbake-shadow"),
		"services/api/.gitignore": filepath.Join(fileutil.CommittedBaseDir, "services", "api", ".gitignore.scbake-shadow"),
		"services/api/main.go":    filepath.Join(fileutil.CommittedBaseDir, "services", "api", "main.go"),
	}
	for key, want := range cases {
		if got := BaseShadowRelPath(key); got != want {
			t.Errorf("BaseShadowRelPath(%q) = %q, want %q", key, got, want)
		}
	}
}
