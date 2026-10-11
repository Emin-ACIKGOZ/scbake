// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package merge

import (
	"bytes"
	"testing"
)

func TestMerge_IdentityLaws(t *testing.T) {
	base := []byte("a\nb\nc\nd\n")
	ours := []byte("a\nB\nc\nd\n")
	theirs := []byte("a\nb\nc\nD\n")

	cases := []struct {
		name               string
		base, ours, theirs []byte
		want               []byte
	}{
		{"no local edit yields theirs", base, base, theirs, theirs},
		{"no template edit yields ours", base, ours, base, ours},
		{"both agree yields that", base, ours, ours, ours},
		{"all equal yields base", base, base, base, base},
		{"no trailing newline preserved", []byte("x"), []byte("x"), []byte("y"), []byte("y")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, status := Merge(tc.base, tc.ours, tc.theirs)
			if status != Clean {
				t.Fatalf("status = %v, want Clean", status)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Merge() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMerge_DisjointEditsCombine(t *testing.T) {
	base := []byte("line1\nline2\nline3\nline4\nline5\n")
	ours := []byte("line1\nline2-local\nline3\nline4\nline5\n")
	theirs := []byte("line1\nline2\nline3\nline4-template\nline5\n")

	got, status := Merge(base, ours, theirs)
	if status != Clean {
		t.Fatalf("status = %v, want Clean; got:\n%s", status, got)
	}
	// I4: both sides' changes are conserved.
	if !bytes.Contains(got, []byte("line2-local")) {
		t.Error("local edit was dropped")
	}
	if !bytes.Contains(got, []byte("line4-template")) {
		t.Error("template edit was dropped")
	}
}

func TestMerge_OverlappingEditsConflict(t *testing.T) {
	base := []byte("a\nshared\nb\n")
	ours := []byte("a\nshared-local\nb\n")
	theirs := []byte("a\nshared-template\nb\n")

	got, status := Merge(base, ours, theirs)
	if status != Conflict {
		t.Fatalf("status = %v, want Conflict; got:\n%s", status, got)
	}
	for _, marker := range []string{"<<<<<<< " + OursLabel, "=======", ">>>>>>> " + TheirsLabel} {
		if !bytes.Contains(got, []byte(marker)) {
			t.Errorf("missing conflict marker %q in:\n%s", marker, got)
		}
	}
	if !bytes.Contains(got, []byte("shared-local")) || !bytes.Contains(got, []byte("shared-template")) {
		t.Error("conflict must retain both sides")
	}
}

func TestMerge_AdjacentChangesConflict(t *testing.T) {
	// Two touching (adjacent) regions are conservatively treated as one
	// cluster: the algorithm never guesses an ordering between them.
	base := []byte("a\nb\nc\nd\n")
	ours := []byte("a\nb-local\nc\nd\n")
	theirs := []byte("a\nb\nc-theirs\nd\n")

	if _, status := Merge(base, ours, theirs); status != Conflict {
		t.Errorf("adjacent regions must conflict, got %v", status)
	}
}

func TestMerge_BinaryIsNotText(t *testing.T) {
	base := []byte("a\n\x00b\n")
	ours := []byte("a\n\x00c\n")
	theirs := []byte("a\n\x00d\n")

	got, status := Merge(base, ours, theirs)
	if status != NotText {
		t.Fatalf("status = %v, want NotText", status)
	}
	if got != nil {
		t.Errorf("NotText must return nil, got %q", got)
	}
}

func TestMerge_LargeIsNotText(t *testing.T) {
	var b bytes.Buffer
	for i := 0; i <= MaxLines; i++ {
		b.WriteString("x\n")
	}
	base := bytes.Repeat([]byte("base\n"), MaxLines+1)
	ours := bytes.Repeat([]byte("ours\n"), MaxLines+1)
	if _, status := Merge(base, ours, b.Bytes()); status != NotText {
		t.Errorf("oversized input must be NotText")
	}
}

func TestMerge_ConflictMarkersRespectMissingNewline(t *testing.T) {
	// The final line of a side may lack a newline; the marker must still land
	// on its own line.
	base := []byte("a\nb")
	ours := []byte("a\nOURS")
	theirs := []byte("a\nTHEIRS")

	got, status := Merge(base, ours, theirs)
	if status != Conflict {
		t.Fatalf("status = %v, want Conflict", status)
	}
	want := "a\n<<<<<<< local\nOURS\n=======\nTHEIRS\n>>>>>>> template\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMerge_ExactConflictOutput(t *testing.T) {
	base := []byte("a\nb\nc\n")
	ours := []byte("a\nB\nc\n")
	theirs := []byte("a\nT\nc\n")

	got, status := Merge(base, ours, theirs)
	if status != Conflict {
		t.Fatalf("status = %v, want Conflict", status)
	}
	want := "a\n<<<<<<< local\nB\n=======\nT\n>>>>>>> template\nc\n"
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMerge_InsertionsAtDifferentPositions(t *testing.T) {
	base := []byte("a\nb\n")
	ours := []byte("x\na\nb\n")
	theirs := []byte("a\nb\ny\n")

	got, status := Merge(base, ours, theirs)
	if status != Clean {
		t.Fatalf("status = %v, want Clean; got %q", status, got)
	}
	if want := "x\na\nb\ny\n"; string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMerge_SamePointInsertionsConflict(t *testing.T) {
	base := []byte("a\n")
	ours := []byte("x\na\n")
	theirs := []byte("y\na\n")

	if _, status := Merge(base, ours, theirs); status != Conflict {
		t.Errorf("same-point insertions must conflict, got %v", status)
	}
}

// TestMerge_MultiRegionConflictConservesBothSides exercises a cluster that
// spans several disjoint regions per side, including gaps and trailing base
// content inside applyRegions.
func TestMerge_MultiRegionConflictConservesBothSides(t *testing.T) {
	base := []byte("a\nb\nc\n")
	ours := []byte("A\nb\nC\n")
	theirs := []byte("a\nB\nc\n")

	got, status := Merge(base, ours, theirs)
	if status != Conflict {
		t.Fatalf("status = %v, want Conflict", status)
	}
	for _, side := range []string{"A\nb\nC\n", "a\nB\nc\n"} {
		if !bytes.Contains(got, []byte(side)) {
			t.Errorf("conflict lost a side %q in %q", side, got)
		}
	}
}

// FuzzMergeIdentity asserts the four three-way-merge identity laws for
// arbitrary inputs. These must hold regardless of text/binary classification
// because they are decided by the byte-equality fast paths.
func FuzzMergeIdentity(f *testing.F) {
	f.Add([]byte("a\n"), []byte("b\n"), []byte("c\n"))
	f.Add([]byte(""), []byte("x"), []byte(""))
	f.Add([]byte("x"), []byte("x"), []byte("y"))
	f.Add([]byte{0, 1}, []byte{0}, []byte{1})

	f.Fuzz(func(t *testing.T, base, ours, theirs []byte) {
		if got, _ := Merge(base, base, theirs); !bytes.Equal(got, theirs) {
			t.Fatalf("Merge(b,b,t) = %q, want %q", got, theirs)
		}
		if got, _ := Merge(base, ours, base); !bytes.Equal(got, ours) {
			t.Fatalf("Merge(b,o,b) = %q, want %q", got, ours)
		}
		if got, _ := Merge(base, ours, ours); !bytes.Equal(got, ours) {
			t.Fatalf("Merge(b,o,o) = %q, want %q", got, ours)
		}
	})
}
