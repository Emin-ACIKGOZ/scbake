// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// readGitIgnore returns the contents of root/.gitignore.
func readGitIgnore(t *testing.T, root string) string {
	t.Helper()
	//nolint:gosec // test-controlled temp path
	data, err := os.ReadFile(filepath.Join(root, fileutil.GitIgnore))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(data)
}

// countInternalStateIgnoreLines counts standalone .scbake/ entries. Leading
// whitespace is part of a Git pattern, so only trailing whitespace is ignored.
func countInternalStateIgnoreLines(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimRight(line, " \t\r") == internalStateIgnoreEntry {
			count++
		}
	}
	return count
}

// TestWriteBaseShadow_CreatesGitIgnoreIgnoringInternalState covers a project
// without a language pack: the universal helper must create .gitignore so the
// persistent .scbake/base state is never left untracked.
func TestWriteBaseShadow_CreatesGitIgnoreIgnoringInternalState(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	tc := types.TaskContext{TargetPath: root, Tx: tx}

	if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
		t.Fatalf("writeBaseShadow() failed: %v", err)
	}

	got := readGitIgnore(t, root)
	if count := countInternalStateIgnoreLines(got); count != 1 {
		t.Fatalf(".gitignore has %d .scbake/ entries, want 1:\n%s", count, got)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir, fileutil.BaseDir, "Makefile")); err != nil {
		t.Fatalf("base shadow was not written: %v", err)
	}
}

// TestWriteBaseShadow_AppendsAfterMissingTrailingNewline verifies a user's
// existing .gitignore is preserved and the new entry starts on its own line.
func TestWriteBaseShadow_AppendsAfterMissingTrailingNewline(t *testing.T) {
	root := t.TempDir()
	const initial = "node_modules"
	if err := os.WriteFile(filepath.Join(root, fileutil.GitIgnore), []byte(initial), fileutil.FilePerms); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	tc := types.TaskContext{TargetPath: root, Tx: tx}

	if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
		t.Fatalf("writeBaseShadow() failed: %v", err)
	}

	want := initial + "\n" + internalStateIgnoreEntry + "\n"
	if got := readGitIgnore(t, root); got != want {
		t.Fatalf(".gitignore = %q, want %q", got, want)
	}
}

// TestWriteBaseShadow_DoesNotDuplicateInternalStateIgnore covers every common
// spelling of an existing .scbake ignore entry. The original bytes must be
// preserved exactly, including a missing trailing newline. Leading whitespace
// is part of the Git pattern, so an indented entry is not a duplicate.
func TestWriteBaseShadow_DoesNotDuplicateInternalStateIgnore(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		want    string
	}{
		{name: ".scbake", initial: ".scbake", want: ".scbake"},
		{name: ".scbake slash", initial: ".scbake/", want: ".scbake/"},
		{name: "root .scbake", initial: "/.scbake", want: "/.scbake"},
		{name: "root .scbake slash", initial: "/.scbake/", want: "/.scbake/"},
		{name: "among other entries", initial: "*.log\n.scbake/\ntmp/\n", want: "*.log\n.scbake/\ntmp/\n"},
		{name: "trailing whitespace", initial: ".scbake/ \t\r\n", want: ".scbake/ \t\r\n"},
		{
			name:    "leading whitespace is not an ignore",
			initial: "  .scbake/  \n",
			want:    "  .scbake/  \n" + internalStateIgnoreEntry + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, fileutil.GitIgnore), []byte(tt.initial), fileutil.FilePerms); err != nil {
				t.Fatalf("seed .gitignore: %v", err)
			}

			tx, err := transaction.New(root)
			if err != nil {
				t.Fatalf("transaction.New() failed: %v", err)
			}
			tc := types.TaskContext{TargetPath: root, Tx: tx}

			if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
				t.Fatalf("writeBaseShadow() failed: %v", err)
			}

			if got := readGitIgnore(t, root); got != tt.want {
				t.Fatalf(".gitignore mismatch:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestWriteBaseShadow_IdempotentInternalStateIgnore ensures repeated writes do
// not add duplicate entries, even across different managed files.
func TestWriteBaseShadow_IdempotentInternalStateIgnore(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	tc := types.TaskContext{TargetPath: root, Tx: tx}

	for _, ledgerKey := range []string{"Makefile", "README.md", "Makefile"} {
		if err := writeBaseShadow(tc, ledgerKey, []byte("content\n")); err != nil {
			t.Fatalf("writeBaseShadow(%q) failed: %v", ledgerKey, err)
		}
	}

	got := readGitIgnore(t, root)
	if count := countInternalStateIgnoreLines(got); count != 1 {
		t.Fatalf("repeated writes produced %d .scbake/ entries, want 1:\n%s", count, got)
	}
}

// TestEnsureInternalStateIgnored_NoOps verifies the helper is inert in dry-run
// mode and when no transaction is active.
func TestEnsureInternalStateIgnored_NoOps(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	t.Run("nil transaction does not create .gitignore", func(t *testing.T) {
		root := t.TempDir()
		tc := types.TaskContext{TargetPath: root}

		if err := ensureInternalStateIgnored(tc, root); err != nil {
			t.Fatalf("ensureInternalStateIgnored() failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); !os.IsNotExist(err) {
			t.Fatalf(".gitignore should not exist without a transaction (stat err: %v)", err)
		}
	})

	t.Run("dry run does not modify existing .gitignore", func(t *testing.T) {
		root := t.TempDir()
		const initial = "node_modules\n"
		if err := os.WriteFile(filepath.Join(root, fileutil.GitIgnore), []byte(initial), fileutil.FilePerms); err != nil {
			t.Fatalf("seed .gitignore: %v", err)
		}

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New() failed: %v", err)
		}
		tc := types.TaskContext{TargetPath: root, Tx: tx, DryRun: true}

		if err := ensureInternalStateIgnored(tc, root); err != nil {
			t.Fatalf("ensureInternalStateIgnored() failed: %v", err)
		}
		if got := readGitIgnore(t, root); got != initial {
			t.Fatalf("dry run modified .gitignore: got %q, want %q", got, initial)
		}
	})

	t.Run("writeBaseShadow in dry run writes nothing", func(t *testing.T) {
		root := t.TempDir()
		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New() failed: %v", err)
		}
		tc := types.TaskContext{TargetPath: root, Tx: tx, DryRun: true}

		if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
			t.Fatalf("writeBaseShadow() failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); !os.IsNotExist(err) {
			t.Fatalf(".gitignore should not exist after dry run (stat err: %v)", err)
		}
		if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir)); !os.IsNotExist(err) {
			t.Fatalf(".scbake should not exist after dry run (stat err: %v)", err)
		}
	})
}

// TestEnsureInternalStateIgnored_SkipsManagedRootGitIgnore verifies that a
// managed root .gitignore is only reconciled through its rendered content. An
// out-of-band append here would desynchronize the ledger from the file.
func TestEnsureInternalStateIgnored_SkipsManagedRootGitIgnore(t *testing.T) {
	root := t.TempDir()
	const initial = "node_modules\n"
	if err := os.WriteFile(filepath.Join(root, fileutil.GitIgnore), []byte(initial), fileutil.FilePerms); err != nil {
		t.Fatalf("seed .gitignore: %v", err)
	}

	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	manifest := &types.Manifest{
		ManagedFiles: map[string]string{fileutil.GitIgnore: HashContent([]byte(initial))},
	}
	tc := types.TaskContext{TargetPath: root, Manifest: manifest, Tx: tx}

	if err := ensureInternalStateIgnored(tc, root); err != nil {
		t.Fatalf("ensureInternalStateIgnored() failed: %v", err)
	}
	if got := readGitIgnore(t, root); got != initial {
		t.Fatalf("managed .gitignore was modified out-of-band: got %q, want %q", got, initial)
	}
}

// TestWriteManagedFile_RootGitIgnoreInjectsEntryBeforeHashing is the regression
// test for the MEDIUM review finding: a managed root .gitignore render that
// lacks .scbake/ must have the entry injected before hashing, so the ledger,
// base shadow, and on-disk bytes agree and a repeat run is not reported as
// drift.
func TestWriteManagedFile_RootGitIgnoreInjectsEntryBeforeHashing(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "Root"}},
	}
	tc := types.TaskContext{TargetPath: root, Manifest: manifest, Tx: tx}
	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   fileutil.GitIgnore,
		Desc:         "Render root .gitignore",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	onDisk := readGitIgnore(t, root)
	want := "Hello Root from v1.0.0\n" + internalStateIgnoreEntry + "\n"
	if onDisk != want {
		t.Fatalf(".gitignore = %q, want %q", onDisk, want)
	}
	if got, wantHash := manifest.ManagedFiles[fileutil.GitIgnore], HashContent([]byte(onDisk)); got != wantHash {
		t.Fatalf("managed_files[%q] = %q, want on-disk hash %q", fileutil.GitIgnore, got, wantHash)
	}

	// A repeat reconciliation must not treat scbake's injected entry as drift.
	if err := task.Execute(tc); err != nil {
		t.Fatalf("repeat Execute() reported drift: %v", err)
	}
	if got := readGitIgnore(t, root); got != onDisk {
		t.Fatalf("repeat Execute() changed .gitignore: got %q, want %q", got, onDisk)
	}
	if got, wantHash := manifest.ManagedFiles[fileutil.GitIgnore], HashContent([]byte(onDisk)); got != wantHash {
		t.Fatalf("repeat Execute() changed the recorded hash: got %q, want %q", got, wantHash)
	}
}

// TestWriteManagedFile_SubprojectGitIgnoreNotInjected ensures the root's
// .scbake entry is not written into a subproject .gitignore. Only the
// manifest-root file owns the persistent state directory.
func TestWriteManagedFile_SubprojectGitIgnoreNotInjected(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	manifest := &types.Manifest{
		SbakeVersion: "v1.0.0",
		Projects:     []types.Project{{Name: "Sub"}},
	}
	target := filepath.Join(root, "sub")
	tc := types.TaskContext{TargetPath: target, Manifest: manifest, Tx: tx}
	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   fileutil.GitIgnore,
		Desc:         "Render subproject .gitignore",
		TaskPrio:     100,
	}

	if err := task.Execute(tc); err != nil {
		t.Fatalf("Execute() failed: %v", err)
	}

	//nolint:gosec // test-controlled temp path
	subContent, err := os.ReadFile(filepath.Join(target, fileutil.GitIgnore))
	if err != nil {
		t.Fatalf("read subproject .gitignore: %v", err)
	}
	const wantSub = "Hello Sub from v1.0.0"
	if string(subContent) != wantSub {
		t.Fatalf("subproject .gitignore = %q, want %q", subContent, wantSub)
	}
	if strings.Contains(string(subContent), internalStateIgnoreEntry) {
		t.Fatalf("subproject .gitignore inherited the root entry: %q", subContent)
	}

	key := "sub/" + fileutil.GitIgnore
	if got, wantHash := manifest.ManagedFiles[key], HashContent(subContent); got != wantHash {
		t.Fatalf("managed_files[%q] = %q, want %q", key, got, wantHash)
	}

	// The root still gets the universal entry through the fallback helper.
	if got := readGitIgnore(t, root); got != internalStateIgnoreEntry+"\n" {
		t.Fatalf("root .gitignore = %q, want %q", got, internalStateIgnoreEntry+"\n")
	}
}

// TestEnsureInternalStateIgnored_UnwritableRootGitIgnoreIsBestEffort verifies a
// read-only or symlinked root .gitignore only warns: scaffolding must continue
// and the existing file or link target must remain untouched.
func TestEnsureInternalStateIgnored_UnwritableRootGitIgnoreIsBestEffort(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	if runtime.GOOS == osWindows {
		t.Skip("permission and symlink semantics differ on Windows")
	}

	t.Run("read-only file", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root can write through read-only permissions")
		}

		root := t.TempDir()
		const initial = "node_modules\n"
		gitignore := filepath.Join(root, fileutil.GitIgnore)
		if err := os.WriteFile(gitignore, []byte(initial), fileutil.PrivateFilePerms); err != nil {
			t.Fatalf("seed .gitignore: %v", err)
		}
		if err := os.Chmod(gitignore, 0o400); err != nil {
			t.Fatalf("chmod .gitignore: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(gitignore, fileutil.PrivateFilePerms)
		})

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New() failed: %v", err)
		}
		tc := types.TaskContext{TargetPath: root, Tx: tx}

		if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
			t.Fatalf("writeBaseShadow() aborted on a read-only .gitignore: %v", err)
		}
		if got := readGitIgnore(t, root); got != initial {
			t.Fatalf("read-only .gitignore was modified: got %q, want %q", got, initial)
		}
		if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir, fileutil.BaseDir, "Makefile")); err != nil {
			t.Fatalf("base shadow was not written: %v", err)
		}
	})

	t.Run("in-root symlink", func(t *testing.T) {
		requireSymlinks(t)

		root := t.TempDir()
		const initial = "node_modules\n"
		target := filepath.Join(root, "actual-gitignore")
		if err := os.WriteFile(target, []byte(initial), fileutil.PrivateFilePerms); err != nil {
			t.Fatalf("seed symlink target: %v", err)
		}
		link := filepath.Join(root, fileutil.GitIgnore)
		if err := os.Symlink(filepath.Base(target), link); err != nil {
			t.Fatalf("create symlink: %v", err)
		}

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New() failed: %v", err)
		}
		tc := types.TaskContext{TargetPath: root, Tx: tx}

		if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
			t.Fatalf("writeBaseShadow() aborted on a symlinked .gitignore: %v", err)
		}
		//nolint:gosec // test-controlled temp path
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("read symlink target: %v", err)
		}
		if string(got) != initial {
			t.Fatalf("symlink target was modified: got %q, want %q", got, initial)
		}
		info, err := os.Lstat(link)
		if err != nil {
			t.Fatalf("lstat .gitignore: %v", err)
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf(".gitignore symlink was replaced by a regular file")
		}
	})
}

// TestWriteBaseShadow_RollbackRestoresGitIgnore verifies the .gitignore change
// participates in the transaction: a new file is removed, an edited file is
// restored byte-for-byte.
func TestWriteBaseShadow_RollbackRestoresGitIgnore(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	t.Run("newly created", func(t *testing.T) {
		root := t.TempDir()
		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New() failed: %v", err)
		}
		tc := types.TaskContext{TargetPath: root, Tx: tx}

		if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
			t.Fatalf("writeBaseShadow() failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); err != nil {
			t.Fatalf(".gitignore was not created: %v", err)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback() failed: %v", err)
		}
		if _, err := os.Stat(filepath.Join(root, fileutil.GitIgnore)); !os.IsNotExist(err) {
			t.Fatalf("rollback left the created .gitignore behind (stat err: %v)", err)
		}
	})

	t.Run("existing modified", func(t *testing.T) {
		root := t.TempDir()
		const initial = "node_modules\n"
		if err := os.WriteFile(filepath.Join(root, fileutil.GitIgnore), []byte(initial), fileutil.FilePerms); err != nil {
			t.Fatalf("seed .gitignore: %v", err)
		}

		tx, err := transaction.New(root)
		if err != nil {
			t.Fatalf("transaction.New() failed: %v", err)
		}
		tc := types.TaskContext{TargetPath: root, Tx: tx}

		if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
			t.Fatalf("writeBaseShadow() failed: %v", err)
		}
		if got := readGitIgnore(t, root); !strings.Contains(got, internalStateIgnoreEntry) {
			t.Fatalf(".gitignore was not modified: %q", got)
		}

		if err := tx.Rollback(); err != nil {
			t.Fatalf("Rollback() failed: %v", err)
		}
		if got := readGitIgnore(t, root); got != initial {
			t.Fatalf("rollback did not restore .gitignore: got %q, want %q", got, initial)
		}
	})
}
