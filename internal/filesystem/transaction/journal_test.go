// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"context"
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

// TestRecoverContextCanceledBeforeRecovery verifies that a canceled context is
// returned instead of reporting an empty recovery or touching the transaction.
//
//nolint:cyclop // Covers list, recover, and discard cancellation paths.
func TestRecoverContextCanceledBeforeRecovery(t *testing.T) {
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
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := ListPendingContext(ctx, root); !errors.Is(err, context.Canceled) {
		t.Errorf("ListPendingContext error = %v, want context.Canceled", err)
	}
	recovered, err := RecoverContext(ctx, root, false)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("RecoverContext error = %v, want context.Canceled", err)
	}
	if recovered != 0 {
		t.Errorf("RecoverContext reported %d recoveries with a canceled context", recovered)
	}
	if _, err := os.Stat(created); err != nil {
		t.Errorf("canceled recovery touched a created path: %v", err)
	}
	if _, err := os.Stat(tx.tempDir); err != nil {
		t.Errorf("canceled recovery removed the transaction directory: %v", err)
	}

	recovered, err = RecoverContext(ctx, root, true)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("discard RecoverContext error = %v, want context.Canceled", err)
	}
	if recovered != 0 {
		t.Errorf("discard RecoverContext reported %d discards with a canceled context", recovered)
	}
	if _, err := os.Stat(tx.tempDir); err != nil {
		t.Errorf("canceled discard removed the transaction directory: %v", err)
	}
}

// cancelWhenGoneContext reports context.Canceled as soon as path disappears.
// It lets a test stop recovery deterministically between journal entries
// without racing a timer.
type cancelWhenGoneContext struct {
	context.Context
	path     string
	canceled bool
}

func (c *cancelWhenGoneContext) Err() error {
	if c.canceled {
		return context.Canceled
	}
	if _, err := os.Stat(c.path); os.IsNotExist(err) {
		c.canceled = true
		return context.Canceled
	}
	return nil
}

// TestRecoverContextStopsBetweenEntries verifies that cancellation between
// journal entries preserves the remaining paths and the transaction directory
// for a retry, and surfaces the context error.
func TestRecoverContextStopsBetweenEntries(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(root, "first.txt")
	second := filepath.Join(root, "second.txt")
	for _, path := range []string{first, second} {
		if err := tx.Track(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("partial"), fileutil.FilePerms); err != nil {
			t.Fatal(err)
		}
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	// Created paths are deleted in LIFO order, so second disappears first and
	// the next context check cancels before first is touched.
	ctx := &cancelWhenGoneContext{Context: context.Background(), path: second}
	recovered, err := RecoverContext(ctx, root, false)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("RecoverContext error = %v, want context.Canceled", err)
	}
	if recovered != 0 {
		t.Errorf("RecoverContext reported %d recoveries before cancellation", recovered)
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Errorf("recovery did not process the first entry: %v", err)
	}
	if _, err := os.Stat(first); err != nil {
		t.Errorf("recovery continued after cancellation and removed %s: %v", first, err)
	}
	if _, err := os.Stat(tx.tempDir); err != nil {
		t.Errorf("canceled recovery removed the transaction directory: %v", err)
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
	if _, err := Recover(root, true); err == nil {
		t.Fatal("Recover --discard touched a transaction owned by a live process")
	}
	if _, err := os.Stat(created); err != nil {
		t.Errorf("live transaction path was modified: %v", err)
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

// closeFailFile closes the underlying journal file successfully, then reports
// an injected error to simulate a Close failure after a successful Sync.
type closeFailFile struct{ journalFile }

func (f *closeFailFile) Close() error {
	if err := f.journalFile.Close(); err != nil {
		return err
	}
	return errors.New("injected journal close failure")
}

// TestCommitRemainsCommittedAfterJournalCloseError verifies that a committed
// record which was written and fsynced is treated as durable even when the
// subsequent Close fails. Commit must return the error, but deferred rollback
// must not undo the run or its backups.
//
//nolint:cyclop // Sets up a backup, injects a close failure, and checks both durability and rollback.
func TestCommitRemainsCommittedAfterJournalCloseError(t *testing.T) {
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
	if err := os.WriteFile(existing, []byte("committed"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	originalOpener := openJournalForAppend
	t.Cleanup(func() { openJournalForAppend = originalOpener })
	openJournalForAppend = func(baseDir, targetPath string, flag int, perm os.FileMode) (journalFile, error) {
		f, err := originalOpener(baseDir, targetPath, flag, perm)
		if err != nil {
			return nil, err
		}
		return &closeFailFile{journalFile: f}, nil
	}

	if err := tx.Commit(); err == nil {
		t.Fatal("Commit did not report the injected journal close error")
	} else if !errors.Is(err, ErrCommitted) {
		t.Errorf("Commit error = %v, want ErrCommitted", err)
	}
	if !tx.committed {
		t.Error("Commit did not treat the fsynced committed record as durable")
	}

	journal, err := readTransactionJournal(tx.tempDir)
	if err != nil {
		t.Fatalf("reading committed journal: %v", err)
	}
	if !journal.Committed {
		t.Error("committed record was not durable on disk")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback after committed close error failed: %v", err)
	}
	content, err := os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "committed" {
		t.Errorf("Rollback undid a durably committed run: content = %q", content)
	}
}

// TestTrackBackupCloseErrorKeepsRecoverableBackup verifies that a durable
// backup record is not left pointing at a deleted backup when closing the
// journal fails after Sync. Recovery must still be able to restore the file.
//
//nolint:cyclop // Creates a backup, injects a close failure, and verifies crash recovery.
func TestTrackBackupCloseErrorKeepsRecoverableBackup(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "config.yml")
	if err := os.WriteFile(existing, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	originalOpener := openJournalForAppend
	t.Cleanup(func() { openJournalForAppend = originalOpener })
	openJournalForAppend = func(baseDir, targetPath string, flag int, perm os.FileMode) (journalFile, error) {
		f, err := originalOpener(baseDir, targetPath, flag, perm)
		if err != nil {
			return nil, err
		}
		return &closeFailFile{journalFile: f}, nil
	}

	if err := tx.Track(existing); err == nil {
		t.Fatal("Track did not report the injected journal close error")
	}
	if _, ok := tx.backups[existing]; !ok {
		t.Error("Track dropped the in-memory backup after a durable record")
	}

	journal, err := readTransactionJournal(tx.tempDir)
	if err != nil {
		t.Fatalf("reading journal after the close error: %v", err)
	}
	if len(journal.Entries) != 1 || journal.Entries[0].Op != journalOpBackup {
		t.Fatalf("journal entries = %+v, want one durable backup record", journal.Entries)
	}

	// Simulate a crash after the close error. The original was modified as if a
	// caller had proceeded with the write before the process died.
	if err := os.WriteFile(existing, []byte("modified"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	markJournalOwnerDead(t, tx.tempDir, 1<<30)

	recovered, err := Recover(root, false)
	if err != nil {
		t.Fatalf("Recover failed after a durable backup close error: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}
	content, err := os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("recovered content = %q, want %q", content, "original")
	}
}

// writeTestJournal writes a syntactically valid journal with the supplied
// records and owner pid.
func writeTestJournal(t *testing.T, dir string, pid int, records ...journalRecord) {
	t.Helper()
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	header := journalRecord{Version: journalVersion, PID: pid, Started: time.Now().UTC()}
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
// belongs to an unrelated process is still recoverable.
func TestRecoverIgnoresRecycledPID(t *testing.T) {
	if processStartToken(os.Getpid()) == "" {
		t.Skip("process start tokens are unavailable on this platform")
	}

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
