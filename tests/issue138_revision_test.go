// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// issue138RunCLISplit runs the built binary and keeps stdout and stderr apart.
// The shared runCLI helper merges them, which cannot prove a warning reached
// stderr rather than stdout.
func issue138RunCLISplit(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	//nolint:gosec // binaryPath is built by this test package's TestMain
	cmd := exec.CommandContext(context.Background(), binaryPath, args...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test Runner",
		"GIT_AUTHOR_EMAIL=runner@test.com",
		"GIT_COMMITTER_NAME=Test Runner",
		"GIT_COMMITTER_EMAIL=runner@test.com",
	)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	return out.String(), errOut.String(), err
}

// TestIssue138Revision_MalformedConfigWarnsAndFallsBack covers the untested
// fail-closed CLI seam: a malformed registries.json must warn on stderr, disable
// the registry tier, render the built-in template, and still exit 0.
func TestIssue138Revision_MalformedConfigWarnsAndFallsBack(t *testing.T) {
	home := isolateUserDirs(t)
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatalf("creating work dir: %v", err)
	}
	t.Chdir(work)

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("cannot determine user config dir: %v", err)
	}
	configPath := filepath.Join(configDir, "scbake", "registries.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o750); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte(`{"registries":[{"name":`), 0o600); err != nil {
		t.Fatalf("writing malformed config: %v", err)
	}

	stdout, stderr, err := issue138RunCLISplit(t, "new", "issue138-malformed", "--with", "editorconfig")
	if err != nil {
		t.Fatalf("new with malformed registry config failed (want exit 0): %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stderr, "registry configuration unavailable") {
		t.Errorf("stderr does not warn that the registry config is unavailable:\n%s", stderr)
	}
	if !strings.Contains(stderr, "registry template overrides disabled") {
		t.Errorf("stderr does not say registry overrides are disabled:\n%s", stderr)
	}

	issue138AssertBuiltin(t, filepath.Join(work, "issue138-malformed"))

	if data, readErr := os.ReadFile(filepath.Clean(configPath)); readErr != nil {
		t.Fatalf("malformed config disappeared: %v", readErr)
	} else if string(data) != `{"registries":[{"name":` {
		t.Errorf("malformed config was rewritten: %q", data)
	}
}

// TestIssue138Revision_DryRunNamesTemplateDir covers the third dry-run label the
// issue documents. The other two tiers are covered by the existing issue tests.
func TestIssue138Revision_DryRunNamesTemplateDir(t *testing.T) {
	home := isolateUserDirs(t)
	work := filepath.Join(home, "work")
	if err := os.MkdirAll(work, 0o750); err != nil {
		t.Fatalf("creating work dir: %v", err)
	}
	t.Chdir(work)

	overrideDir := filepath.Join(home, "override")
	if err := os.MkdirAll(overrideDir, 0o750); err != nil {
		t.Fatalf("creating override dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(overrideDir, ".editorconfig.tpl"), []byte("root = true\n# TEMPLATE DIR OVERRIDE\n"), 0o600); err != nil {
		t.Fatalf("writing override template: %v", err)
	}

	out, err := runCLI("new", "issue138-label", "--with", "editorconfig", "--template-dir", overrideDir, "--dry-run")
	if err != nil {
		t.Fatalf("new --dry-run with template-dir failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "[template-dir]") {
		t.Errorf("dry-run did not label the template-dir tier:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(work, "issue138-label")); !os.IsNotExist(err) {
		t.Errorf("dry-run created the project directory: %v", err)
	}
}
