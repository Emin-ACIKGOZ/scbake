// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package tasks

import (
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"testing"
)

// TestManagedFileDryRunNote pins the note vocabulary for every non-plain
// reconciliation outcome. The executor appends the note only when it is
// non-empty, so an ordinary write must stay empty.
func TestManagedFileDryRunNote(t *testing.T) {
	absPath := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(absPath, []byte("existing"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		decision managedFileDecision
		want     string
	}{
		{
			name:     "skip is no change",
			decision: managedFileDecision{writePath: ""},
			want:     "no change",
		},
		{
			name:     "artifact is disclosed",
			decision: managedFileDecision{writePath: absPath + ".scbake-new", content: []byte("new")},
			want:     "conflict; would write artifact",
		},
		{
			name: "merge override equal to the existing bytes preserves local edits",
			decision: managedFileDecision{
				writePath: absPath,
				content:   []byte("pending"),
				override:  []byte("existing"),
			},
			want: "local edits preserved",
		},
		{
			name:     "pending bytes equal to existing are unchanged",
			decision: managedFileDecision{writePath: absPath, content: []byte("existing")},
			want:     "unchanged",
		},
		{
			name:     "ordinary write has no note",
			decision: managedFileDecision{writePath: absPath, content: []byte("new")},
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := managedFileDryRunNote(absPath, tt.decision); got != tt.want {
				t.Errorf("managedFileDryRunNote() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDryRunVerbForNote pins the verb vocabulary: a no-op reconciliation skips,
// a diverted write names its artifact, and an ordinary write keeps its original
// leading verb.
func TestDryRunVerbForNote(t *testing.T) {
	tests := []struct {
		note string
		want string
	}{
		{note: "", want: ""},
		{note: "no change", want: "Skip"},
		{note: "local edits preserved", want: "Skip"},
		{note: "unchanged", want: "Skip"},
		{note: "base repair only", want: "Skip"},
		{note: "conflict; would write artifact", want: "Write artifact for"},
	}
	for _, tt := range tests {
		if got := DryRunVerbForNote(tt.note); got != tt.want {
			t.Errorf("DryRunVerbForNote(%q) = %q, want %q", tt.note, got, tt.want)
		}
	}
}

// TestCreateTemplateTask_ValidateRecordsDryRunNote verifies that Validate wires
// the shared reconciliation decision into the annotation: a preserved merge and
// an ordinary create must be distinguishable, and an ordinary write stays
// unannotated.
func TestCreateTemplateTask_ValidateRecordsDryRunNote(t *testing.T) {
	tmpDir := t.TempDir()
	rendered := "Hello DryRunNote from v1.0.0"

	tc := types.TaskContext{
		TargetPath: tmpDir,
		Manifest: &types.Manifest{
			SbakeVersion: "v1.0.0",
			Projects:     []types.Project{{Name: "DryRunNote"}},
		},
		DryRun:           true,
		ConflictStrategy: types.ConflictMerge,
	}
	task := &CreateTemplateTask{
		TemplateFS:   testTemplates,
		TemplatePath: "testdata/simple.tpl",
		OutputPath:   "dryrun.txt",
		Desc:         "Create dryrun.txt",
		TaskPrio:     100,
	}

	// No existing file: an ordinary create, so no note.
	if err := task.Validate(tc); err != nil {
		t.Fatalf("Validate on missing file: %v", err)
	}
	if note := task.DryRunNote(); note != "" {
		t.Errorf("ordinary create note = %q, want empty", note)
	}
	if verb := task.DryRunVerb(); verb != "" {
		t.Errorf("ordinary create verb = %q, want empty", verb)
	}

	// A modified file with an unchanged template is a merge skip.
	outputPath := filepath.Join(tmpDir, "dryrun.txt")
	if err := os.WriteFile(outputPath, []byte(rendered+"\n// local edit\n"), fileutil.FilePerms); err != nil {
		t.Fatal(err)
	}
	tc.Manifest.ManagedFiles = map[string]string{"dryrun.txt": HashContent([]byte(rendered))}
	if err := task.Validate(tc); err != nil {
		t.Fatalf("Validate on preserved file: %v", err)
	}
	if note := task.DryRunNote(); note != "no change" {
		t.Errorf("preserved merge note = %q, want %q", note, "no change")
	}
	if verb := task.DryRunVerb(); verb != "Skip" {
		t.Errorf("preserved merge verb = %q, want %q", verb, "Skip")
	}
}
