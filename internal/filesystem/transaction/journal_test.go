// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"strings"
	"testing"
	"time"
)

// markJournalOwnerDead rewrites the journal header so recovery treats the
// transaction as belonging to a process that is no longer running.
func markJournalOwnerDead(t *testing.T, dir string, pid int) {
	t.Helper()
	rewriteJournalOwner(t, dir, pid, "")
}

// rewriteJournalOwner replaces the journal owner fields for a test scenario.
func rewriteJournalOwner(t *testing.T, dir string, pid int, ownerStart string) {
	t.Helper()

	path := filepath.Join(dir, journalFileName)
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading journal: %v", err)
	}

	lines := strings.Split(string(data), "\n")
	var header journalRecord
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("decoding journal header: %v", err)
	}
	header.PID = pid
	header.OwnerStart = ownerStart
	rewritten, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("encoding journal header: %v", err)
	}
	lines[0] = string(rewritten)

	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), fileutil.PrivateFilePerms); err != nil { //nolint:gosec // test-controlled path
		t.Fatalf("rewriting journal: %v", err)
	}
}

// TestRecoverRestoresInterruptedTransaction simulates a SIGKILL after a backup
// and a few creates, then verifies that recovery restores the original file and
// removes the paths the interrupted run created.
//
//nolint:cyclop // The test constructs a backup and two created paths in one scenario.
func TestRecoverRestoresInterruptedTransaction(t *testing.T) {
	root := t.TempDir()

	existing := filepath.Join(root, "config.yml")
	if err := os.WriteFile(existing, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	if err := tx.Track(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("modified"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	createdDir := filepath.Join(root, "generated")
	if err := tx.Track(createdDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(createdDir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	createdFile := filepath.Join(createdDir, "file.txt")
	if err := tx.Track(createdFile); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(createdFile, []byte("partial"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	// Simulate the crash: close the journal without committing or rolling back,
	// then mark the owner as a process that is not running.
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	if _, err := New(root); !errors.Is(err, ErrIncompleteTransaction) {
		t.Fatalf("New() error = %v, want ErrIncompleteTransaction", err)
	}

	recovered, err := Recover(root, false)
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}

	content, err := os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading restored file: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("restored content = %q, want %q", content, "original")
	}
	if _, err := os.Stat(createdDir); !os.IsNotExist(err) {
		t.Errorf("recovery left created directory %s", createdDir)
	}
	if _, err := os.Stat(tx.tempDir); !os.IsNotExist(err) {
		t.Errorf("recovery left transaction directory %s", tx.tempDir)
	}
}

// TestRecoverRequiresLocalRunMarker verifies that a journal without a matching
// run marker, such as one checked out through Git, is not replayed unless the
// caller explicitly opts in.
func TestRecoverRequiresLocalRunMarker(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "config.yml")
	if err := os.WriteFile(existing, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("modified"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)
	if err := os.Remove(tx.markerPath); err != nil {
		t.Fatalf("removing run marker: %v", err)
	}

	if _, err := Recover(root, false); err == nil ||
		!strings.Contains(err.Error(), "no local run marker") {
		t.Fatalf("Recover error = %v, want a missing-marker refusal", err)
	}
	content, err := os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "modified" {
		t.Errorf("recovery without opt-in changed %s to %q", existing, content)
	}
	if _, err := os.Stat(filepath.Join(tx.tempDir, journalFileName)); err != nil {
		t.Errorf("refused recovery removed the journal: %v", err)
	}

	recovered, err := RecoverWithOptions(root, RecoverOptions{Force: true})
	if err != nil {
		t.Fatalf("RecoverWithOptions --force failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("force recovery handled %d transactions, want 1", recovered)
	}
	content, err = os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("force recovery restored %q, want %q", content, "original")
	}
}

// TestRecoverDoesNotRemoveRootWithoutLocalMarker verifies that a journal found
// in a checkout cannot delete the project root even when its header carries
// remove_root: true. The explicit --force opt-in remains available.
func TestRecoverDoesNotRemoveRootWithoutLocalMarker(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"clone")
	writeTestJournal(t, dir, 1<<30, journalRecord{Op: journalOpCreate, Path: "keep.txt"})
	setTestJournalRemoveRoot(t, dir, true)
	removeTestJournalMarker(t, dir)

	if _, err := Recover(root, false); err == nil ||
		!strings.Contains(err.Error(), "no local run marker") {
		t.Fatalf("Recover error = %v, want a missing-marker refusal", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("untrusted recovery deleted project content: %v", err)
	}

	recovered, err := RecoverWithOptions(root, RecoverOptions{Force: true})
	if err != nil {
		t.Fatalf("RecoverWithOptions --force failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("force recovery handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Errorf("explicit force recovery did not remove the bootstrapped root: %v", err)
	}
}

// TestRecoverRefusesLiveTransaction ensures recovery never touches a
// transaction owned by a running process.
func TestRecoverRefusesLiveTransaction(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	created := filepath.Join(root, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("data"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	if _, err := Recover(root, false); err == nil {
		t.Fatal("Recover touched a transaction owned by a live process")
	}
	// Discard is refused only when the platform can prove the live PID is the
	// recorded incarnation. Where it cannot, --discard is the explicit escape
	// hatch and is covered by TestRecoverDiscardWhenLivenessUnverified.
	journal, err := readTransactionJournal(tx.tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if journalOwnerVerifiablyAlive(journal) {
		if _, err := Recover(root, true); err == nil {
			t.Fatal("Recover --discard touched a transaction owned by a live process")
		}
	}
	if _, err := os.Stat(created); err != nil {
		t.Errorf("live transaction path was modified: %v", err)
	}
}

// TestRecoverDiscardWhenLivenessUnverified verifies that --discard can clear a
// transaction whose owner PID appears alive but whose incarnation cannot be
// verified. This is the escape hatch for PID-reuse false positives.
func TestRecoverDiscardWhenLivenessUnverified(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	created := filepath.Join(root, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("data"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	// Name a live PID without an incarnation token. On Linux this falls back to
	// kill(pid, 0), which reports alive; the other platforms behave the same.
	rewriteJournalOwner(t, tx.tempDir, os.Getpid(), "")

	pending, err := ListPending(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || !pending[0].Alive {
		t.Fatalf("pending = %+v, want one Alive transaction", pending)
	}

	recovered, err := RecoverWithOptions(root, RecoverOptions{Discard: true})
	if err != nil {
		t.Fatalf("discard refused an unverifiable liveness check: %v", err)
	}
	if recovered != 1 {
		t.Errorf("discard handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(tx.tempDir); !os.IsNotExist(err) {
		t.Errorf("discard left transaction directory %s", tx.tempDir)
	}
	if _, err := os.Stat(created); err != nil {
		t.Errorf("discard removed user-visible path %s: %v", created, err)
	}
}

// TestUnverifiableLivenessExpires verifies that a PID match without an
// incarnation token stops blocking recovery once the transaction is older than
// the bounded window, covering PID reuse on macOS and Windows.
func TestUnverifiableLivenessExpires(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	created := filepath.Join(root, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("partial"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	rewriteJournalOwner(t, tx.tempDir, os.Getpid(), "")
	setTestJournalStarted(t, tx.tempDir, time.Now().Add(-2*unverifiedLivenessWindow))

	pending, err := ListPending(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Alive {
		t.Fatalf("pending = %+v, want one non-Alive transaction", pending)
	}

	recovered, err := Recover(root, false)
	if err != nil {
		t.Fatalf("Recover did not expire an unverifiable PID: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("recovery left created path %s", created)
	}
}

// TestRecoverDiscardRemovesTransaction verifies the explicit discard path.
func TestRecoverDiscardRemovesTransaction(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	created := filepath.Join(root, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("data"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	recovered, err := Recover(root, true)
	if err != nil {
		t.Fatalf("Recover --discard failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover --discard handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(tx.tempDir); !os.IsNotExist(err) {
		t.Errorf("discard left transaction directory %s", tx.tempDir)
	}
	if _, err := os.Stat(created); err != nil {
		t.Errorf("discard unexpectedly removed user-visible path %s: %v", created, err)
	}
}

// TestJournalRejectsMalformedTransaction verifies recovery fails closed on a
// corrupted journal instead of deleting unknown paths.
func TestJournalRejectsMalformedTransaction(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(filepath.Join(root, "created.txt")); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	path := filepath.Join(tx.tempDir, journalFileName)
	if err := os.WriteFile(path, []byte("not json\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	if _, err := ListPending(root); err == nil {
		t.Fatal("ListPending accepted a malformed journal")
	}
	if _, err := Recover(root, false); err == nil {
		t.Fatal("Recover accepted a malformed journal")
	}
	if _, err := os.Stat(tx.tempDir); err != nil {
		t.Errorf("malformed journal directory was removed: %v", err)
	}
	if _, err := Recover(root, true); err != nil {
		t.Fatalf("discard failed for a malformed journal: %v", err)
	}
	if _, err := os.Stat(tx.tempDir); !os.IsNotExist(err) {
		t.Errorf("discard left a malformed journal directory: %v", err)
	}
}

// writeTestJournal writes a syntactically valid journal with the supplied
// records and owner pid. It also writes the local run marker that makes the
// journal trusted; tests that need an untrusted journal remove the marker.
func writeTestJournal(t *testing.T, dir string, pid int, records ...journalRecord) {
	t.Helper()
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	nonce, err := newJournalNonce()
	if err != nil {
		t.Fatal(err)
	}
	header := journalRecord{
		Version: journalVersion,
		PID:     pid,
		Nonce:   nonce,
		Started: time.Now().UTC(),
	}
	var lines []string
	for _, record := range append([]journalRecord{header}, records...) {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, string(data))
	}
	path := filepath.Join(dir, journalFileName)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	markerPath, err := writeRunMarker(nonce, testRootForTxDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(markerPath) })
}

// testRootForTxDir recovers <root> from <root>/.scbake/tmp/tx-*.
func testRootForTxDir(dir string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(dir)))
}

// readTestJournalHeader parses the first line of a test journal.
func readTestJournalHeader(t *testing.T, dir string) journalRecord {
	t.Helper()
	path := filepath.Join(dir, journalFileName)
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading journal: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	var header journalRecord
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatalf("decoding journal header: %v", err)
	}
	return header
}

// writeTestJournalHeader replaces the first line of a test journal.
func writeTestJournalHeader(t *testing.T, dir string, header journalRecord) {
	t.Helper()
	path := filepath.Join(dir, journalFileName)
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading journal: %v", err)
	}
	lines := strings.Split(string(data), "\n")
	rewritten, err := json.Marshal(header)
	if err != nil {
		t.Fatalf("encoding journal header: %v", err)
	}
	lines[0] = string(rewritten)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), fileutil.PrivateFilePerms); err != nil { //nolint:gosec // test-controlled path
		t.Fatalf("rewriting journal: %v", err)
	}
}

// setTestJournalStarted sets the header start time in a test journal.
func setTestJournalStarted(t *testing.T, dir string, started time.Time) {
	t.Helper()
	header := readTestJournalHeader(t, dir)
	header.Started = started
	writeTestJournalHeader(t, dir, header)
}

// setTestJournalRemoveRoot sets the remove_root flag in a test journal header.
func setTestJournalRemoveRoot(t *testing.T, dir string, value bool) {
	t.Helper()
	header := readTestJournalHeader(t, dir)
	header.RemoveRoot = value
	writeTestJournalHeader(t, dir, header)
}

// removeTestJournalMarker deletes the local marker that makes a test journal
// trusted, simulating a journal copied from another checkout.
func removeTestJournalMarker(t *testing.T, dir string) {
	t.Helper()
	header := readTestJournalHeader(t, dir)
	if !validJournalNonce(header.Nonce) {
		t.Fatalf("test journal has invalid nonce %q", header.Nonce)
	}
	if err := os.Remove(runMarkerPath(header.Nonce)); err != nil {
		t.Fatalf("removing test run marker: %v", err)
	}
}

// TestRecoverRejectsRootPath ensures a journal entry that normalizes to the
// project root cannot make recovery delete the entire tree.
func TestRecoverRejectsRootPath(t *testing.T) {
	for _, raw := range []string{"", "."} {
		t.Run(raw, func(t *testing.T) {
			root := t.TempDir()
			keep := filepath.Join(root, "keep.txt")
			if err := os.WriteFile(keep, []byte("keep"), fileutil.FilePerms); err != nil {
				t.Fatal(err)
			}

			dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"root")
			writeTestJournal(t, dir, 1<<30, journalRecord{Op: journalOpCreate, Path: raw})

			if _, err := Recover(root, false); err == nil {
				t.Fatal("Recover accepted a path that resolves to the project root")
			}
			if _, err := os.Stat(keep); err != nil {
				t.Errorf("recovery deleted the project root: %v", err)
			}
		})
	}
}

// TestRecoverPreservesJournalOnRestoreFailure ensures a failed restore does not
// remove the only copy of the journal and backups.
//
//nolint:cyclop // The test builds an interrupted transaction and a failing restore.
func TestRecoverPreservesJournalOnRestoreFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	root := t.TempDir()
	existing := filepath.Join(root, "config.yml")
	if err := os.WriteFile(existing, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("modified"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	// Replace the restore target with an escaping symlink so restoreBackups
	// fails after recovery has begun.
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, existing); err != nil {
		t.Fatal(err)
	}

	if _, err := Recover(root, false); err == nil {
		t.Fatal("Recover unexpectedly succeeded despite a failed restore")
	}
	if _, err := os.Stat(filepath.Join(tx.tempDir, journalFileName)); err != nil {
		t.Errorf("failed recovery removed the journal: %v", err)
	}
	if entries, err := os.ReadDir(tx.tempDir); err != nil || len(entries) < 2 {
		t.Errorf("failed recovery removed the backups: entries=%d err=%v", len(entries), err)
	}
}

// TestRecoverDiscardEmptyJournal verifies the explicit discard path works when
// the journal cannot be parsed.
func TestRecoverDiscardEmptyJournal(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"empty")
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, journalFileName), nil, fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}

	if _, err := Recover(root, false); err == nil {
		t.Fatal("normal recovery accepted an empty journal")
	}
	recovered, err := Recover(root, true)
	if err != nil {
		t.Fatalf("discard failed for an empty journal: %v", err)
	}
	if recovered != 1 {
		t.Errorf("discard handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("discard left an empty journal directory: %v", err)
	}
}

// TestJournalIgnoresTornTrailingRecord verifies that a crash during the final
// journal append does not discard the valid records before it. The mutation
// described by the torn record never started, so ignoring it is safe.
//
//nolint:cyclop // The test builds a backup, a create, and a torn trailing record.
func TestJournalIgnoresTornTrailingRecord(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "config.yml")
	if err := os.WriteFile(existing, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("modified"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(root, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("partial"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	journalPath := filepath.Join(tx.tempDir, journalFileName)
	f, err := os.OpenFile(journalPath, os.O_APPEND|os.O_WRONLY, fileutil.PrivateFilePerms) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"op":"create","path":"torn`); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	recovered, err := Recover(root, false)
	if err != nil {
		t.Fatalf("Recover failed on a torn trailing record: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}

	content, err := os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("restored content = %q, want %q", content, "original")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("recovery left created path %s", created)
	}
}

// TestRecoverThroughSymlinkedRoot verifies that journaled relative paths can be
// recovered when the project is addressed through a symlinked spelling.
//
//nolint:cyclop // The test builds a backup, a create, and a symlinked root.
func TestRecoverThroughSymlinkedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	realRoot := t.TempDir()
	existing := filepath.Join(realRoot, "config.yml")
	if err := os.WriteFile(existing, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("modified"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	created := filepath.Join(realRoot, "created.txt")
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("partial"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	link := filepath.Join(t.TempDir(), "project")
	if err := os.Symlink(realRoot, link); err != nil {
		t.Fatal(err)
	}

	if _, err := Recover(link, false); err != nil {
		t.Fatalf("Recover through symlink failed: %v", err)
	}
	content, err := os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("restored content = %q, want %q", content, "original")
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("recovery left created path %s", created)
	}
}

// TestRecoverIgnoresRecycledPID verifies that a journal owned by a PID that now
// belongs to an unrelated process is still recoverable. Linux detects the
// different incarnation token; macOS and Windows rely on the bounded age for a
// PID they cannot verify, so the scenario ages the journal as well.
func TestRecoverIgnoresRecycledPID(t *testing.T) {
	root := t.TempDir()
	created := filepath.Join(root, "created.txt")
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(created); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(created, []byte("partial"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	// The header now names this live process but with a different start time,
	// exactly what PID reuse looks like to recovery.
	rewriteJournalOwner(t, tx.tempDir, os.Getpid(), "1")
	setTestJournalStarted(t, tx.tempDir, time.Now().Add(-2*unverifiedLivenessWindow))

	recovered, err := Recover(root, false)
	if err != nil {
		t.Fatalf("Recover treated a recycled PID as the live owner: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("recovery left created path %s", created)
	}
}
