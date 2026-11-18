// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"scbake/internal/util/fileutil"
	"testing"
)

// TestLicenseTask_ValidatePlansRestorableLicense is the task-level regression
// for the compliance half of #226: a deleted LICENSE is a canonical planned
// write, so a dry-run prediction must not list it as residual drift.
func TestLicenseTask_ValidatePlansRestorableLicense(t *testing.T) {
	root := t.TempDir()
	tc := types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: root,
		Manifest: &types.Manifest{
			Metadata: map[string]string{"license": "MIT", "copyright_holder": "Plan Holder"},
		},
		DryRun:        true,
		Update:        true,
		PlannedWrites: make(map[string]types.PlannedWrite),
	}

	task := &LicenseTask{TaskPrio: 100}
	if err := task.Validate(tc); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	if note := task.DryRunNote(); note != "" {
		t.Errorf("ordinary LICENSE create note = %q, want empty", note)
	}
	write, ok := tc.PlannedWrites["LICENSE"]
	if !ok {
		t.Fatalf("Validate did not plan a canonical LICENSE write: %+v", tc.PlannedWrites)
	}
	if write.Key != "LICENSE" || !write.Clean {
		t.Errorf("planned LICENSE write = %+v, want key LICENSE and Clean", write)
	}
}

// TestLicenseTask_ValidateMergeSkipsModifiedLicense pins the preview decision
// for a locally modified LICENSE whose template is unchanged: merge writes
// nothing, so the plan says Skip and annotates the no-op.
func TestLicenseTask_ValidateMergeSkipsModifiedLicense(t *testing.T) {
	root := t.TempDir()
	manifest := &types.Manifest{
		Metadata: map[string]string{"license": "MIT", "copyright_holder": "Plan Holder"},
	}
	task := &LicenseTask{TaskPrio: 100}
	if err := task.Execute(types.TaskContext{
		Ctx:        context.Background(),
		TargetPath: root,
		Manifest:   manifest,
	}); err != nil {
		t.Fatalf("seeding LICENSE failed: %v", err)
	}

	licensePath := filepath.Join(root, "LICENSE")
	f, err := os.OpenFile(licensePath, os.O_APPEND|os.O_WRONLY, fileutil.FilePerms) // #nosec G304 -- test path
	if err != nil {
		t.Fatalf("opening LICENSE: %v", err)
	}
	if _, err := f.WriteString("\nlocal license edit\n"); err != nil {
		_ = f.Close()
		t.Fatalf("appending local edit: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing LICENSE: %v", err)
	}

	tc := types.TaskContext{
		Ctx:              context.Background(),
		TargetPath:       root,
		Manifest:         manifest,
		DryRun:           true,
		Update:           true,
		ConflictStrategy: types.ConflictMerge,
		PlannedWrites:    make(map[string]types.PlannedWrite),
	}
	if err := task.Validate(tc); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	if note := task.DryRunNote(); note != "no change" {
		t.Errorf("modified LICENSE merge note = %q, want %q", note, "no change")
	}
	if verb := task.DryRunVerb(); verb != "Skip" {
		t.Errorf("modified LICENSE merge verb = %q, want %q", verb, "Skip")
	}
	if _, ok := tc.PlannedWrites["LICENSE"]; ok {
		t.Errorf("merge no-op planned a canonical LICENSE write: %+v", tc.PlannedWrites)
	}
}
