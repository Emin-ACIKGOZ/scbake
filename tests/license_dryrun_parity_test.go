// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const licenseTestArgs = "--license=MIT"

// licenseNewProject creates a real project fixture and returns its directory.
func licenseNewProject(t *testing.T, root string, args ...string) string {
	t.Helper()
	all := append([]string{"new", "proj"}, args...)
	if out, err := dryRunRunIn(t, root, all...); err != nil {
		t.Fatalf("preparing project fixture: %v\n%s", err, out)
	}
	return filepath.Join(root, "proj")
}

// licenseSetupUnmanaged creates a project whose LICENSE predates scbake and is
// absent from the managed_files ledger.
func licenseSetupUnmanaged(t *testing.T, root string) string {
	t.Helper()
	project := licenseNewProject(t, root, "--with", "makefile")
	dryRunWriteFile(t, project, "LICENSE", "CUSTOM LICENSE\n")
	return project
}

// licenseSetupDrifted creates a project with a managed LICENSE and then edits
// it locally.
func licenseSetupDrifted(t *testing.T, root string) string {
	t.Helper()
	project := licenseNewProject(t, root,
		"--with", "compliance", licenseTestArgs, "--copyright-holder=Test Corp")
	path := filepath.Join(project, "LICENSE")
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("reading managed LICENSE: %v", err)
	}
	if err := os.WriteFile(path, append(data, []byte("\nLOCAL EDIT\n")...), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatalf("drifting managed LICENSE: %v", err)
	}
	return project
}

// TestDryRunLicenseConflictParity is a regression for #144: a pre-existing
// unmanaged LICENSE and a drifted managed LICENSE must fail a dry run with the
// same exit code and error text as the real run, leaving the tree untouched.
func TestDryRunLicenseConflictParity(t *testing.T) {
	complianceArgs := []string{
		"apply", "--with", "compliance", licenseTestArgs,
		"--copyright-holder=Test Corp", "--conflict-strategy=fail",
	}
	cases := []struct {
		name  string
		setup func(*testing.T, string) string
		args  []string
	}{
		{name: "apply unmanaged", setup: licenseSetupUnmanaged, args: complianceArgs},
		{name: "apply drifted managed", setup: licenseSetupDrifted, args: complianceArgs},
		{
			name:  "update drifted managed",
			setup: licenseSetupDrifted,
			args:  []string{"update", "--conflict-strategy=fail"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dryRoot := t.TempDir()
			dryProject := tc.setup(t, dryRoot)
			before := dryRunTakeSnapshot(t, dryRoot)
			dryOut, dryErr := dryRunRunIn(t, dryProject, append(append([]string{}, tc.args...), "--dry-run")...)
			after := dryRunTakeSnapshot(t, dryRoot)
			dryRunAssertSnapshotsEqual(t, before, after)
			dryRunAssertNoResidue(t, after)

			realRoot := t.TempDir()
			realProject := tc.setup(t, realRoot)
			licensePath := filepath.Join(realProject, "LICENSE")
			plainBefore, err := os.ReadFile(filepath.Clean(licensePath))
			if err != nil {
				t.Fatalf("reading LICENSE before real run: %v", err)
			}
			realOut, realErr := dryRunRunIn(t, realProject, tc.args...)

			dryRunAssertSameFailure(t, dryOut, dryErr, realOut, realErr, "Generate LICENSE file")
			line := dryRunErrorLine(dryOut)
			for _, want := range []string{"LICENSE", "manual modifications (drift detected)"} {
				if !strings.Contains(line, want) {
					t.Errorf("dry-run error = %q, want substring %q", line, want)
				}
			}
			plainAfter, err := os.ReadFile(filepath.Clean(licensePath))
			if err != nil {
				t.Fatalf("reading LICENSE after real run: %v", err)
			}
			if string(plainAfter) != string(plainBefore) {
				t.Errorf("failed real run rewrote LICENSE:\nbefore: %q\nafter:  %q", plainBefore, plainAfter)
			}
		})
	}
}

// TestDryRunLicenseUpdateSkipStaysWriteFree covers the update skip path: a clean
// managed LICENSE validates without repairing the pristine base shadow, so the
// dry run succeeds and writes nothing. The real update then repairs it.
func TestDryRunLicenseUpdateSkipStaysWriteFree(t *testing.T) {
	root := t.TempDir()
	project := licenseNewProject(t, root,
		"--with", "compliance", licenseTestArgs, "--copyright-holder=Test Corp")

	// Remove the committed base so a real update must repair it, while the
	// read-only validation must not.
	base := filepath.Join(project, ".scbake-base")
	if err := os.RemoveAll(base); err != nil {
		t.Fatalf("removing base tree: %v", err)
	}
	baseLicense := filepath.Join(base, "LICENSE")
	if _, err := os.Stat(baseLicense); !os.IsNotExist(err) {
		t.Fatalf("base LICENSE should be absent after removal (stat err: %v)", err)
	}

	before := dryRunTakeSnapshot(t, root)
	out, err := dryRunRunIn(t, project, "update", "--conflict-strategy=fail", "--dry-run")
	if err != nil {
		t.Fatalf("clean update dry run = %v, want nil\n%s", err, out)
	}
	after := dryRunTakeSnapshot(t, root)
	dryRunAssertSnapshotsEqual(t, before, after)
	dryRunAssertNoResidue(t, after)
	if _, err := os.Stat(baseLicense); !os.IsNotExist(err) {
		t.Fatalf("dry run repaired the base shadow (stat err: %v)", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".scbake")); !os.IsNotExist(err) {
		t.Errorf("dry run opened transaction state (stat err: %v)", err)
	}

	if out, err := dryRunRunIn(t, project, "update", "--conflict-strategy=fail"); err != nil {
		t.Fatalf("clean real update = %v, want nil\n%s", err, out)
	}
	if _, err := os.Stat(baseLicense); err != nil {
		t.Errorf("real update did not repair the base shadow: %v", err)
	}
}
