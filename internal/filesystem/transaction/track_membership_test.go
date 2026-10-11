// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package transaction

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestTrack_CreatedSetMirrorsSlice is a regression test for issue #57
// (SCB-061). alreadyTracked now answers membership from createdSet instead of
// scanning the created slice, so the two structures must stay in exact sync:
// every recorded path is in both, and neither holds an extra entry.
func TestTrack_CreatedSetMirrorsSlice(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	const n = 128
	trackDistinct(t, tx, rootDir, n)

	if len(tx.created) != n {
		t.Fatalf("created slice has %d entries; want %d", len(tx.created), n)
	}
	if len(tx.createdSet) != n {
		t.Fatalf("createdSet has %d entries; want %d", len(tx.createdSet), n)
	}

	for _, p := range tx.created {
		if _, ok := tx.createdSet[p]; !ok {
			t.Errorf("created entry %s is missing from createdSet", p)
		}
	}
	for p := range tx.createdSet {
		if !containsString(tx.created, p) {
			t.Errorf("createdSet entry %s is missing from created slice", p)
		}
	}
}

// trackDistinct tracks n distinct, not-yet-existing paths under root/d.
func trackDistinct(t *testing.T, tx *Manager, root string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		p := filepath.Join(root, "d", fmt.Sprintf("f%03d.txt", i))
		if err := tx.Track(p); err != nil {
			t.Fatalf("Track(%q) failed: %v", p, err)
		}
	}
}

// containsString reports whether needle is present in haystack.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// TestAlreadyTracked_UsesMembershipSet is the regression test for issue #57
// (SCB-061). It fails if alreadyTracked is mutated back into a linear scan of
// the created slice: with a path present only in createdSet and another present
// only in the created slice, a set-based lookup and a slice scan disagree on
// both, so reverting the implementation flips exactly one assertion.
func TestAlreadyTracked_UsesMembershipSet(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	sliceOnly := filepath.Join(rootDir, "slice-only.txt")
	setOnly := filepath.Join(rootDir, "set-only.txt")

	// Construct the two divergent states directly: one path is known only to
	// the LIFO slice, the other only to the membership set.
	tx.created = append(tx.created, sliceOnly)
	tx.createdSet[setOnly] = struct{}{}

	if tx.alreadyTracked(sliceOnly) {
		t.Error("alreadyTracked matched a path that is only in the created slice; " +
			"membership must be answered from createdSet (O(1)), not a linear scan")
	}
	if !tx.alreadyTracked(setOnly) {
		t.Error("alreadyTracked ignored a path recorded in createdSet")
	}
}

// TestTrack_DeduplicatesRepeatedCreatedPaths pins the behavior alreadyTracked
// provided with a linear scan: tracking the same not-yet-existing path many
// times records it once.
func TestTrack_DeduplicatesRepeatedCreatedPaths(t *testing.T) {
	rootDir := t.TempDir()
	tx, err := New(rootDir)
	if err != nil {
		t.Fatalf("New() failed: %v", err)
	}

	p := filepath.Join(rootDir, "once.txt")
	for i := 0; i < 1000; i++ {
		if err := tx.Track(p); err != nil {
			t.Fatalf("Track iteration %d failed: %v", i, err)
		}
	}

	if len(tx.created) != 1 {
		t.Errorf("repeated Track recorded %d created entries; want 1", len(tx.created))
	}
	if len(tx.createdSet) != 1 {
		t.Errorf("repeated Track left %d createdSet entries; want 1", len(tx.createdSet))
	}
}
