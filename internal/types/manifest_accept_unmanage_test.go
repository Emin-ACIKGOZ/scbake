// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package types

import (
	"reflect"
	"testing"
)

// TestManifestDeepCopyAcceptedUnmanagedIndependent pins that the additive
// accepted map and unmanaged slice are copied, not aliased.
func TestManifestDeepCopyAcceptedUnmanagedIndependent(t *testing.T) {
	original := &Manifest{
		SbakeVersion:   "v0.3.0",
		ManagedFiles:   map[string]string{"main.go": "managed"},
		AcceptedFiles:  map[string]string{"main.go": "accepted"},
		UnmanagedFiles: []string{"a.txt", "b.txt"},
	}

	copied := original.DeepCopy()
	if !reflect.DeepEqual(copied.AcceptedFiles, original.AcceptedFiles) {
		t.Fatalf("AcceptedFiles not copied: %v", copied.AcceptedFiles)
	}
	if !reflect.DeepEqual(copied.UnmanagedFiles, original.UnmanagedFiles) {
		t.Fatalf("UnmanagedFiles not copied: %v", copied.UnmanagedFiles)
	}

	copied.AcceptedFiles["main.go"] = "mutated"
	copied.AcceptedFiles["extra.txt"] = "extra"
	copied.UnmanagedFiles[0] = "mutated"
	copied.UnmanagedFiles = append(copied.UnmanagedFiles, "c.txt")

	if got := original.AcceptedFiles["main.go"]; got != "accepted" {
		t.Errorf("copy mutation leaked into the original accepted map: %q", got)
	}
	if _, ok := original.AcceptedFiles["extra.txt"]; ok {
		t.Errorf("added key leaked into the original accepted map")
	}
	if got := original.UnmanagedFiles; !reflect.DeepEqual(got, []string{"a.txt", "b.txt"}) {
		t.Errorf("copy mutation leaked into the original unmanaged slice: %v", got)
	}
}

// TestManifestDeepCopyEmptyAdditiveFields: nil additive fields are safe to copy
// and the normalized empty containers are independent.
func TestManifestDeepCopyEmptyAdditiveFields(t *testing.T) {
	original := &Manifest{}
	copied := original.DeepCopy()
	if copied == nil {
		t.Fatal("DeepCopy returned nil")
	}
	if len(copied.AcceptedFiles) != 0 {
		t.Errorf("AcceptedFiles = %v, want empty", copied.AcceptedFiles)
	}
	if copied.UnmanagedFiles != nil {
		t.Errorf("UnmanagedFiles = %v, want nil", copied.UnmanagedFiles)
	}
	copied.AcceptedFiles["x"] = "y"
	if len(original.AcceptedFiles) != 0 {
		t.Errorf("original accepted map mutated: %v", original.AcceptedFiles)
	}
}

// TestManifestDeepCopyPreservesSortedOrder: the copy keeps the stored order so a
// load/copy/save cycle does not reorder unmanaged_files. The input is
// deliberately unsorted: sorting is the writer's job, so a DeepCopy that
// normalizes the slice must fail here.
func TestManifestDeepCopyPreservesSortedOrder(t *testing.T) {
	want := []string{"z", "a", "m"}
	original := &Manifest{UnmanagedFiles: append([]string(nil), want...)}

	copied := original.DeepCopy()
	if !reflect.DeepEqual(copied.UnmanagedFiles, want) {
		t.Fatalf("copy reordered unmanaged files: got %v, want %v", copied.UnmanagedFiles, want)
	}
	if !reflect.DeepEqual(original.UnmanagedFiles, want) {
		t.Fatalf("DeepCopy reordered the original: got %v, want %v", original.UnmanagedFiles, want)
	}

	// The copy must stay independent: reordering it must not touch the original.
	copied.UnmanagedFiles[0], copied.UnmanagedFiles[2] = copied.UnmanagedFiles[2], copied.UnmanagedFiles[0]
	if !reflect.DeepEqual(original.UnmanagedFiles, want) {
		t.Errorf("copy mutation leaked into the original: got %v, want %v", original.UnmanagedFiles, want)
	}
}
