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

// chdirToTempDir creates an isolated working directory and changes into it for
// the duration of the test.
func chdirToTempDir(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWd) })
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}
	return tmpDir
}

// readGeneratedMakefile reads the Makefile from a generated project.
func readGeneratedMakefile(t *testing.T, projectDir string) string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join(projectDir, "Makefile")) //nolint:gosec // path comes from t.TempDir
	if err != nil {
		t.Fatalf("read generated Makefile: %v", err)
	}
	return string(raw)
}

// assertMetadataKeysAbsent verifies that metadata keys are genuinely absent,
// as opposed to present with an empty value.
func assertMetadataKeysAbsent(t *testing.T, projectDir string, keys []string) {
	t.Helper()

	doc, md := decodeManifestDefaults(t, filepath.Join(projectDir, "scbake.toml"))
	for _, key := range keys {
		if md.IsDefined("metadata", key) {
			t.Errorf("manifest must not define metadata.%s", key)
		}
		if _, ok := doc.Metadata[key]; ok {
			t.Errorf("manifest metadata unexpectedly contains %q", key)
		}
	}
}

// assertMetadataValues verifies that metadata keys are present with the
// expected values.
func assertMetadataValues(t *testing.T, projectDir string, want map[string]string) {
	t.Helper()

	doc, md := decodeManifestDefaults(t, filepath.Join(projectDir, "scbake.toml"))
	for key, wantValue := range want {
		if !md.IsDefined("metadata", key) {
			t.Errorf("manifest must define metadata.%s", key)
		}
		if got := doc.Metadata[key]; got != wantValue {
			t.Errorf("metadata.%s = %q, want %q", key, got, wantValue)
		}
	}
}

// assertExplicitCommandTarget verifies that an explicit test_command is
// rendered verbatim and suppresses the per-language loop.
func assertExplicitCommandTarget(t *testing.T, target, command string) {
	t.Helper()

	if !strings.Contains(target, command) {
		t.Errorf("test target did not render the explicit test_command %q:\n%s", command, target)
	}
	if strings.Contains(target, "Testing ") || strings.Contains(target, "$(MAKE_TEST_COMMAND_") {
		t.Errorf("test target emitted a per-language loop despite an explicit test_command:\n%s", target)
	}
}

// TestNewMakefileWithoutLanguageOmitsGoDefaults is a regression guard for #148:
// creating a project with tooling but no language must not record Go build or
// test defaults, and the generated Makefile must fail clearly until tests are
// configured for the root.
func TestNewMakefileWithoutLanguageOmitsGoDefaults(t *testing.T) {
	tmpDir := chdirToTempDir(t)

	const projectName = "no-lang"
	output, err := runCLI("new", projectName, "--with", "makefile")
	if err != nil {
		t.Fatalf("new without language failed: %v\nOutput: %s", err, output)
	}

	projectDir := filepath.Join(tmpDir, projectName)
	assertMetadataKeysAbsent(t, projectDir, []string{"build_tool", "test_command"})

	makefile := readGeneratedMakefile(t, projectDir)
	if strings.Contains(makefile, "go test ./...") {
		t.Errorf("no-language Makefile must not contain a Go test default:\n%s", makefile)
	}
	target := manifestTestTarget(t, makefile)
	if !strings.Contains(target, "No projects configured; set metadata.test_command to run root-level tests.") || !strings.Contains(target, "@exit 1") {
		t.Errorf("no-language Makefile must fail with guidance to configure root tests:\n%s", target)
	}
}

// TestNewMakefileSetRecordsBuildToolAndTestCommand verifies that explicit --set
// values are still accepted, recorded in the manifest, and rendered.
func TestNewMakefileSetRecordsBuildToolAndTestCommand(t *testing.T) {
	tmpDir := chdirToTempDir(t)

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
	assertMetadataValues(t, projectDir, map[string]string{
		"build_tool":   "npm",
		"test_command": customTest,
	})

	target := manifestTestTarget(t, readGeneratedMakefile(t, projectDir))
	assertExplicitCommandTarget(t, target, customTest)
}

// assertPerProjectTestTarget verifies that the rendered test target runs the
// per-project Go test command and does not hard-code a root `go test ./...`.
func assertPerProjectTestTarget(t *testing.T, target string) {
	t.Helper()

	if !strings.Contains(target, "(cd . && $(MAKE_TEST_COMMAND_go))") {
		t.Errorf("test target did not run the per-project Go test command:\n%s", target)
	}
	if strings.Contains(target, "go test ./...") {
		t.Errorf("test target still renders the root go test ./... default:\n%s", target)
	}
}

// TestApplyMakefileWithoutTestCommandRendersPerProjectTests drives the real
// apply path: a Go project with no recorded test_command must get the
// per-project test loop, not a root-level go test ./... (#146).
func TestApplyMakefileWithoutTestCommandRendersPerProjectTests(t *testing.T) {
	tmpDir := chdirToTempDir(t)

	const projectName = "apply-proj"
	if output, err := runCLI("new", projectName, "--lang", "go"); err != nil {
		t.Fatalf("new go project failed: %v\nOutput: %s", err, output)
	}

	projectDir := filepath.Join(tmpDir, projectName)
	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir to project: %v", err)
	}
	if output, err := runCLI("apply", "--with", "makefile"); err != nil {
		t.Fatalf("apply makefile failed: %v\nOutput: %s", err, output)
	}

	assertMetadataKeysAbsent(t, projectDir, []string{"test_command"})
	assertPerProjectTestTarget(t, manifestTestTarget(t, readGeneratedMakefile(t, projectDir)))
}

// TestUpdateLegacyDefaultTestCommandRendersPerProjectTests covers manifests that
// already record the old go test ./... default: both new and update must keep
// rendering the per-project test loop for them.
func TestUpdateLegacyDefaultTestCommandRendersPerProjectTests(t *testing.T) {
	tmpDir := chdirToTempDir(t)

	const projectName = "legacy-proj"
	if output, err := runCLI("new", projectName, "--lang", "go", "--with", "makefile",
		"--set", "test_command=go test ./...",
	); err != nil {
		t.Fatalf("new with legacy test_command failed: %v\nOutput: %s", err, output)
	}

	projectDir := filepath.Join(tmpDir, projectName)
	assertMetadataValues(t, projectDir, map[string]string{"test_command": "go test ./..."})
	assertPerProjectTestTarget(t, manifestTestTarget(t, readGeneratedMakefile(t, projectDir)))

	if err := os.Chdir(projectDir); err != nil {
		t.Fatalf("chdir to project: %v", err)
	}
	if output, err := runCLI("update"); err != nil {
		t.Fatalf("update failed: %v\nOutput: %s", err, output)
	}

	assertPerProjectTestTarget(t, manifestTestTarget(t, readGeneratedMakefile(t, projectDir)))
}
