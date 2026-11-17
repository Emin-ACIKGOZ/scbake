// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package compliance

import (
	"context"
	"os"
	"path/filepath"
	"scbake/internal/types"
	"strings"
	"testing"
)

// prepareLicenseConflict builds either a pre-existing unmanaged LICENSE or a
// managed LICENSE that was edited after Execute recorded it.
func prepareLicenseConflict(t *testing.T, drifted bool) (string, *types.Manifest) {
	t.Helper()

	dir := t.TempDir()
	manifest := &types.Manifest{
		Metadata: map[string]string{"license": "MIT", "copyright_holder": "Parity Holder"},
	}
	licensePath := filepath.Join(dir, "LICENSE")

	if drifted {
		task := &LicenseTask{TaskPrio: 100}
		tc := types.TaskContext{
			Ctx:              context.Background(),
			TargetPath:       dir,
			Manifest:         manifest,
			ConflictStrategy: types.ConflictFail,
		}
		if err := task.Execute(tc); err != nil {
			t.Fatalf("seed managed LICENSE: %v", err)
		}
		//nolint:gosec // Test temp directory
		if err := os.WriteFile(licensePath, []byte("DRIFTED CONTENT"), 0o644); err != nil {
			t.Fatalf("drift managed LICENSE: %v", err)
		}
		return dir, manifest
	}

	//nolint:gosec // Test temp directory
	if err := os.WriteFile(licensePath, []byte("CUSTOM LICENSE"), 0o644); err != nil {
		t.Fatalf("write unmanaged LICENSE: %v", err)
	}
	return dir, manifest
}

func readLicense(t *testing.T, path string) []byte {
	t.Helper()
	//nolint:gosec // Test temp directory
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading LICENSE %s: %v", path, err)
	}
	return data
}

// assertLicenseValidateReadOnly checks that Validate left the file, the ledger,
// and all managed-file state untouched.
func assertLicenseValidateReadOnly(t *testing.T, dir, licensePath string, before []byte, manifest *types.Manifest, recordedBefore string) {
	t.Helper()

	if after := readLicense(t, licensePath); string(after) != string(before) {
		t.Errorf("Validate() changed LICENSE:\nbefore: %q\nafter:  %q", before, after)
	}
	if got := manifest.ManagedFiles["LICENSE"]; got != recordedBefore {
		t.Errorf("Validate() changed the managed-files ledger: got %q, want %q", got, recordedBefore)
	}
	for _, state := range []string{".scbake", ".scbake-base"} {
		if _, err := os.Stat(filepath.Join(dir, state)); !os.IsNotExist(err) {
			t.Errorf("Validate() created %s state (stat err: %v)", state, err)
		}
	}
}

// assertLicenseConflictParity runs Validate and Execute against the same
// fixture and requires identical failure text.
func assertLicenseConflictParity(t *testing.T, drifted, update bool) {
	t.Helper()

	dir, manifest := prepareLicenseConflict(t, drifted)
	licensePath := filepath.Join(dir, "LICENSE")
	before := readLicense(t, licensePath)
	recordedBefore := manifest.ManagedFiles["LICENSE"]

	task := &LicenseTask{TaskPrio: 100}
	dryTC := types.TaskContext{
		Ctx:              context.Background(),
		TargetPath:       dir,
		Manifest:         manifest,
		DryRun:           true,
		Update:           update,
		ConflictStrategy: types.ConflictFail,
	}
	dryErr := task.Validate(dryTC)
	if dryErr == nil {
		t.Fatal("Validate() = nil, want the conflict the real run reports")
	}
	if !strings.Contains(dryErr.Error(), "manual modifications (drift detected)") {
		t.Errorf("Validate() error = %q, want the drift conflict text", dryErr)
	}
	assertLicenseValidateReadOnly(t, dir, licensePath, before, manifest, recordedBefore)

	realTC := dryTC
	realTC.DryRun = false
	realErr := task.Execute(realTC)
	if realErr == nil {
		t.Fatal("Execute() = nil, want the same conflict")
	}
	if dryErr.Error() != realErr.Error() {
		t.Errorf("dry-run error differs from real run:\ndry:  %v\nreal: %v", dryErr, realErr)
	}
}

// TestLicenseTask_ValidateParityForManagedConflicts is a regression for #144:
// Validate must make the same reconciliation decision as Execute, so a dry run
// fails exactly when a real run would for an unmanaged or drifted LICENSE.
func TestLicenseTask_ValidateParityForManagedConflicts(t *testing.T) {
	cases := []struct {
		name    string
		drifted bool
		update  bool
	}{
		{name: "apply unmanaged", drifted: false, update: false},
		{name: "apply drifted managed", drifted: true, update: false},
		{name: "update unmanaged", drifted: false, update: true},
		{name: "update drifted managed", drifted: true, update: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertLicenseConflictParity(t, tc.drifted, tc.update)
		})
	}
}
