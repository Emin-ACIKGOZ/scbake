// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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

// createTrackedFile records content before writing it, as first-party task
// writers do.
func createTrackedFile(t *testing.T, tx *Manager, path, content string) {
	t.Helper()
	if err := tx.TrackWrite(path, []byte(content)); err != nil {
		t.Fatalf("TrackWrite(%s) failed: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), fileutil.FilePerms); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// readTestFile reads a test-controlled path.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func TestRollback_CreatedFileMatchingHashRemoved(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "created.txt")
	createTrackedFile(t, tx, path, "created\n")
	tempDir := tx.tempDir

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	assertNotExist(t, path)
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedFileChangedKeptWithWarning(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "created.txt")
	createTrackedFile(t, tx, path, "transaction\n")
	if err := os.WriteFile(path, []byte("external\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("Rollback treated a kept path as an error: %v", rollbackErr)
	}
	if got := readTestFile(t, path); got != "external\n" {
		t.Errorf("kept content = %q, want %q", got, "external\n")
	}
	if !strings.Contains(stderr, "content changed since this transaction wrote it") {
		t.Errorf("rollback stderr = %q, want a content-change warning", stderr)
	}
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedFileCRLFOnlyRewriteKept(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "created.txt")
	createTrackedFile(t, tx, path, "line one\nline two\n")
	// A CRLF-only rewrite is different raw bytes even though it is the same
	// text. Rollback authority is byte-exact, so the file must be kept.
	const rewritten = "line one\r\nline two\r\n"
	if err := os.WriteFile(path, []byte(rewritten), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("Rollback treated a CRLF-only rewrite as an error: %v", rollbackErr)
	}
	if got := readTestFile(t, path); got != rewritten {
		t.Errorf("kept content = %q, want the CRLF rewrite %q", got, rewritten)
	}
	if !strings.Contains(stderr, "content changed since this transaction wrote it") {
		t.Errorf("rollback stderr = %q, want a content-change warning", stderr)
	}
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedFileReplacedByInRootSymlinkKept(t *testing.T) { //nolint:cyclop // symlink setup plus retained-target and warning assertions
	if runtime.GOOS == osWindows {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	if err := os.WriteFile(target, []byte("target\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "created.txt")
	createTrackedFile(t, tx, path, "created\n")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	// The link resolves inside the root, so containment accepts it; rollback
	// must still refuse to follow or remove a non-regular created path.
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("Rollback treated an in-root symlink replacement as an error: %v", rollbackErr)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("created path was removed: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("created path mode = %v, want a kept symlink", info.Mode())
	}
	if got := readTestFile(t, target); got != "target\n" {
		t.Errorf("symlink target content = %q, want %q", got, "target\n")
	}
	if !strings.Contains(stderr, "no longer a regular file") {
		t.Errorf("rollback stderr = %q, want a non-regular-file warning", stderr)
	}
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedFileWithoutHashKept(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "created.txt")
	if err := tx.Track(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unverified\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("Rollback treated an unhashed path as an error: %v", rollbackErr)
	}
	if got := readTestFile(t, path); got != "unverified\n" {
		t.Errorf("kept content = %q, want %q", got, "unverified\n")
	}
	if !strings.Contains(stderr, "no recorded content hash") {
		t.Errorf("rollback stderr = %q, want a missing-hash warning", stderr)
	}
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedEmptyDirectoryRemoved(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "empty")
	if err := tx.Track(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	assertNotExist(t, dir)
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedDirectoryWithExternalChildKept(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "generated")
	child := filepath.Join(dir, "external.txt")
	if err := tx.Track(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(child, []byte("external\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("Rollback treated a non-empty created directory as an error: %v", rollbackErr)
	}
	if got := readTestFile(t, child); got != "external\n" {
		t.Errorf("external child content = %q, want %q", got, "external\n")
	}
	if !strings.Contains(stderr, "created directory is not empty") {
		t.Errorf("rollback stderr = %q, want a non-empty-directory warning", stderr)
	}
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedFileReplacedByDirectoryKept(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "created.txt")
	if err := tx.TrackWrite(path, []byte("created\n")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("Rollback treated a file-to-directory replacement as an error: %v", rollbackErr)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		t.Errorf("replacement directory was not kept: info=%v err=%v", info, err)
	}
	if !strings.Contains(stderr, "created file was replaced by a directory") {
		t.Errorf("rollback stderr = %q, want a file-to-directory warning", stderr)
	}
	assertNotExist(t, tempDir)
}

func TestRollback_CreatedDirectoryReplacedByFileKept(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "created")
	if err := tx.Track(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("Rollback treated a directory-to-file replacement as an error: %v", rollbackErr)
	}
	if got := readTestFile(t, path); got != "replacement\n" {
		t.Errorf("replacement file content = %q, want %q", got, "replacement\n")
	}
	if !strings.Contains(stderr, "no recorded content hash") {
		t.Errorf("rollback stderr = %q, want a missing-hash warning", stderr)
	}
	assertNotExist(t, tempDir)
}

func TestRollback_RemovesDeepestFirstAcrossTrackingOrder(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	parent := filepath.Join(root, "parent")
	child := filepath.Join(parent, "child")
	file := filepath.Join(child, "leaf.txt")

	// Track leaf-first: depth sorting, not insertion order, must remove the
	// tree successfully with empty-only directory removal.
	if err := tx.TrackWrite(file, []byte("leaf\n")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(child); err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(child, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("leaf\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	assertNotExist(t, parent)
	assertNotExist(t, tempDir)
}

func TestRollback_AbsentCreatedPathIsNoOp(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "never-written.txt")
	if err := tx.TrackWrite(path, []byte("planned\n")); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed for an absent created path: %v", err)
	}
	assertNotExist(t, path)
	assertNotExist(t, tempDir)
}

func TestRollback_ContainmentViolationIsHardError(t *testing.T) { //nolint:cyclop // assertion-heavy containment and retained-journal check
	if runtime.GOOS == osWindows {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	if err := os.WriteFile(victim, []byte("outside\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "created.txt")
	createTrackedFile(t, tx, path, "created\n")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, path); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	err = tx.Rollback()
	if err == nil {
		t.Fatal("Rollback did not report an escaping created path as an error")
	}
	if !strings.Contains(err.Error(), "refusing to delete created path") {
		t.Errorf("Rollback error = %v, want a containment refusal", err)
	}
	if got := readTestFile(t, victim); got != "outside\n" {
		t.Errorf("outside victim content = %q, want %q", got, "outside\n")
	}
	if _, statErr := os.Lstat(path); statErr != nil {
		t.Errorf("rollback removed the escaping symlink instead of refusing: %v", statErr)
	}
	if _, statErr := os.Stat(tempDir); statErr != nil {
		t.Errorf("hard error removed the retry journal: %v", statErr)
	}
}

func TestRollback_WarningsDoNotPreventJournalAndBackupCleanup(t *testing.T) { //nolint:cyclop // setup plus restore, warning, and cleanup assertions
	root := t.TempDir()
	existing := filepath.Join(root, "existing.txt")
	if err := os.WriteFile(existing, []byte("original\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Track(existing); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("modified\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	unhashed := filepath.Join(root, "unhashed.txt")
	if err := tx.Track(unhashed); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unhashed, []byte("unverified\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir

	var rollbackErr error
	stderr := captureStderr(t, func() { rollbackErr = tx.Rollback() })
	if rollbackErr != nil {
		t.Fatalf("warning paths retained the journal: %v", rollbackErr)
	}
	if got := readTestFile(t, existing); got != "original\n" {
		t.Errorf("restored content = %q, want %q", got, "original\n")
	}
	if got := readTestFile(t, unhashed); got != "unverified\n" {
		t.Errorf("unhashed content = %q, want %q", got, "unverified\n")
	}
	if !strings.Contains(stderr, "no recorded content hash") {
		t.Errorf("rollback stderr = %q, want a missing-hash warning", stderr)
	}
	assertNotExist(t, tempDir)
}

// writeHashRecoveryJournal writes a create journal for created.txt and returns
// its transaction directory.
func writeHashRecoveryJournal(t *testing.T, root string, version int, records ...journalRecord) string {
	t.Helper()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"hash")
	writeTestJournalVersion(t, dir, version, 1<<30, "", records...)
	return dir
}

func writeRecoveryFile(t *testing.T, root, content string) string {
	t.Helper()
	path := filepath.Join(root, "created.txt")
	if err := os.WriteFile(path, []byte(content), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecoverJournalCreateHashRoundTrip(t *testing.T) { //nolint:cyclop // three journal round-trip scenarios with explicit assertions
	content := "created by the transaction\n"
	wantHash := hashBytes([]byte(content))

	t.Run("matching hash removes the file", func(t *testing.T) {
		root := t.TempDir()
		path := writeRecoveryFile(t, root, content)
		dir := writeHashRecoveryJournal(t, root, journalVersion,
			journalRecord{Op: journalOpCreate, Path: "created.txt", Hash: wantHash})

		recovered, err := Recover(root, false)
		if err != nil {
			t.Fatalf("Recover failed: %v", err)
		}
		if recovered != 1 {
			t.Errorf("Recover handled %d transactions, want 1", recovered)
		}
		assertNotExist(t, path)
		assertNotExist(t, dir)
	})

	t.Run("duplicate create keeps one path and last non-empty hash wins", func(t *testing.T) {
		root := t.TempDir()
		path := writeRecoveryFile(t, root, content)
		dir := writeHashRecoveryJournal(t, root, journalVersion,
			journalRecord{Op: journalOpCreate, Path: "created.txt", Hash: hashBytes([]byte("stale"))},
			journalRecord{Op: journalOpCreate, Path: "created.txt"},
			journalRecord{Op: journalOpCreate, Path: "created.txt", Hash: wantHash},
		)

		journal, err := readTransactionJournal(dir)
		if err != nil {
			t.Fatalf("readTransactionJournal: %v", err)
		}
		manager, err := managerFromJournal(root, dir, journal)
		if err != nil {
			t.Fatalf("managerFromJournal: %v", err)
		}
		if len(manager.created) != 1 {
			t.Errorf("duplicate create records produced %d created paths, want 1", len(manager.created))
		}
		if got := manager.createdHashes[path]; got != wantHash {
			t.Errorf("recorded hash = %q, want the last non-empty hash %q", got, wantHash)
		}

		recovered, err := Recover(root, false)
		if err != nil {
			t.Fatalf("Recover failed: %v", err)
		}
		if recovered != 1 {
			t.Errorf("Recover handled %d transactions, want 1", recovered)
		}
		assertNotExist(t, path)
		assertNotExist(t, dir)
	})

	for _, version := range []int{legacyJournalVersion, journalVersion} {
		t.Run(fmt.Sprintf("version %d create without hash keeps the file", version), func(t *testing.T) {
			root := t.TempDir()
			path := writeRecoveryFile(t, root, content)
			dir := writeHashRecoveryJournal(t, root, version,
				journalRecord{Op: journalOpCreate, Path: "created.txt"})

			recovered, err := Recover(root, false)
			if err != nil {
				t.Fatalf("Recover failed: %v", err)
			}
			if recovered != 1 {
				t.Errorf("Recover handled %d transactions, want 1", recovered)
			}
			if got := readTestFile(t, path); got != content {
				t.Errorf("kept content = %q, want %q", got, content)
			}
			assertNotExist(t, dir)
		})
	}
}

func TestRecoverContentCheckKeepsLaterWrites(t *testing.T) {
	root := t.TempDir()
	tx, err := New(root)
	if err != nil {
		t.Fatal(err)
	}

	matching := filepath.Join(root, "matching.txt")
	later := filepath.Join(root, "later.txt")
	createTrackedFile(t, tx, matching, "transaction\n")
	createTrackedFile(t, tx, later, "transaction\n")
	if err := os.WriteFile(later, []byte("external rewrite\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tempDir := tx.tempDir
	markJournalOwnerDead(t, tempDir, 1<<30)

	recovered, err := Recover(root, false)
	if err != nil {
		t.Fatalf("Recover failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}
	assertNotExist(t, matching)
	if got := readTestFile(t, later); got != "external rewrite\n" {
		t.Errorf("later write content = %q, want %q", got, "external rewrite\n")
	}
	assertNotExist(t, tempDir)
}

// FuzzRecoverJournalCreateHash checks the rollback safety invariant on parsed
// create records: recovery removes the file only when the recorded hash equals
// the raw SHA-256 of the bytes on disk, and keeps it otherwise.
func FuzzRecoverJournalCreateHash(f *testing.F) {
	f.Add("", []byte("legacy\n"))
	f.Add(hashBytes([]byte("content\n")), []byte("content\n"))
	f.Add("not-a-hex-digest", []byte("content\n"))

	f.Fuzz(func(t *testing.T, recordedHash string, fileContent []byte) {
		root := t.TempDir()
		path := filepath.Join(root, "created.txt")
		if err := os.WriteFile(path, fileContent, fileutil.FilePerms); err != nil {
			t.Fatal(err)
		}
		dir := writeHashRecoveryJournal(t, root, journalVersion,
			journalRecord{Op: journalOpCreate, Path: "created.txt", Hash: recordedHash})

		if _, err := Recover(root, false); err != nil {
			t.Fatalf("Recover failed for hash %q: %v", recordedHash, err)
		}

		_, statErr := os.Lstat(path)
		if recordedHash != "" && recordedHash == hashBytes(fileContent) {
			if !os.IsNotExist(statErr) {
				t.Fatalf("matching hash did not remove the file: %v", statErr)
			}
		} else if statErr != nil {
			t.Fatalf("unverified content was removed (hash %q): %v", recordedHash, statErr)
		}
		assertNotExist(t, dir)
	})
}
