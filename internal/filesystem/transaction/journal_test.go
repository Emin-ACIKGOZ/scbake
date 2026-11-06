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

// TestRunMarkerRejectsTempInsideProject verifies that a relative or absolute
// TMPDIR inside the checkout cannot place the run marker in the project, where
// `git add .` would commit it and a clone could replay the journal.
func TestRunMarkerRejectsTempInsideProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("TMPDIR is not used on Windows")
	}

	cases := []struct {
		name   string
		tmpDir string
		chdir  bool
	}{
		{name: "absolute", tmpDir: "tmp"},
		{name: "relative", tmpDir: "tmp", chdir: true},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			inside := filepath.Join(root, "tmp")
			if err := os.MkdirAll(inside, fileutil.DirPerms); err != nil {
				t.Fatal(err)
			}
			if tt.chdir {
				t.Chdir(root)
				t.Setenv("TMPDIR", tt.tmpDir)
			} else {
				t.Setenv("TMPDIR", inside)
			}

			nonce, err := newJournalNonce()
			if err != nil {
				t.Fatal(err)
			}
			if path, err := writeRunMarker(nonce, root); err == nil {
				t.Fatalf("writeRunMarker wrote %s inside the project root", path)
			}

			err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() && strings.HasPrefix(info.Name(), runMarkerPrefix) {
					t.Errorf("run marker leaked into the project at %s", path)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestJournalTrustedRejectsSharedTempSpoof verifies that a marker planted in
// the shared temp root, where an older scbake version looked, is ignored. Only
// the private per-user marker directory is trusted.
func TestJournalTrustedRejectsSharedTempSpoof(t *testing.T) {
	root := t.TempDir()
	nonce, err := newJournalNonce()
	if err != nil {
		t.Fatal(err)
	}
	markerDir, err := runMarkerDir(root, true)
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(markerDir, runMarkerPrefix+nonce+runMarkerSuffix)
	sharedPath := filepath.Join(os.TempDir(), runMarkerPrefix+nonce+runMarkerSuffix)
	if sharedPath == privatePath {
		t.Skip("system temp directory is already the private marker directory")
	}

	if err := os.WriteFile(sharedPath, []byte(canonicalRoot(root)+"\n"), fileutil.PrivateFilePerms); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(sharedPath) })

	if journalTrusted(&transactionJournal{Nonce: nonce}, root) {
		t.Error("journalTrusted accepted a marker from the shared temp directory")
	}
}

// TestJournalTrustedRejectsNonRegularMarker verifies that a directory at the
// marker path is not treated as a marker file.
func TestJournalTrustedRejectsNonRegularMarker(t *testing.T) {
	root := t.TempDir()
	nonce, err := newJournalNonce()
	if err != nil {
		t.Fatal(err)
	}
	markerDir, err := runMarkerDir(root, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(markerDir, runMarkerPrefix+nonce+runMarkerSuffix)
	if err := os.Mkdir(path, runMarkerDirPerms); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(path) })

	if journalTrusted(&transactionJournal{Nonce: nonce}, root) {
		t.Error("journalTrusted accepted a non-regular marker")
	}
}

// TestJournalTrustedRejectsInsecureMarker verifies that a marker readable by
// other local users is not trusted, even when its contents are correct.
func TestJournalTrustedRejectsInsecureMarker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX marker permissions are not enforced on Windows")
	}

	root := t.TempDir()
	nonce, err := newJournalNonce()
	if err != nil {
		t.Fatal(err)
	}
	markerDir, err := runMarkerDir(root, true)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(markerDir, runMarkerPrefix+nonce+runMarkerSuffix)
	if err := os.WriteFile(path, []byte(canonicalRoot(root)+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o666); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if journalTrusted(&transactionJournal{Nonce: nonce}, root) {
		t.Error("journalTrusted accepted a world-writable marker")
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
//
//nolint:cyclop // The test builds an interrupted transaction and checks both refusal and opt-in.
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
//
//nolint:cyclop // The test builds a transaction, checks its liveness, and verifies discard.
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

// TestUnverifiableLivenessExpiresToUnknown verifies that an aged PID match
// without an incarnation token is treated as unknown, not dead. A live process
// may be suspended or simply outlive the apply timeout, so recovery must not
// replay its journal without an explicit --force (or --discard).
//
//nolint:cyclop // The test checks the refusal, the unknown state, and the opt-in path.
func TestUnverifiableLivenessExpiresToUnknown(t *testing.T) {
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
	if len(pending) != 1 || pending[0].Alive || !pending[0].LivenessUnknown {
		t.Fatalf("pending = %+v, want one unknown-liveness transaction", pending)
	}

	if _, err := Recover(root, false); err == nil ||
		!strings.Contains(err.Error(), "liveness cannot be verified") {
		t.Fatalf("Recover error = %v, want an unverified-liveness refusal", err)
	}
	if _, err := os.Stat(created); err != nil {
		t.Errorf("refused recovery removed created path %s: %v", created, err)
	}

	recovered, err := RecoverWithOptions(root, RecoverOptions{Force: true})
	if err != nil {
		t.Fatalf("RecoverWithOptions --force failed: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("recovery left created path %s", created)
	}
}

// TestRecoverWithOptionsStopsOnCanceledContext verifies that cancellation is
// observed before any transaction is replayed, so an interrupted recover does
// not keep mutating files.
func TestRecoverWithOptionsStopsOnCanceledContext(t *testing.T) {
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
	recovered, err := RecoverWithOptions(root, RecoverOptions{Ctx: ctx})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RecoverWithOptions error = %v, want context.Canceled", err)
	}
	if recovered != 0 {
		t.Errorf("canceled recovery handled %d transactions, want 0", recovered)
	}
	if _, err := os.Stat(created); err != nil {
		t.Errorf("canceled recovery removed created path %s: %v", created, err)
	}
	if _, err := os.Stat(filepath.Join(tx.tempDir, journalFileName)); err != nil {
		t.Errorf("canceled recovery removed the journal: %v", err)
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

// TestJournalRejectsOversizedFile verifies that a force-added or corrupted
// journal larger than the size cap fails closed before it is parsed.
func TestJournalRejectsOversizedFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"oversized")
	writeTestJournal(t, dir, 1<<30, journalRecord{Op: journalOpCreate, Path: "created.txt"})

	path := filepath.Join(dir, journalFileName)
	if err := os.Truncate(path, maxJournalFileBytes+1); err != nil {
		t.Fatal(err)
	}

	if _, err := readTransactionJournal(dir); err == nil ||
		!strings.Contains(err.Error(), "too large") {
		t.Fatalf("readTransactionJournal error = %v, want an oversized-journal refusal", err)
	}
}

// TestJournalRejectsOverlongLine verifies that a single unbounded journal line
// cannot be accumulated in memory.
func TestJournalRejectsOverlongLine(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"longline")
	writeTestJournal(t, dir, 1<<30, journalRecord{Op: journalOpCreate, Path: "created.txt"})

	path := filepath.Join(dir, journalFileName)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, fileutil.PrivateFilePerms) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(strings.Repeat("a", maxJournalLineBytes+1)); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := readTransactionJournal(dir); err == nil ||
		!strings.Contains(err.Error(), "line longer than") {
		t.Fatalf("readTransactionJournal error = %v, want an overlong-line refusal", err)
	}
}

// TestJournalRejectsTooManyEntries verifies that an untrusted journal cannot
// make recovery accumulate an unbounded number of entries.
func TestJournalRejectsTooManyEntries(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"entries")

	records := make([]journalRecord, maxJournalEntries+1)
	for i := range records {
		records[i] = journalRecord{Op: journalOpCreate, Path: "created.txt"}
	}
	writeTestJournal(t, dir, 1<<30, records...)

	if _, err := readTransactionJournal(dir); err == nil ||
		!strings.Contains(err.Error(), "more than") {
		t.Fatalf("readTransactionJournal error = %v, want an entry-count refusal", err)
	}
}

// TestJournalRejectsNonRegularFile verifies that a directory at the journal path
// is refused instead of being read as a journal.
func TestJournalRejectsNonRegularFile(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, fileutil.InternalDir, fileutil.TmpDir, txDirPrefix+"nonregular")
	if err := os.MkdirAll(dir, fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, journalFileName), fileutil.DirPerms); err != nil {
		t.Fatal(err)
	}

	if _, err := readTransactionJournal(dir); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("readTransactionJournal error = %v, want a non-regular-file refusal", err)
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
	path, err := runMarkerPath(header.Nonce, testRootForTxDir(dir))
	if err != nil {
		t.Fatalf("resolving test run marker: %v", err)
	}
	if err := os.Remove(path); err != nil {
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
		// Windows cannot compare a start token here, so an aged PID match is
		// unknown rather than provably dead and requires the explicit opt-in.
		if !strings.Contains(err.Error(), "liveness cannot be verified") {
			t.Fatalf("Recover treated a recycled PID as the live owner: %v", err)
		}
		recovered, err = RecoverWithOptions(root, RecoverOptions{Force: true})
	}
	if err != nil {
		t.Fatalf("Recover failed after an explicit opt-in: %v", err)
	}
	if recovered != 1 {
		t.Errorf("Recover handled %d transactions, want 1", recovered)
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("recovery left created path %s", created)
	}
}
