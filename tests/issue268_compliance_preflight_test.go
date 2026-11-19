// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestIssue268NewComplianceRequiresFlagsBeforeCreatingDirectory is the
// regression for #268: missing compliance metadata must fail before `new`
// creates the project directory, and the error must name the CLI flags rather
// than the manifest fields. The baseline creates the directory, runs the plan,
// then fails inside the LICENSE task and cleans the directory up.
func TestIssue268NewComplianceRequiresFlagsBeforeCreatingDirectory(t *testing.T) {
	work := t.TempDir()
	skipIfAncestorHasMarker(t, work)
	t.Chdir(work)

	output, err := runCLI("new", "demo", "--with", "compliance")
	if err == nil {
		t.Fatalf("new succeeded without compliance metadata:\n%s", output)
	}
	if strings.Contains(output, "Creating directory") {
		t.Errorf("new created the project directory before validating compliance flags:\n%s", output)
	}
	if _, statErr := os.Stat(filepath.Join(work, "demo")); !os.IsNotExist(statErr) {
		t.Errorf("new left the project directory behind (stat error: %v):\n%s", statErr, output)
	}
	entries, readErr := os.ReadDir(work)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Errorf("new wrote into the work directory before failing: %v", entries)
	}
	line := dryRunErrorLine(output)
	if line == "" {
		t.Fatalf("missing Error line:\n%s", output)
	}
	for _, flag := range []string{"--license", "--copyright-holder"} {
		if !strings.Contains(line, flag) {
			t.Errorf("error does not name %s: %s", flag, line)
		}
	}
	if strings.Contains(line, "copyright_holder") {
		t.Errorf("error still names the internal manifest field: %s", line)
	}
}

// TestIssue268ApplyComplianceRequiresFlagsBeforeWriting pins the apply half of
// #268: on a project without recorded compliance metadata, `apply --with
// compliance` must fail before the plan executes. The baseline writes several
// files, fails on LICENSE, and rolls back, so the tree looks unchanged while
// the output proves the plan ran.
func TestIssue268ApplyComplianceRequiresFlagsBeforeWriting(t *testing.T) {
	root := t.TempDir()
	skipIfAncestorHasMarker(t, root)
	t.Chdir(root)

	if output, err := runCLI("new", "proj", "--with", "makefile"); err != nil {
		t.Fatalf("preparing project fixture: %v\n%s", err, output)
	}
	project := filepath.Join(root, "proj")
	t.Chdir(project)

	before := dryRunTakeSnapshot(t, project)
	output, err := runCLI("apply", "--with", "compliance")
	if err == nil {
		t.Fatalf("apply succeeded without compliance metadata:\n%s", output)
	}
	if strings.Contains(output, "Executing plan") {
		t.Errorf("apply executed the plan before validating compliance flags:\n%s", output)
	}
	dryRunAssertSnapshotsEqual(t, before, dryRunTakeSnapshot(t, project))
	line := dryRunErrorLine(output)
	if line == "" {
		t.Fatalf("missing Error line:\n%s", output)
	}
	for _, flag := range []string{"--license", "--copyright-holder"} {
		if !strings.Contains(line, flag) {
			t.Errorf("error does not name %s: %s", flag, line)
		}
	}
	if strings.Contains(line, "copyright_holder") {
		t.Errorf("error still names the internal manifest field: %s", line)
	}
}

// TestIssue268ApplyComplianceReusesRecordedMetadata keeps a valid invocation
// working: once a project records both values, re-applying the template does
// not require repeating the flags.
func TestIssue268ApplyComplianceReusesRecordedMetadata(t *testing.T) {
	root := t.TempDir()
	skipIfAncestorHasMarker(t, root)
	t.Chdir(root)

	if output, err := runCLI("new", "proj", "--with", "compliance",
		"--license", "MIT", "--copyright-holder", "Test Corp"); err != nil {
		t.Fatalf("preparing compliance project: %v\n%s", err, output)
	}
	t.Chdir(filepath.Join(root, "proj"))

	if output, err := runCLI("apply", "--with", "compliance"); err != nil {
		t.Fatalf("re-applying compliance with recorded metadata: %v\n%s", err, output)
	}
}

// TestIssue268UpdateFailsBeforeExecutingWhenMetadataMissing covers a hand-edited
// manifest that still selects compliance but lost the recorded metadata. update
// must fail before the plan executes rather than mid-LICENSE and roll back.
func TestIssue268UpdateFailsBeforeExecutingWhenMetadataMissing(t *testing.T) { //nolint:cyclop // multi-step manifest-corruption regression
	root := t.TempDir()
	skipIfAncestorHasMarker(t, root)
	t.Chdir(root)

	if output, err := runCLI("new", "proj", "--with", "compliance",
		"--license", "MIT", "--copyright-holder", "Test Corp"); err != nil {
		t.Fatalf("preparing compliance project: %v\n%s", err, output)
	}
	project := filepath.Join(root, "proj")
	t.Chdir(project)

	manifestPath := filepath.Join(project, "scbake.toml")
	data, err := os.ReadFile(manifestPath) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "license =") ||
			strings.HasPrefix(trimmed, "copyright_holder =") {
			continue
		}
		kept = append(kept, line)
	}
	if err := os.WriteFile(manifestPath, []byte(strings.Join(kept, "\n")), 0o600); err != nil { //nolint:gosec // test-controlled temp path
		t.Fatalf("rewrite manifest: %v", err)
	}

	output, err := runCLI("update")
	if err == nil {
		t.Fatalf("update succeeded with missing compliance metadata:\n%s", output)
	}
	if strings.Contains(output, "Executing plan") {
		t.Errorf("update executed the plan before validating metadata:\n%s", output)
	}
	for _, flag := range []string{"--license", "--copyright-holder"} {
		if !strings.Contains(output, flag) {
			t.Errorf("error does not name %s:\n%s", flag, output)
		}
	}
}

// TestIssue268ExistingDirectoryPrecedence keeps the pre-existing error order:
// an impossible target is reported before missing compliance flags.
func TestIssue268ExistingDirectoryPrecedence(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	if err := os.Mkdir(filepath.Join(work, "demo"), 0o750); err != nil {
		t.Fatalf("mkdir demo: %v", err)
	}

	out, err := runCLI("new", "demo", "--with", "compliance")
	if err == nil {
		t.Fatalf("new on an existing directory succeeded:\n%s", out)
	}
	line := dryRunErrorLine(out)
	if !strings.Contains(line, "already exists") {
		t.Errorf("expected the existing-directory error, got:\n%s", out)
	}
	if strings.Contains(line, "--license") {
		t.Errorf("compliance preflight masked the existing-directory error:\n%s", out)
	}
}

// TestIssue268ExplicitEmptySetOverrideIsHonored restores the pre-fix semantic:
// an explicit empty --set value is authoritative and fails as missing metadata.
func TestIssue268ExplicitEmptySetOverrideIsHonored(t *testing.T) {
	root := t.TempDir()
	skipIfAncestorHasMarker(t, root)
	t.Chdir(root)
	if out, err := runCLI("new", "proj", "--with", "compliance",
		"--license", "MIT", "--copyright-holder", "Test Corp"); err != nil {
		t.Fatalf("prepare project: %v\n%s", err, out)
	}
	project := filepath.Join(root, "proj")
	t.Chdir(project)

	out, err := runCLI("apply", "--with", "compliance", "--set", "license=")
	if err == nil {
		t.Fatalf("an explicit empty license override was silently ignored:\n%s", out)
	}
	if strings.Contains(out, "Executing plan") {
		t.Errorf("empty override reached plan execution:\n%s", out)
	}
	if !strings.Contains(out, "--license") {
		t.Errorf("error does not name --license:\n%s", out)
	}
}
