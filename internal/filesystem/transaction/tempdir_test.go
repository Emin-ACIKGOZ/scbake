// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTempDirStartsJournalBeforeReturning verifies that TempDir publishes a
// durable transaction journal first, so a crash while a scaffolding child runs
// is visible to ListPending and Recover.
func TestTempDirStartsJournalBeforeReturning(t *testing.T) {
	root := t.TempDir()
	m, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = m.Rollback() })

	tempDir, err := m.TempDir()
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if !strings.HasPrefix(filepath.Base(tempDir), txDirPrefix) {
		t.Errorf("TempDir = %q, want a %s* directory", tempDir, txDirPrefix)
	}
	if _, err := os.Stat(filepath.Join(tempDir, journalFileName)); err != nil {
		t.Fatalf("journal was not created before TempDir returned: %v", err)
	}

	assertSinglePendingDir(t, root, tempDir)

	second, err := m.TempDir()
	if err != nil {
		t.Fatalf("second TempDir: %v", err)
	}
	if second != tempDir {
		t.Errorf("TempDir is not idempotent: %q then %q", tempDir, second)
	}
}

// assertSinglePendingDir checks that ListPending reports exactly the temp dir
// created by TempDir. Compare by file identity, not path spelling: ListPending
// canonicalizes the temp directory (on macOS /var resolves to /private/var,
// and Windows path normalization can change 8.3 short names).
func assertSinglePendingDir(t *testing.T, root, tempDir string) {
	t.Helper()
	pending, err := ListPending(root)
	if err != nil {
		t.Fatalf("ListPending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("ListPending returned %d transactions, want 1", len(pending))
	}
	pendingInfo, err := os.Stat(pending[0].Dir)
	if err != nil {
		t.Fatalf("stat pending dir %q: %v", pending[0].Dir, err)
	}
	tempInfo, err := os.Stat(tempDir)
	if err != nil {
		t.Fatalf("stat TempDir %q: %v", tempDir, err)
	}
	if !os.SameFile(pendingInfo, tempInfo) {
		t.Errorf("pending dir = %q, want the same directory as %q", pending[0].Dir, tempDir)
	}
}

// TestRollbackRemovesStagingTree verifies that the transaction owns everything
// created under its temp dir: rollback removes a populated staging tree and the
// structural .scbake directories.
func TestRollbackRemovesStagingTree(t *testing.T) {
	root := t.TempDir()
	m, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tempDir, err := m.TempDir()
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	staging := filepath.Join(tempDir, "stage-example", "web")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatalf("creating staging tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "package.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("writing staging file: %v", err)
	}

	if err := m.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("rollback left the transaction temp dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("rollback left structural transaction state: %v", err)
	}
}

// TestCommitRemovesStagingTree verifies that a successful commit also removes
// the staging tree with the transaction temp dir.
func TestCommitRemovesStagingTree(t *testing.T) {
	root := t.TempDir()
	m, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tempDir, err := m.TempDir()
	if err != nil {
		t.Fatalf("TempDir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tempDir, "stage-example"), 0o750); err != nil {
		t.Fatalf("creating staging tree: %v", err)
	}
	if err := m.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("commit left the transaction temp dir: %v", err)
	}
}

// TestRecoverRemovesStagingTree verifies that existing recovery removes a
// crashed transaction's staging tree along with its tx-* directory.
func TestRecoverRemovesStagingTree(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".scbake", "tmp", txDirPrefix+"recover")
	writeTestJournal(t, dir, 1<<30)
	staging := filepath.Join(dir, "stage-example", "web")
	if err := os.MkdirAll(staging, 0o750); err != nil {
		t.Fatalf("creating staging tree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(staging, "package.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("writing staging file: %v", err)
	}

	recovered, err := Recover(root, false)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("recovery left the transaction staging tree: %v", err)
	}
}

// TestRecoverLeavesForeignTempDirectories verifies that recovery only touches
// transaction-owned tx-* directories, never unrelated data under .scbake/tmp.
func TestRecoverLeavesForeignTempDirectories(t *testing.T) {
	root := t.TempDir()
	foreign := filepath.Join(root, ".scbake", "tmp", "scaffold-userdata")
	if err := os.MkdirAll(foreign, 0o750); err != nil {
		t.Fatalf("creating foreign dir: %v", err)
	}
	keep := filepath.Join(foreign, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep\n"), 0o600); err != nil {
		t.Fatalf("writing foreign file: %v", err)
	}

	for _, discard := range []bool{false, true} {
		recovered, err := Recover(root, discard)
		if err != nil {
			t.Fatalf("Recover(discard=%v): %v", discard, err)
		}
		if recovered != 0 {
			t.Errorf("Recover(discard=%v) recovered %d transactions, want 0", discard, recovered)
		}
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("Recover(discard=%v) removed foreign data: %v", discard, err)
		}
	}
}
