// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/filesystem/transaction"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr while fn runs and returns everything it
// wrote. Tests using it must not call t.Parallel.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	readEnd, writeEnd, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	original := os.Stderr
	os.Stderr = writeEnd
	defer func() {
		os.Stderr = original
		_ = readEnd.Close()
	}()

	fn()

	if err := writeEnd.Close(); err != nil {
		os.Stderr = original
		t.Fatalf("close stderr pipe: %v", err)
	}
	os.Stderr = original

	out, err := io.ReadAll(readEnd)
	if err != nil {
		t.Fatalf("read stderr: %v", err)
	}
	return string(out)
}

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

// countIgnoreLine counts standalone occurrences of entry. Leading whitespace is
// part of a Git pattern, so only trailing whitespace is ignored.
func countIgnoreLine(content, entry string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimRight(line, " \t\r") == entry {
			count++
		}
	}
	return count
}

// requireCommittedBaseBlock fails unless all three .scbake-base re-include
// lines appear exactly once and exactly one supported .scbake ignore spelling
// is present.
func requireCommittedBaseBlock(t *testing.T, content string) {
	t.Helper()
	for _, entry := range committedBaseIgnoreEntries {
		if got := countIgnoreLine(content, entry); got != 1 {
			t.Fatalf("%q appears %d times, want exactly 1:\n%s", entry, got, content)
		}
	}
	spellings := 0
	for _, entry := range []string{".scbake", ".scbake/", "/.scbake", "/.scbake/"} {
		spellings += countIgnoreLine(content, entry)
	}
	if spellings != 1 {
		t.Fatalf("found %d .scbake ignore spellings, want exactly 1:\n%s", spellings, content)
	}
}

// committedBaseIgnoreBlock is the exact append expected on a file that already
// carries a .scbake/ entry.
const committedBaseIgnoreBlock = "!.scbake-base/\n!.scbake-base/**\n!.gitignore\n"

// TestWriteBaseShadow_CreatesGitIgnoreIgnoringInternalState covers a project
// without a language pack: the universal helper must create .gitignore so the
// private .scbake state is ignored while the committed .scbake-base tree is
// re-included for Git.
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
	requireCommittedBaseBlock(t, got)
	if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")); err != nil {
		t.Fatalf("committed base shadow was not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, fileutil.InternalDir, fileutil.BaseDir)); !os.IsNotExist(err) {
		t.Fatalf("legacy .scbake/base must not be created (stat err: %v)", err)
	}
}

// TestWriteBaseShadow_AppendsAfterMissingTrailingNewline verifies a user's
// existing .gitignore is preserved and each new entry starts on its own line.
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

	want := initial + "\n" + internalStateIgnoreEntry + "\n" + committedBaseIgnoreBlock
	if got := readGitIgnore(t, root); got != want {
		t.Fatalf(".gitignore = %q, want %q", got, want)
	}
}

// TestWriteBaseShadow_DoesNotDuplicateInternalStateIgnore covers every common
// spelling of an existing .scbake ignore entry plus pre-existing and partial
// committed-base blocks. The original bytes must be preserved exactly,
// including a missing trailing newline, and only missing lines are appended.
// Leading whitespace is part of the Git pattern, so an indented entry is not a
// duplicate.
func TestWriteBaseShadow_DoesNotDuplicateInternalStateIgnore(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		want    string
	}{
		{
			name:    ".scbake",
			initial: ".scbake",
			want:    ".scbake\n" + committedBaseIgnoreBlock,
		},
		{
			name:    ".scbake slash",
			initial: ".scbake/",
			want:    ".scbake/\n" + committedBaseIgnoreBlock,
		},
		{
			name:    "root .scbake",
			initial: "/.scbake",
			want:    "/.scbake\n" + committedBaseIgnoreBlock,
		},
		{
			name:    "root .scbake slash",
			initial: "/.scbake/",
			want:    "/.scbake/\n" + committedBaseIgnoreBlock,
		},
		{
			name:    "among other entries",
			initial: "*.log\n.scbake/\ntmp/\n",
			want:    "*.log\n.scbake/\ntmp/\n" + committedBaseIgnoreBlock,
		},
		{
			name:    "trailing whitespace",
			initial: ".scbake/ \t\r\n",
			want:    ".scbake/ \t\r\n" + committedBaseIgnoreBlock,
		},
		{
			name:    "leading whitespace is not an ignore",
			initial: "  .scbake/  \n",
			want:    "  .scbake/  \n" + internalStateIgnoreEntry + "\n" + committedBaseIgnoreBlock,
		},
		{
			name:    "complete block is unchanged",
			initial: ".scbake/\n" + committedBaseIgnoreBlock,
			want:    ".scbake/\n" + committedBaseIgnoreBlock,
		},
		{
			name:    "partial block appends only the missing lines",
			initial: ".scbake/\n!.scbake-base/\n",
			want:    ".scbake/\n!.scbake-base/\n!.scbake-base/**\n!.gitignore\n",
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

			got := readGitIgnore(t, root)
			if got != tt.want {
				t.Fatalf(".gitignore mismatch:\n got %q\nwant %q", got, tt.want)
			}
			requireCommittedBaseBlock(t, got)
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
	requireCommittedBaseBlock(t, got)
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

// TestEnsureInternalStateIgnored_ManagedRootGitIgnoreIsNotMutated verifies that
// a managed root .gitignore is only reconciled through its rendered content. If
// it is missing, no longer ignores .scbake, or no longer re-includes
// .scbake-base, the helper warns with regeneration guidance instead of mutating
// the file and desynchronizing the ledger.
func TestEnsureInternalStateIgnored_ManagedRootGitIgnoreIsNotMutated(t *testing.T) { //nolint:cyclop // assertion-heavy regression test
	tests := []struct {
		name    string
		initial string
		missing bool
	}{
		{name: "missing file", missing: true},
		{name: "missing entry", initial: "node_modules\n"},
		{name: "missing base re-include", initial: ".scbake/\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			gitignore := filepath.Join(root, fileutil.GitIgnore)
			if !tt.missing {
				if err := os.WriteFile(gitignore, []byte(tt.initial), fileutil.FilePerms); err != nil {
					t.Fatalf("seed .gitignore: %v", err)
				}
			}

			tx, err := transaction.New(root)
			if err != nil {
				t.Fatalf("transaction.New() failed: %v", err)
			}
			recordedHash := HashContent([]byte(tt.initial))
			manifest := &types.Manifest{
				ManagedFiles: map[string]string{fileutil.GitIgnore: recordedHash},
			}
			tc := types.TaskContext{TargetPath: root, Manifest: manifest, Tx: tx}

			out := captureStderr(t, func() {
				if err := ensureInternalStateIgnored(tc, root); err != nil {
					t.Fatalf("ensureInternalStateIgnored() failed: %v", err)
				}
			})

			if !strings.Contains(out, "Managed .gitignore") ||
				!strings.Contains(out, "regenerate it with 'scbake apply' or 'scbake update'") {
				t.Fatalf("warning does not mention regeneration:\n%s", out)
			}
			if !strings.Contains(out, "is missing the .scbake/ ignore or the .scbake-base/ re-include") {
				t.Fatalf("warning does not describe the stale ignore:\n%s", out)
			}

			if tt.missing {
				if _, err := os.Stat(gitignore); !os.IsNotExist(err) {
					t.Fatalf("missing managed .gitignore was recreated (stat err: %v)", err)
				}
			} else {
				got := readGitIgnore(t, root)
				if got != tt.initial {
					t.Fatalf("managed .gitignore was modified out-of-band: got %q, want %q", got, tt.initial)
				}
				if hash := HashContent([]byte(got)); hash != recordedHash {
					t.Fatalf("helper created drift: on-disk hash %q, recorded %q", hash, recordedHash)
				}
			}
			if got := manifest.ManagedFiles[fileutil.GitIgnore]; got != recordedHash {
				t.Fatalf("helper changed the recorded hash: got %q, want %q", got, recordedHash)
			}
		})
	}
}

// TestEnsureInternalStateIgnored_PropagatesWriteFailure forces trackAndWrite to
// fail after the pre-write check by targeting a missing parent directory. The
// error must propagate so the caller rolls back instead of committing a
// partially written .gitignore.
func TestEnsureInternalStateIgnored_PropagatesWriteFailure(t *testing.T) {
	root := t.TempDir()
	tx, err := transaction.New(root)
	if err != nil {
		t.Fatalf("transaction.New() failed: %v", err)
	}
	missingRoot := filepath.Join(root, "missing-parent")
	tc := types.TaskContext{TargetPath: missingRoot, Tx: tx}

	err = ensureInternalStateIgnored(tc, missingRoot)
	if err == nil {
		t.Fatal("ensureInternalStateIgnored() swallowed a trackAndWrite failure")
	}
	if !strings.Contains(err.Error(), fileutil.GitIgnore) {
		t.Fatalf("write failure does not mention .gitignore: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(missingRoot, fileutil.GitIgnore)); !os.IsNotExist(statErr) {
		t.Fatalf("failure left a .gitignore behind (stat err: %v)", statErr)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback() failed: %v", err)
	}
	if _, statErr := os.Stat(missingRoot); !os.IsNotExist(statErr) {
		t.Fatalf("rollback left the missing parent directory behind (stat err: %v)", statErr)
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
	want := "Hello Root from v1.0.0\n" + internalStateIgnoreEntry + "\n" + committedBaseIgnoreBlock
	if onDisk != want {
		t.Fatalf(".gitignore = %q, want %q", onDisk, want)
	}
	requireCommittedBaseBlock(t, onDisk)
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

// TestWriteManagedFile_EquivalentRootGitIgnorePaths verifies injection keys on
// the root-relative ledger entry, not the spelling of the output path. Dot
// prefixes, parent traversal that resolves to the root, and absolute in-root
// paths all normalize to the root .gitignore, while a subproject file does not.
func TestWriteManagedFile_EquivalentRootGitIgnorePaths(t *testing.T) { //nolint:cyclop // table-driven path regression test
	tests := []struct {
		name       string
		outputPath func(root string) string
		wantKey    string
		wantInject bool
	}{
		{
			name:       "dot prefixed",
			outputPath: func(string) string { return "./" + fileutil.GitIgnore },
			wantKey:    fileutil.GitIgnore,
			wantInject: true,
		},
		{
			name:       "parent traversal",
			outputPath: func(string) string { return filepath.Join("sub", "..", fileutil.GitIgnore) },
			wantKey:    fileutil.GitIgnore,
			wantInject: true,
		},
		{
			name:       "absolute in-root",
			outputPath: func(root string) string { return filepath.Join(root, fileutil.GitIgnore) },
			wantKey:    fileutil.GitIgnore,
			wantInject: true,
		},
		{
			name:       "subproject",
			outputPath: func(string) string { return filepath.Join("sub", fileutil.GitIgnore) },
			wantKey:    "sub/" + fileutil.GitIgnore,
			wantInject: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tx, err := transaction.New(root)
			if err != nil {
				t.Fatalf("transaction.New() failed: %v", err)
			}
			manifest := &types.Manifest{SbakeVersion: "v1.0.0"}
			tc := types.TaskContext{TargetPath: root, Manifest: manifest, Tx: tx}

			const initial = "vendor/\n"
			if err := WriteManagedFile(tc, tt.outputPath(root), []byte(initial), fileutil.FilePerms); err != nil {
				t.Fatalf("WriteManagedFile() failed: %v", err)
			}

			onDisk, err := fileutil.SafeReadFile(root, tt.wantKey)
			if err != nil {
				t.Fatalf("read %s: %v", tt.wantKey, err)
			}
			want := initial
			if tt.wantInject {
				want += internalStateIgnoreEntry + "\n" + committedBaseIgnoreBlock
			}
			if string(onDisk) != want {
				t.Fatalf("%s = %q, want %q", tt.wantKey, onDisk, want)
			}
			if tt.wantInject {
				requireCommittedBaseBlock(t, string(onDisk))
			} else if strings.Contains(string(onDisk), "!.scbake-base/") {
				t.Fatalf("subproject %s inherited the committed-base block: %q", tt.wantKey, onDisk)
			}
			if got, wantHash := manifest.ManagedFiles[tt.wantKey], HashContent(onDisk); got != wantHash {
				t.Fatalf("managed_files[%q] = %q, want on-disk hash %q", tt.wantKey, got, wantHash)
			}

			// A later base-shadow write must not append the entry again.
			if err := writeBaseShadow(tc, "Makefile", []byte("all:\n")); err != nil {
				t.Fatalf("writeBaseShadow() failed: %v", err)
			}
			after, err := fileutil.SafeReadFile(root, tt.wantKey)
			if err != nil {
				t.Fatalf("read %s after base shadow: %v", tt.wantKey, err)
			}
			if string(after) != want {
				t.Fatalf("%s changed after base shadow write: got %q, want %q", tt.wantKey, after, want)
			}
			if tt.wantInject {
				requireCommittedBaseBlock(t, string(after))
			}
		})
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

	// The root still gets the universal ignore and committed-base block through
	// the fallback helper.
	wantRoot := internalStateIgnoreEntry + "\n" + committedBaseIgnoreBlock
	if got := readGitIgnore(t, root); got != wantRoot {
		t.Fatalf("root .gitignore = %q, want %q", got, wantRoot)
	}
	requireCommittedBaseBlock(t, readGitIgnore(t, root))
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
		if _, err := os.Stat(filepath.Join(root, fileutil.CommittedBaseDir, "Makefile")); err != nil {
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
