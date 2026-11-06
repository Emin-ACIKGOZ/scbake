// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package transaction

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestJournalTrustedRejectsFIFOMarker verifies that a FIFO at the marker path
// is rejected before it is opened. Reading a FIFO with no writer blocks, which
// used to hang read-only commands such as `recover --dry-run`.
func TestJournalTrustedRejectsFIFOMarker(t *testing.T) {
	root := t.TempDir()
	nonce, err := newJournalNonce()
	if err != nil {
		t.Fatal(err)
	}
	markerDir, err := runMarkerDir(root, true)
	if err != nil {
		t.Fatal(err)
	}

	// Create the FIFO both at the private path (which the current code must
	// Lstat before opening) and at the legacy shared-temp path (which the
	// previous implementation opened and blocked on).
	privatePath := filepath.Join(markerDir, runMarkerPrefix+nonce+runMarkerSuffix)
	legacyPath := filepath.Join(os.TempDir(), runMarkerPrefix+nonce+runMarkerSuffix)
	for _, path := range []string{privatePath, legacyPath} {
		if err := syscall.Mkfifo(path, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(path) })
	}

	done := make(chan bool, 1)
	go func() {
		done <- journalTrusted(&transactionJournal{Nonce: nonce}, root)
	}()

	select {
	case trusted := <-done:
		if trusted {
			t.Error("journalTrusted accepted a FIFO marker")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("journalTrusted blocked while opening a FIFO marker")
	}
}

// TestReadJournalRejectsFIFO verifies that a FIFO at the journal path is
// rejected before it is opened, so a read-only command cannot block on it.
func TestReadJournalRejectsFIFO(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".scbake", "tmp", txDirPrefix+"fifo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, journalFileName), 0o600); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := readTransactionJournal(dir)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("readTransactionJournal error = %v, want a non-regular-file refusal", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readTransactionJournal blocked while opening a FIFO")
	}
}
