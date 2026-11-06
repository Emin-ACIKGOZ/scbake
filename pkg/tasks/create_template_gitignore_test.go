// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"os"
	"path/filepath"
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

// countInternalStateIgnoreLines counts standalone .scbake/ entries.
func countInternalStateIgnoreLines(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == internalStateIgnoreEntry {
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
	//nolint:gosec // test-controlled temp path
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
// preserved exactly, including a missing trailing newline.
func TestWriteBaseShadow_DoesNotDuplicateInternalStateIgnore(t *testing.T) {
	tests := []struct {
		name    string
		initial string
	}{
		{name: ".scbake", initial: ".scbake"},
		{name: ".scbake slash", initial: ".scbake/"},
		{name: "root .scbake", initial: "/.scbake"},
		{name: "root .scbake slash", initial: "/.scbake/"},
		{name: "among other entries", initial: "*.log\n.scbake/\ntmp/\n"},
		{name: "with surrounding whitespace", initial: "  .scbake/  \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			//nolint:gosec // test-controlled temp path
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

			if got := readGitIgnore(t, root); got != tt.initial {
				t.Fatalf(".gitignore was modified:\n got %q\nwant %q", got, tt.initial)
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
func TestEnsureInternalStateIgnored_NoOps(t *testing.T) {
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
		//nolint:gosec // test-controlled temp path
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

// TestWriteBaseShadow_RollbackRestoresGitIgnore verifies the .gitignore change
// participates in the transaction: a new file is removed, an edited file is
// restored byte-for-byte.
func TestWriteBaseShadow_RollbackRestoresGitIgnore(t *testing.T) {
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
		//nolint:gosec // test-controlled temp path
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
