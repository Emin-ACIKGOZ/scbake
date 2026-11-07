// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"scbake/internal/util/fileutil"
	"testing"
)

const (
	markerNone uint8 = iota
	markerTemp
	markerRegular
	markerSymlink
	markerDirectory

	fuzzJournalKinds = 3
	fuzzMarkerStates = 5
	fuzzDeadPID      = 1 << 30
)

// FuzzTransactionCommitState explores the interaction between journal format,
// committed records, marker state, owner liveness, directory age, backups, and
// unreadable journals.
//
// Invariants:
//   - A version 2 journal is committed only when a regular marker exists.
//   - A version 1 journal keeps its record-based commit point.
//   - An uncommitted or malformed journal is never removed by the sweep.
//   - New refuses an uncommitted dead transaction and any unreadable journal.
//   - Recovery restores every backup or fails while preserving journal+backups.
//   - No path outside the root is touched.
//
//nolint:cyclop // The target intentionally enumerates every state combination.
func FuzzTransactionCommitState(f *testing.F) {
	// versionSel, committedOp, markerSel, liveSel, oldSel, backupSel, journalSel
	f.Add(uint8(0), uint8(0), markerRegular, uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(1), uint8(0), markerRegular, uint8(0), uint8(0), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(1), markerNone, uint8(0), uint8(1), uint8(0), uint8(0))
	f.Add(uint8(0), uint8(0), markerNone, uint8(0), uint8(1), uint8(1), uint8(0))
	f.Add(uint8(1), uint8(1), markerNone, uint8(0), uint8(1), uint8(1), uint8(0))
	f.Add(uint8(1), uint8(0), markerTemp, uint8(1), uint8(0), uint8(1), uint8(0))
	f.Add(uint8(1), uint8(0), markerDirectory, uint8(0), uint8(0), uint8(0), uint8(2))
	f.Add(uint8(1), uint8(0), markerNone, uint8(0), uint8(0), uint8(0), uint8(1))

	f.Fuzz(func(
		t *testing.T,
		versionSel, committedOp, markerSel, liveSel, oldSel, backupSel, journalSel uint8,
	) {
		root := t.TempDir()
		outside := t.TempDir()
		outsidePath := filepath.Join(outside, "sentinel")
		if err := os.WriteFile(outsidePath, []byte("outside"), fileutil.FilePerms); err != nil {
			t.Fatalf("writing outside sentinel: %v", err)
		}

		dir := newTransactionDir(t, root, "fuzz")

		version := legacyJournalVersion
		if versionSel%2 == 1 {
			version = journalVersion
		}

		live := liveSel%2 == 1
		pid := fuzzDeadPID
		ownerStart := ""
		if live {
			pid = os.Getpid()
			ownerStart = processStartToken(pid)
		}

		backupPresent := backupSel%2 == 1
		original := filepath.Join(root, "state.txt")
		backupName := "0_state.txt"
		var records []journalRecord
		if backupPresent {
			if err := os.WriteFile(original, []byte("original"), fileutil.FilePerms); err != nil {
				t.Fatalf("writing original: %v", err)
			}
			if err := os.WriteFile(filepath.Join(dir, backupName), []byte("original"), fileutil.PrivateFilePerms); err != nil {
				t.Fatalf("writing backup: %v", err)
			}
			records = append(records, journalRecord{
				Op:     journalOpBackup,
				Path:   "state.txt",
				Backup: backupName,
				Mode:   uint32(fileutil.FilePerms),
			})
			if err := os.WriteFile(original, []byte("modified"), fileutil.FilePerms); err != nil {
				t.Fatalf("writing modified original: %v", err)
			}
		}
		if committedOp%2 == 1 {
			records = append(records, journalRecord{Op: journalOpCommitted})
		}

		journalKind := journalSel % fuzzJournalKinds
		switch journalKind {
		case 0:
			writeTestJournalVersion(t, dir, version, pid, ownerStart, records...)
		case 1:
			if err := os.WriteFile(filepath.Join(dir, journalFileName), nil, fileutil.PrivateFilePerms); err != nil {
				t.Fatalf("writing empty journal: %v", err)
			}
		default:
			if err := os.WriteFile(filepath.Join(dir, journalFileName), []byte("not json\n"), fileutil.PrivateFilePerms); err != nil {
				t.Fatalf("writing malformed journal: %v", err)
			}
		}

		markerState := markerSel % fuzzMarkerStates
		switch markerState {
		case markerTemp:
			if err := os.WriteFile(filepath.Join(dir, commitMarkerTempName), []byte("{\"version\":2}\n"), fileutil.PrivateFilePerms); err != nil {
				t.Fatalf("writing temp marker: %v", err)
			}
		case markerRegular:
			if err := writeCommitMarker(dir); err != nil {
				t.Fatalf("writing regular marker: %v", err)
			}
		case markerSymlink:
			if runtime.GOOS != osWindows {
				if err := os.Symlink(outsidePath, filepath.Join(dir, commitMarkerFileName)); err != nil {
					t.Fatalf("writing symlink marker: %v", err)
				}
			}
		case markerDirectory:
			if err := os.Mkdir(filepath.Join(dir, commitMarkerFileName), fileutil.DirPerms); err != nil {
				t.Fatalf("writing directory marker: %v", err)
			}
		}

		if oldSel%2 == 1 {
			ageTransactionDir(t, dir)
		}

		committed := false
		if journalKind == 0 {
			journal, err := readTransactionJournal(dir)
			if err != nil {
				t.Fatalf("reading valid journal: %v", err)
			}
			expectedCommitted := false
			if version == journalVersion {
				// A version 2 journal must ignore any committed record.
				expectedCommitted = markerState == markerRegular
			} else {
				expectedCommitted = committedOp%2 == 1
			}
			if journal.Committed != expectedCommitted {
				t.Fatalf("Committed = %v, want %v (version=%d marker=%d committedOp=%d)",
					journal.Committed, expectedCommitted, version, markerState, committedOp)
			}
			committed = journal.Committed
		} else if _, err := readTransactionJournal(dir); err == nil {
			t.Fatalf("journal kind %d unexpectedly parsed", journalKind)
		}

		sweepStaleTransactions(root)
		_, statErr := os.Lstat(dir)
		dirExists := statErr == nil

		switch {
		case journalKind != 0:
			if !dirExists {
				t.Fatal("startup sweep removed an unreadable journal")
			}
		case committed && !live:
			if dirExists {
				t.Fatal("startup sweep kept a committed transaction with a dead owner")
			}
		case committed && live:
			if !dirExists {
				t.Fatal("startup sweep removed a committed transaction with a live owner")
			}
		default:
			if !dirExists {
				t.Fatal("startup sweep removed an uncommitted journal")
			}
		}

		_, newErr := New(root)
		switch {
		case journalKind != 0:
			if newErr == nil {
				t.Fatal("New accepted an unreadable journal")
			}
		case committed:
			if newErr != nil {
				t.Fatalf("New rejected a committed transaction: %v", newErr)
			}
		case live:
			// A live owner may still be writing; New lets it continue.
			if newErr != nil {
				t.Fatalf("New rejected a live transaction: %v", newErr)
			}
		default:
			if !errors.Is(newErr, ErrIncompleteTransaction) {
				t.Fatalf("New error = %v, want ErrIncompleteTransaction", newErr)
			}
		}

		if data, err := os.ReadFile(outsidePath); err != nil || string(data) != "outside" { //nolint:gosec // test-controlled path
			t.Fatalf("path outside the root was touched: data=%q err=%v", data, err)
		}

		if !dirExists || live {
			return
		}

		if journalKind != 0 {
			if _, err := Recover(root, false); err == nil {
				t.Fatal("Recover accepted an unreadable journal")
			}
			if _, err := os.Stat(dir); err != nil {
				t.Fatalf("failed recovery removed the unreadable journal: %v", err)
			}
			if backupPresent {
				if _, err := os.Stat(filepath.Join(dir, backupName)); err != nil {
					t.Fatalf("failed recovery removed the backup: %v", err)
				}
			}
			return
		}

		if committed {
			if _, err := Recover(root, false); err != nil {
				t.Fatalf("Recover failed for a committed transaction: %v", err)
			}
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Fatalf("Recover did not clean a committed transaction: %v", err)
			}
			return
		}

		recovered, err := Recover(root, false)
		if err != nil {
			t.Fatalf("Recover failed for an uncommitted transaction: %v", err)
		}
		if recovered != 1 {
			t.Fatalf("Recover handled %d transactions, want 1", recovered)
		}
		if backupPresent {
			data, err := os.ReadFile(original) //nolint:gosec // test-controlled path
			if err != nil || string(data) != "original" {
				t.Fatalf("recovery did not restore the backup: data=%q err=%v", data, err)
			}
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("successful recovery left the transaction directory: %v", err)
		}
	})
}
