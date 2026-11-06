// Copyright 2025 Emin Salih Açıkgöz
// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

// manifestDefaultsDoc is the subset of scbake.toml needed to distinguish an
// absent metadata key from one recorded with an empty value.
type manifestDefaultsDoc struct {
	Metadata map[string]string `toml:"metadata"`
}

// manifestTestTarget extracts the rendered `test` target section from a
// generated Makefile.
func manifestTestTarget(t *testing.T, makefile string) string {
	t.Helper()

	const startMarker = "# --- Test Target ---"
	const endMarker = "# --- Main Clean Target ---"

	start := strings.Index(makefile, startMarker)
	end := strings.Index(makefile, endMarker)
	if start < 0 || end <= start {
		t.Fatalf("generated Makefile is missing a complete test target section:\n%s", makefile)
	}
	return makefile[start:end]
}

// decodeManifestDefaults reads and decodes a manifest, returning the decoded
// document and its TOML metadata.
func decodeManifestDefaults(t *testing.T, path string) (manifestDefaultsDoc, toml.MetaData) {
	t.Helper()

	raw, err := os.ReadFile(path) //nolint:gosec // path comes from t.TempDir
	if err != nil {
		t.Fatalf("read manifest %s: %v", path, err)
	}

	var doc manifestDefaultsDoc
	md, err := toml.Decode(string(raw), &doc)
	if err != nil {
		t.Fatalf("decode manifest %s: %v\n%s", path, err, raw)
	}
	return doc, md
}

// TestNewMakefileWithoutLanguageOmitsGoDefaults is a regression guard for #148:
// creating a project with tooling but no language must not record Go build or
// test defaults, and the generated Makefile must not assume a Go test.
func TestNewMakefileWithoutLanguageOmitsGoDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}

	const projectName = "no-lang"
	output, err := runCLI("new", projectName, "--with", "makefile")
	if err != nil {
		t.Fatalf("new without language failed: %v\nOutput: %s", err, output)
	}

	projectDir := filepath.Join(tmpDir, projectName)
	doc, md := decodeManifestDefaults(t, filepath.Join(projectDir, "scbake.toml"))

	for _, key := range []string{"build_tool", "test_command"} {
		if md.IsDefined("metadata", key) {
			t.Errorf("manifest must not define metadata.%s without a language or --set", key)
		}
		if _, ok := doc.Metadata[key]; ok {
			t.Errorf("manifest metadata unexpectedly contains %q", key)
		}
	}

	rawMakefile, err := os.ReadFile(filepath.Join(projectDir, "Makefile")) //nolint:gosec // path comes from t.TempDir
	if err != nil {
		t.Fatalf("read generated Makefile: %v", err)
	}
	makefile := string(rawMakefile)

	if strings.Contains(makefile, "go test ./...") {
		t.Errorf("no-language Makefile must not contain a Go test default:\n%s", makefile)
	}
	if !strings.Contains(manifestTestTarget(t, makefile), "No projects configured; nothing to test.") {
		t.Errorf("no-language Makefile must explain that there is nothing to test:\n%s", makefile)
	}
}

// TestNewMakefileSetRecordsBuildToolAndTestCommand verifies that explicit --set
// values are still accepted, recorded in the manifest, and rendered.
func TestNewMakefileSetRecordsBuildToolAndTestCommand(t *testing.T) {
	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}

	const projectName = "set-proj"
	const customTest = "npm test -- --run"

	output, err := runCLI("new", projectName, "--with", "makefile",
		"--set", "build_tool=npm",
		"--set", "test_command="+customTest,
	)
	if err != nil {
		t.Fatalf("new with --set failed: %v\nOutput: %s", err, output)
	}

	projectDir := filepath.Join(tmpDir, projectName)
	doc, md := decodeManifestDefaults(t, filepath.Join(projectDir, "scbake.toml"))

	for key, want := range map[string]string{
		"build_tool":   "npm",
		"test_command": customTest,
	} {
		if !md.IsDefined("metadata", key) {
			t.Errorf("manifest must define metadata.%s after --set", key)
		}
		if got := doc.Metadata[key]; got != want {
			t.Errorf("metadata.%s = %q, want %q", key, got, want)
		}
	}

	rawMakefile, err := os.ReadFile(filepath.Join(projectDir, "Makefile")) //nolint:gosec // path comes from t.TempDir
	if err != nil {
		t.Fatalf("read generated Makefile: %v", err)
	}
	target := manifestTestTarget(t, string(rawMakefile))

	if !strings.Contains(target, customTest) {
		t.Errorf("test target did not render the explicit test_command %q:\n%s", customTest, target)
	}
	if strings.Contains(target, "Testing ") || strings.Contains(target, "$(MAKE_TEST_COMMAND_") {
		t.Errorf("test target emitted a per-language loop despite an explicit test_command:\n%s", target)
	}
}
