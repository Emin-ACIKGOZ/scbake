// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// minimalManifest is a loadable manifest with no projects or templates.
const minimalManifest = "scbake_version = \"v0.3.0\"\nprojects = []\ntemplates = []\n"

func writeMinimalManifest(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "scbake.toml")
	if err := os.WriteFile(path, []byte(minimalManifest), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}

func assertUnchanged(t *testing.T, path string) {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // test-controlled temporary path
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != minimalManifest {
		t.Errorf("manifest was mutated by a rejected run:\n%s", got)
	}
}

// TestApplyRejectsInvalidUTF8SetValue reproduces #206's metadata variant: an
// invalid --set value must fail before writing an unloadable manifest.
func TestApplyRejectsInvalidUTF8SetValue(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	if err := os.Mkdir(filepath.Join(work, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	manifestPath := writeMinimalManifest(t, work)

	out, err := runCLI("apply", ".", "--with", "compliance",
		"--set", "license=MIT", "--set", "copyright_holder=\x82")
	if err == nil {
		t.Fatalf("apply accepted invalid UTF-8 metadata:\n%s", out)
	}
	assertUnchanged(t, manifestPath)
	if _, err := os.Stat(filepath.Join(work, "LICENSE")); !os.IsNotExist(err) {
		t.Errorf("rejected run wrote LICENSE")
	}
}

// TestApplyRejectsInvalidUTF8TemplatePath reproduces #206's path variant: an
// invalid target path must not be recorded as a template path.
func TestApplyRejectsInvalidUTF8TemplatePath(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)
	if err := os.Mkdir(filepath.Join(work, ".git"), 0o750); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	manifestPath := writeMinimalManifest(t, work)
	badDir := "\x82"
	if err := os.Mkdir(filepath.Join(work, badDir), 0o750); err != nil {
		t.Fatalf("mkdir bad dir: %v", err)
	}

	out, err := runCLI("apply", badDir, "--with", "compliance",
		"--set", "license=MIT", "--set", "copyright_holder=Acme")
	if err == nil {
		t.Fatalf("apply accepted an invalid UTF-8 target path:\n%s", out)
	}
	if !strings.Contains(out, "not valid UTF-8") {
		t.Errorf("error should explain the invalid encoding:\n%s", out)
	}
	assertUnchanged(t, manifestPath)
}
