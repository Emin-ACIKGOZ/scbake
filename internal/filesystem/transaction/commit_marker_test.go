// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"testing"
	"time"
)

// readOnlyDirPerms removes write permission so os.RemoveAll cannot unlink a
// child. Tests use it to make backup-directory cleanup fail deterministically.
const readOnlyDirPerms os.FileMode = 0o500

// newTransactionDir creates a tx-* directory under root's private state dir.
func newTransactionDir(t *testing.T, root, suffix string) string {
	t.Helper()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+suffix)
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		t.Fatalf("creating transaction dir: %v", err)
	}
	return dir
}

// ageTransactionDir moves a directory well past the stale-sweep threshold.
func ageTransactionDir(t *testing.T, dir string) {
	t.Helper()
	old := time.Now().Add(-2 * staleTransactionThreshold)
	if err := os.Chtimes(dir, old, old); err != nil {
		t.Fatalf("aging transaction dir: %v", err)
	}
}

// blockTransactionCleanup makes os.RemoveAll of dir fail while the blocker
// survives. Go's RemoveAll still deletes the other siblings it can, so the
// transaction directory remains as journal-less committed residue.
func blockTransactionCleanup(t *testing.T, dir string) func() {
	t.Helper()
	blocker := filepath.Join(dir, "!cleanup-blocker")
	if err := os.Mkdir(blocker, fileutil.DirPerms); err != nil {
		t.Fatalf("creating cleanup blocker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blocker, "keep"), []byte("keep"), fileutil.PrivateFilePerms); err != nil {
		t.Fatalf("writing cleanup blocker file: %v", err)
	}
	if err := os.Chmod(blocker, readOnlyDirPerms); err != nil {
		t.Fatalf("making cleanup blocker read-only: %v", err)
	}

	released := false
	release := func() {
		if released {
			return
		}
		released = true
		_ = os.Chmod(blocker, fileutil.DirPerms)
	}
	t.Cleanup(release)
	return release
}

func assertNoCommitMarker(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{commitMarkerFileName, commitMarkerTempName} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("commit marker %q survived a failed commit (stat error: %v)", name, err)
		}
	}
}

func assertNoCommitMarkerTemp(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(dir, commitMarkerTempName)); !os.IsNotExist(err) {
		t.Errorf("temporary commit marker survived (stat error: %v)", err)
	}
}

// assertNoCommittedRecord checks the raw journal because readEntry folds a
// committed record into Committed instead of keeping it in Entries.
func assertNoCommittedRecord(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, journalFileName)) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("reading raw journal: %v", err)
	}
	if bytes.Contains(data, []byte(`"op":"committed"`)) {
		t.Error("version 2 journal appended a committed record")
	}
}

func journalBackupName(t *testing.T, dir string) string {
	t.Helper()
	journal, err := readTransactionJournal(dir)
	if err != nil {
		t.Fatalf("reading journal: %v", err)
	}
	for _, entry := range journal.Entries {
		if entry.Op == journalOpBackup {
			return entry.Backup
		}
	}
	t.Fatal("journal has no backup record")
	return ""
}

// TestWriteCommitMarkerPublishesRegularFile covers the marker primitive
// directly: success must leave exactly a regular committed file and no temp.
func TestWriteCommitMarkerPublishesRegularFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeCommitMarker(dir); err != nil {
		t.Fatalf("writeCommitMarker failed: %v", err)
	}

	info, err := os.Lstat(filepath.Join(dir, commitMarkerFileName))
	if err != nil {
		t.Fatalf("stat commit marker: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("commit marker mode = %v, want a regular file", info.Mode())
	}
	if !commitMarkerExists(dir) {
		t.Error("commitMarkerExists rejected a regular marker")
	}
	assertNoCommitMarkerTemp(t, dir)
}

// TestCommitWritesVersionTwoJournalWithoutCommittedRecord verifies the new
// on-disk commit protocol: version 2 journals never append a committed record,
// and a successful Commit removes the transaction directory.
//
//nolint:cyclop // The test observes the journal on both sides of the commit point.
func TestCommitWritesVersionTwoJournalWithoutCommittedRecord(t *testing.T) {
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
	tempDir := tx.tempDir

	journal, err := readTransactionJournal(tempDir)
	if err != nil {
		t.Fatalf("reading journal before commit: %v", err)
	}
	if journal.Version != journalVersion {
		t.Errorf("journal version = %d, want %d", journal.Version, journalVersion)
	}
	if journal.Committed {
		t.Error("new transaction journal was already committed")
	}
	assertNoCommittedRecord(t, tempDir)

	if err := os.WriteFile(existing, []byte("committed"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	// Observe the directory at the exact commit point, before cleanup removes
	// it: the marker is regular, no temp marker remains, and the v2 journal is
	// committed without ever appending a committed record.
	markerPublished := false
	originalRename := renameCommitMarker
	renameCommitMarker = func(oldPath, newPath string) error {
		if err := originalRename(oldPath, newPath); err != nil {
			return err
		}
		markerPublished = true
		info, err := os.Lstat(filepath.Join(tempDir, commitMarkerFileName))
		if err != nil {
			t.Errorf("stat published marker: %v", err)
			return nil
		}
		if !info.Mode().IsRegular() {
			t.Errorf("published marker mode = %v, want a regular file", info.Mode())
		}
		assertNoCommitMarkerTemp(t, tempDir)
		committedJournal, err := readTransactionJournal(tempDir)
		if err != nil {
			t.Errorf("reading journal at commit point: %v", err)
			return nil
		}
		if !committedJournal.Committed {
			t.Error("journal was not committed at the marker rename")
		}
		if committedJournal.Version != journalVersion {
			t.Errorf("journal version = %d, want %d", committedJournal.Version, journalVersion)
		}
		assertNoCommittedRecord(t, tempDir)
		return nil
	}
	t.Cleanup(func() { renameCommitMarker = originalRename })

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if !markerPublished {
		t.Fatal("Commit never published the commit marker")
	}
	if _, err := os.Stat(tempDir); !os.IsNotExist(err) {
		t.Errorf("successful Commit left transaction directory %s: %v", tempDir, err)
	}
	content, err := os.ReadFile(existing) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "committed" {
		t.Errorf("committed content = %q, want %q", content, "committed")
	}
}

// markerFailureStage identifies the commit-marker operation that fails.
type markerFailureStage int

const (
	failMarkerOpen markerFailureStage = iota
	failMarkerShortWrite
	failMarkerSync
	failMarkerClose
	failMarkerRename
)

func (s markerFailureStage) String() string {
	switch s {
	case failMarkerOpen:
		return "open"
	case failMarkerShortWrite:
		return "short_write"
	case failMarkerSync:
		return "sync"
	case failMarkerClose:
		return "close"
	case failMarkerRename:
		return "rename"
	default:
		return "unknown"
	}
}

// commitMarkerFailureFile wraps the real marker file and injects one failure.
type commitMarkerFailureFile struct {
	journalFile
	stage markerFailureStage
}

func (f *commitMarkerFailureFile) Write(p []byte) (int, error) {
	if f.stage == failMarkerShortWrite {
		n := len(p) / 2
		if n == 0 {
			return 0, nil
		}
		return f.journalFile.Write(p[:n])
	}
	return f.journalFile.Write(p)
}

func (f *commitMarkerFailureFile) Sync() error {
	if f.stage == failMarkerSync {
		return errors.New("injected commit marker sync failure")
	}
	return f.journalFile.Sync()
}

func (f *commitMarkerFailureFile) Close() error {
	if err := f.journalFile.Close(); err != nil {
		return err
	}
	if f.stage == failMarkerClose {
		return errors.New("injected commit marker close failure")
	}
	return nil
}

// installMarkerFailure replaces the marker seams for one failure stage.
func installMarkerFailure(t *testing.T, stage markerFailureStage) {
	t.Helper()
	originalOpen := openCommitMarker
	originalRename := renameCommitMarker
	t.Cleanup(func() {
		openCommitMarker = originalOpen
		renameCommitMarker = originalRename
	})

	switch stage {
	case failMarkerOpen:
		openCommitMarker = func(string, string, int, os.FileMode) (journalFile, error) {
			return nil, errors.New("injected commit marker open failure")
		}
	case failMarkerRename:
		renameCommitMarker = func(string, string) error {
			return errors.New("injected commit marker rename failure")
		}
	default:
		openCommitMarker = func(baseDir, targetPath string, flag int, perm os.FileMode) (journalFile, error) {
			f, err := originalOpen(baseDir, targetPath, flag, perm)
			if err != nil {
				return nil, err
			}
			return &commitMarkerFailureFile{journalFile: f, stage: stage}, nil
		}
	}
}

// TestCommitMarkerFailureLeavesTransactionRecoverable injects a failure at
// every marker stage. None of them may publish a commit point: the journal
// stays uncommitted, New refuses to start, the backup survives, and recovery
// restores the original file.
//
//nolint:cyclop // The test drives all five failure stages through one shared invariant.
func TestCommitMarkerFailureLeavesTransactionRecoverable(t *testing.T) {
	stages := []markerFailureStage{
		failMarkerOpen,
		failMarkerShortWrite,
		failMarkerSync,
		failMarkerClose,
		failMarkerRename,
	}

	for _, stage := range stages {
		t.Run(stage.String(), func(t *testing.T) {
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
			tempDir := tx.tempDir
			backupName := journalBackupName(t, tempDir)

			installMarkerFailure(t, stage)

			if err := tx.Commit(); err == nil {
				t.Fatal("Commit succeeded despite the injected marker failure")
			} else if errors.Is(err, ErrCommitted) {
				t.Errorf("Commit reported ErrCommitted before the marker was published: %v", err)
			}
			assertNoCommitMarker(t, tempDir)

			journal, err := readTransactionJournal(tempDir)
			if err != nil {
				t.Fatalf("reading journal after failed commit: %v", err)
			}
			if journal.Committed {
				t.Error("journal became committed without a marker")
			}
			if journal.Version != journalVersion {
				t.Errorf("journal version = %d, want %d", journal.Version, journalVersion)
			}
			if _, err := os.Stat(filepath.Join(tempDir, backupName)); err != nil {
				t.Errorf("failed commit removed the backup: %v", err)
			}

			markJournalOwnerDead(t, tempDir, 1<<30)
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
				t.Fatal(err)
			}
			if string(content) != "original" {
				t.Errorf("recovered content = %q, want %q", content, "original")
			}
		})
	}
}

// TestJournalVersionOneCommittedRecordCompatibility verifies legacy journals
// keep their record-based commit point while uncommitted v1 residue is never
// swept.
func TestJournalVersionOneCommittedRecordCompatibility(t *testing.T) {
	t.Run("committed record is committed", func(t *testing.T) {
		root := t.TempDir()
		dir := newTransactionDir(t, root, "v1-committed")
		writeTestJournalVersion(t, dir, legacyJournalVersion, 1<<30, "", journalRecord{Op: journalOpCommitted})

		journal, err := readTransactionJournal(dir)
		if err != nil {
			t.Fatalf("reading v1 journal: %v", err)
		}
		if !journal.Committed {
			t.Error("v1 committed record was not treated as committed")
		}
		pending, err := ListPending(root)
		if err != nil {
			t.Fatalf("ListPending failed: %v", err)
		}
		if len(pending) != 0 {
			t.Errorf("ListPending returned %d committed v1 transactions", len(pending))
		}

		sweepStaleTransactions(root)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("sweep kept a committed v1 transaction with a dead owner: %v", err)
		}
	})

	t.Run("missing committed record is uncommitted and retained", func(t *testing.T) {
		root := t.TempDir()
		dir := newTransactionDir(t, root, "v1-open")
		writeTestJournalVersion(t, dir, legacyJournalVersion, 1<<30, "")
		ageTransactionDir(t, dir)

		journal, err := readTransactionJournal(dir)
		if err != nil {
			t.Fatalf("reading v1 journal: %v", err)
		}
		if journal.Committed {
			t.Error("v1 journal without a committed record was treated as committed")
		}

		sweepStaleTransactions(root)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("sweep removed an uncommitted v1 journal: %v", err)
		}
		if _, err := New(root); !errors.Is(err, ErrIncompleteTransaction) {
			t.Errorf("New() error = %v, want ErrIncompleteTransaction", err)
		}
	})
}

// TestVersionTwoCommittedRecordWithoutMarkerIsUncommitted is the regression
// test for the residual #131 defect: a v2 journal containing a committed record
// must not be trusted without the atomic marker. The startup sweep must keep
// it, New must refuse it, and recovery must restore the backup.
//
//nolint:cyclop // Builds the exact residual state and checks sweep, startup, and recovery.
func TestVersionTwoCommittedRecordWithoutMarkerIsUncommitted(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "config.yml")
	if err := os.WriteFile(existing, []byte("original"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	dir := newTransactionDir(t, root, "v2-residual")
	backupName := "0_config.yml"
	if err := os.WriteFile(filepath.Join(dir, backupName), []byte("original"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	writeTestJournalVersion(
		t,
		dir,
		journalVersion,
		1<<30,
		"",
		journalRecord{Op: journalOpBackup, Path: "config.yml", Backup: backupName, Mode: uint32(fileutil.FilePerms)},
		journalRecord{Op: journalOpCommitted},
	)
	if err := os.WriteFile(existing, []byte("modified"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	ageTransactionDir(t, dir)

	journal, err := readTransactionJournal(dir)
	if err != nil {
		t.Fatalf("reading v2 journal: %v", err)
	}
	if journal.Committed {
		t.Fatal("v2 journal with a committed record but no marker was treated as committed")
	}
	if journal.Version != journalVersion {
		t.Errorf("journal version = %d, want %d", journal.Version, journalVersion)
	}

	sweepStaleTransactions(root)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("startup sweep removed an uncommitted v2 journal: %v", err)
	}
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
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("recovered content = %q, want %q", content, "original")
	}
}

// TestVersionTwoCommitRequiresRegularMarker verifies every non-regular marker
// representation is rejected. A leftover temp file, symlink, or directory is
// not a commit point.
//
//nolint:cyclop // The table plus per-case setup stays just over the default threshold.
func TestVersionTwoCommitRequiresRegularMarker(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{
			name:  "none",
			setup: func(*testing.T, string) {},
			want:  false,
		},
		{
			name: "temp only",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, commitMarkerTempName), []byte("{\"version\":2}\n"), fileutil.PrivateFilePerms); err != nil {
					t.Fatal(err)
				}
			},
			want: false,
		},
		{
			name: "regular",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := writeCommitMarker(dir); err != nil {
					t.Fatalf("writeCommitMarker failed: %v", err)
				}
			},
			want: true,
		},
		{
			name: "symlink",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if runtime.GOOS == osWindows {
					t.Skip("symlink creation requires elevated privileges on Windows")
				}
				target := filepath.Join(dir, "marker-target")
				if err := os.WriteFile(target, []byte("target"), fileutil.PrivateFilePerms); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(dir, commitMarkerFileName)); err != nil {
					t.Fatal(err)
				}
			},
			want: false,
		},
		{
			name: "directory",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.Mkdir(filepath.Join(dir, commitMarkerFileName), fileutil.DirPerms); err != nil {
					t.Fatal(err)
				}
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := newTransactionDir(t, root, "marker-"+tc.name)
			writeTestJournalVersion(t, dir, journalVersion, 1<<30, "")
			tc.setup(t, dir)

			journal, err := readTransactionJournal(dir)
			if err != nil {
				t.Fatalf("reading journal: %v", err)
			}
			if journal.Committed != tc.want {
				t.Errorf("Committed = %v, want %v", journal.Committed, tc.want)
			}
			if commitMarkerExists(dir) != tc.want {
				t.Errorf("commitMarkerExists = %v, want %v", commitMarkerExists(dir), tc.want)
			}
		})
	}
}

// TestCommittedMarkerSweepHonorsOwnerLiveness verifies a committed marker is
// only reclaimed once its owner is gone, and that a live owner is left alone.
func TestCommittedMarkerSweepHonorsOwnerLiveness(t *testing.T) {
	t.Run("dead owner reclaimed", func(t *testing.T) {
		root := t.TempDir()
		dir := newTransactionDir(t, root, "dead")
		writeTestJournalVersion(t, dir, journalVersion, 1<<30, "")
		if err := writeCommitMarker(dir); err != nil {
			t.Fatal(err)
		}

		sweepStaleTransactions(root)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("sweep did not reclaim a committed transaction with a dead owner: %v", err)
		}
	})

	t.Run("live owner retained", func(t *testing.T) {
		root := t.TempDir()
		dir := newTransactionDir(t, root, "live")
		writeTestJournalVersion(t, dir, journalVersion, os.Getpid(), processStartToken(os.Getpid()))
		if err := writeCommitMarker(dir); err != nil {
			t.Fatal(err)
		}

		sweepStaleTransactions(root)
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("sweep reclaimed a committed transaction with a live owner: %v", err)
		}
		if _, err := New(root); err != nil {
			t.Fatalf("New() with a committed live transaction: %v", err)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("New() reclaimed a committed transaction with a live owner: %v", err)
		}
	})
}

// TestUnreadableJournalWithMarkerFailsClosed verifies a malformed journal is
// never swept or silently ignored even when a marker exists. A marker with no
// journal is committed residue: it is retained until the stale threshold and
// then reclaimed, never followed while it is still recent.
//
//nolint:cyclop // Covers malformed and missing journal states with distinct safety properties.
func TestUnreadableJournalWithMarkerFailsClosed(t *testing.T) {
	t.Run("malformed journal is retained and refused", func(t *testing.T) {
		root := t.TempDir()
		dir := newTransactionDir(t, root, "malformed")
		if err := os.WriteFile(filepath.Join(dir, journalFileName), []byte("not json\n"), fileutil.PrivateFilePerms); err != nil {
			t.Fatal(err)
		}
		backupName := "0_state.txt"
		if err := os.WriteFile(filepath.Join(dir, backupName), []byte("original"), fileutil.PrivateFilePerms); err != nil {
			t.Fatal(err)
		}
		if err := writeCommitMarker(dir); err != nil {
			t.Fatal(err)
		}
		ageTransactionDir(t, dir)

		if _, err := New(root); err == nil {
			t.Fatal("New accepted a malformed journal with a marker")
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("New removed a malformed journal: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, backupName)); err != nil {
			t.Errorf("New removed the backup beside a malformed journal: %v", err)
		}

		if _, err := Recover(root, false); err == nil {
			t.Fatal("Recover accepted a malformed journal with a marker")
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("Recover removed a malformed journal: %v", err)
		}
	})

	t.Run("missing journal with marker is not reclaimed early", func(t *testing.T) {
		root := t.TempDir()
		dir := newTransactionDir(t, root, "missing")
		backupName := "0_state.txt"
		if err := os.WriteFile(filepath.Join(dir, backupName), []byte("original"), fileutil.PrivateFilePerms); err != nil {
			t.Fatal(err)
		}
		if err := writeCommitMarker(dir); err != nil {
			t.Fatal(err)
		}

		if _, err := New(root); err != nil {
			t.Fatalf("New() with a recent journal-less marker directory: %v", err)
		}
		if _, err := os.Stat(dir); err != nil {
			t.Errorf("New removed a recent journal-less marker directory: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, backupName)); err != nil {
			t.Errorf("New removed a backup beside a journal-less marker: %v", err)
		}

		// A marker proves the transaction committed, so the journal-less
		// directory is stale residue once it is old enough to sweep.
		ageTransactionDir(t, dir)
		if _, err := New(root); err != nil {
			t.Fatalf("New() with stale journal-less residue: %v", err)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("sweep did not reclaim stale committed residue: %v", err)
		}
	})
}

// TestFailedCommitMarkerWritePreservesBackupForRecovery reproduces the original
// defect deterministically: the marker Sync fails, rollback also fails, and
// the next startup must still refuse because the backup is intact.
//
//nolint:cyclop // Reproduces the full marker-failure, rollback-failure, and recovery sequence.
func TestFailedCommitMarkerWritePreservesBackupForRecovery(t *testing.T) {
	if runtime.GOOS == osWindows {
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
	tempDir := tx.tempDir
	backupName := journalBackupName(t, tempDir)

	installMarkerFailure(t, failMarkerSync)
	if err := tx.Commit(); err == nil {
		t.Fatal("Commit succeeded despite the injected marker Sync failure")
	}

	// Make deferred rollback fail by replacing the restore target with an
	// escaping symlink, so the journal and backup must be preserved.
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
	if err := tx.Rollback(); err == nil {
		t.Fatal("Rollback unexpectedly succeeded through an escaping symlink")
	}

	if _, err := os.Stat(filepath.Join(tempDir, backupName)); err != nil {
		t.Errorf("failed rollback removed the backup: %v", err)
	}
	journal, err := readTransactionJournal(tempDir)
	if err != nil {
		t.Fatalf("reading journal after failed rollback: %v", err)
	}
	if journal.Committed {
		t.Error("journal became committed after a failed marker write")
	}

	markJournalOwnerDead(t, tempDir, 1<<30)
	if _, err := New(root); !errors.Is(err, ErrIncompleteTransaction) {
		t.Fatalf("New() error = %v, want ErrIncompleteTransaction", err)
	}

	// Remove the escaping symlink so recovery can restore the original file.
	if err := os.Remove(existing); err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	if string(content) != "original" {
		t.Errorf("recovered content = %q, want %q", content, "original")
	}
	data, err := os.ReadFile(outside) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside" {
		t.Errorf("recovery touched a path outside the root: %q", data)
	}
}
